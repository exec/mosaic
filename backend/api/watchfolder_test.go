package api

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/engine"
)

// recordingAdder counts AddTorrentBytes calls so poll-dedup tests can assert
// a file is only added once. Thread-safe; the zero value is ready to use.
type recordingAdder struct {
	mu    sync.Mutex
	calls int
}

func (r *recordingAdder) AddMagnet(context.Context, string, string) (engine.TorrentID, error) {
	return "", nil
}

func (r *recordingAdder) AddTorrentBytes(context.Context, []byte, string) (engine.TorrentID, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return "id", nil
}

func (r *recordingAdder) SetTorrentCategory(context.Context, string, *int) error { return nil }

func (r *recordingAdder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestWatchFolder_StopAfterEmptyPathRestart pins the double-close fix: a
// Start that stops a running loop and then early-returns (empty path) must
// nil out stop/done, otherwise the following Stop re-closes a closed channel
// and panics.
func TestWatchFolder_StopAfterEmptyPathRestart(t *testing.T) {
	w := NewWatchFolder(&recordingAdder{})
	w.Start(t.TempDir(), false)
	w.Start("", false) // disable — early return after stopping the loop
	w.Stop()           // must be a no-op, not a panic
}

// TestWatchFolder_StopAfterStatFailureRestart is the same as above for the
// "path does not exist" early return.
func TestWatchFolder_StopAfterStatFailureRestart(t *testing.T) {
	w := NewWatchFolder(&recordingAdder{})
	w.Start(t.TempDir(), false)
	w.Start(filepath.Join(t.TempDir(), "missing"), false)
	w.Stop()
}

// TestWatchFolder_StopTwice ensures Stop stays idempotent.
func TestWatchFolder_StopTwice(t *testing.T) {
	w := NewWatchFolder(&recordingAdder{})
	w.Start(t.TempDir(), false)
	w.Stop()
	w.Stop()
}

// TestWatchFolder_PollSkipsAlreadyProcessed covers the processed-map dedup:
// with delete_after_add=false the same on-disk file must be added exactly
// once across polls, and a changed file (new size/mtime) must be re-added.
func TestWatchFolder_PollSkipsAlreadyProcessed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.torrent")
	require.NoError(t, os.WriteFile(path, []byte("blob"), 0o644))

	adder := &recordingAdder{}
	w := NewWatchFolder(adder)
	processed := make(map[string]string)
	ctx := context.Background()

	w.poll(ctx, dir, false, processed)
	w.poll(ctx, dir, false, processed)
	require.Equal(t, 1, adder.count(), "unchanged file must not be re-added")

	// Rewrite with different content + mtime → counts as a new file.
	require.NoError(t, os.WriteFile(path, []byte("blob2"), 0o644))
	require.NoError(t, os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)))
	w.poll(ctx, dir, false, processed)
	require.Equal(t, 2, adder.count(), "changed file should be re-added")

	// Delete the file → its processed entry is dropped.
	require.NoError(t, os.Remove(path))
	w.poll(ctx, dir, false, processed)
	require.Empty(t, processed)
}
