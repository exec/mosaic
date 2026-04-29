package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// FakeBackend is an in-memory Backend for tests.
type FakeBackend struct {
	mu       sync.Mutex
	torrents map[TorrentID]*Snapshot
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{torrents: make(map[TorrentID]*Snapshot)}
}

func (f *FakeBackend) AddMagnet(_ context.Context, magnet, savePath string) (TorrentID, error) {
	id := hashOf(magnet)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torrents[id] = &Snapshot{
		ID:         id,
		Name:       "fake-" + string(id[:8]),
		Magnet:     magnet,
		SavePath:   savePath,
		TotalBytes: 1 << 30, // 1 GB placeholder
		AddedAt:    time.Now(),
	}
	return id, nil
}

func (f *FakeBackend) AddFile(_ context.Context, blob []byte, savePath string) (TorrentID, error) {
	id := hashOf(string(blob))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torrents[id] = &Snapshot{
		ID: id, Name: "fake-file", SavePath: savePath, AddedAt: time.Now(),
	}
	return id, nil
}

func (f *FakeBackend) Pause(id TorrentID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return errors.New("not found")
	}
	t.Paused = true
	return nil
}

func (f *FakeBackend) Resume(id TorrentID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return errors.New("not found")
	}
	t.Paused = false
	return nil
}

func (f *FakeBackend) Remove(id TorrentID, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.torrents[id]; !ok {
		return errors.New("not found")
	}
	delete(f.torrents, id)
	return nil
}

func (f *FakeBackend) List() []Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Snapshot, 0, len(f.torrents))
	for _, t := range f.torrents {
		out = append(out, *t)
	}
	return out
}

func (f *FakeBackend) Snapshot(id TorrentID) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	return *t, nil
}

func (f *FakeBackend) Close() error { return nil }

// AdvanceProgress is a test helper: bumps BytesDone for a torrent.
func (f *FakeBackend) AdvanceProgress(id TorrentID, by int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.torrents[id]; ok {
		t.BytesDone += by
		if t.BytesDone >= t.TotalBytes {
			t.Completed = true
		}
	}
}

func hashOf(s string) TorrentID {
	sum := sha1.Sum([]byte(s))
	return TorrentID(hex.EncodeToString(sum[:]))
}
