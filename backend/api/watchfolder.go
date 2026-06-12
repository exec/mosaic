package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// WatchFolder polls a directory every 5 seconds for new .torrent files and
// adds them via a TorrentAdder. It is attached to the Service via
// AttachWatchFolder so that SetWatchFolder can (re)start the polling loop
// when the user changes the configuration.
type WatchFolder struct {
	adder TorrentAdder
	mu    sync.Mutex
	stop  chan struct{}
	done  chan struct{}
}

// NewWatchFolder creates a WatchFolder watcher that polls every 5 s. It does
// NOT start polling automatically — call Start(path, deleteAfterAdd) once the
// persisted config is known. If the configured path is empty or absent the
// watcher does nothing until restarted with a non-empty path.
func NewWatchFolder(adder TorrentAdder) *WatchFolder {
	return &WatchFolder{adder: adder}
}

// Start (re)starts the polling loop with a new path and deleteAfterAdd flag.
// Calling Start while already running stops the previous loop first. Calling
// with an empty path is a no-op (acts as disable).
func (w *WatchFolder) Start(path string, deleteAfterAdd bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Stop any running loop. Swap stop/done to nil under the lock BEFORE
	// closing, so neither a concurrent Stop() during the unlocked wait below
	// nor a later Stop() after one of the early returns can ever observe —
	// and re-close — an already-closed channel.
	if w.stop != nil {
		stop, done := w.stop, w.done
		w.stop, w.done = nil, nil
		close(stop)
		w.mu.Unlock()
		<-done
		w.mu.Lock()
	}

	if path == "" {
		return
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		log.Warn().Str("path", path).Msg("watch_folder: directory does not exist or is not a directory; watcher disabled until re-enabled")
		return
	}

	w.stop = make(chan struct{})
	w.done = make(chan struct{})

	go w.run(path, deleteAfterAdd, w.stop, w.done)
}

// Stop tears down the polling loop. Safe to call when not running.
func (w *WatchFolder) Stop() {
	w.mu.Lock()
	if w.stop == nil {
		w.mu.Unlock()
		return
	}
	// Same swap-before-close discipline as Start: once the fields are nil
	// nobody else can reach the closed channel.
	stop, done := w.stop, w.done
	w.stop, w.done = nil, nil
	w.mu.Unlock()
	close(stop)
	<-done
}

func (w *WatchFolder) run(dir string, deleteAfterAdd bool, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	log.Info().Str("dir", dir).Bool("delete_after_add", deleteAfterAdd).Msg("watch_folder: started")

	ctx := WithCaller(context.Background(), SystemCaller)

	// processed remembers files already added this session, keyed by path
	// with a size+mtime fingerprint as the value. Without it, every poll
	// with delete_after_add=false re-adds every file in the folder (and
	// re-triggers a verify each time). A changed fingerprint counts as a
	// new file; entries for files that disappear are dropped so a later
	// re-creation is picked up again.
	processed := make(map[string]string)

	// Poll immediately on start, then on each 5-second tick.
	w.poll(ctx, dir, deleteAfterAdd, processed)

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			log.Info().Str("dir", dir).Msg("watch_folder: stopped")
			return
		case <-t.C:
			w.poll(ctx, dir, deleteAfterAdd, processed)
		}
	}
}

func (w *WatchFolder) poll(ctx context.Context, dir string, deleteAfterAdd bool, processed map[string]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("watch_folder: ReadDir failed")
		return
	}
	present := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".torrent") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			// File vanished between ReadDir and stat; next poll catches it.
			continue
		}
		present[path] = struct{}{}
		fingerprint := fmt.Sprintf("%d|%d", info.Size(), info.ModTime().UnixNano())
		if processed[path] == fingerprint {
			continue // already added this exact file
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("watch_folder: read file failed")
			continue
		}
		id, err := w.adder.AddTorrentBytes(ctx, blob, "")
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("watch_folder: AddTorrentBytes failed")
			continue
		}
		processed[path] = fingerprint
		log.Info().Str("path", path).Str("id", string(id)).Msg("watch_folder: torrent added")
		if deleteAfterAdd {
			if err := os.Remove(path); err != nil {
				log.Warn().Err(err).Str("path", path).Msg("watch_folder: delete after add failed")
			}
		}
	}
	// Drop processed entries for files that no longer exist so the map can't
	// grow unboundedly and a re-created file is treated as new.
	for p := range processed {
		if _, ok := present[p]; !ok {
			delete(processed, p)
		}
	}
}
