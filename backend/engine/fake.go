package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

// FakeBackend is an in-memory Backend for tests.
type FakeBackend struct {
	mu        sync.Mutex
	torrents  map[TorrentID]*Snapshot
	filePrios map[TorrentID]map[int]Priority

	downBPS int
	upBPS   int
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{
		torrents:  make(map[TorrentID]*Snapshot),
		filePrios: make(map[TorrentID]map[int]Priority),
	}
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

// DetailedSnapshot returns a deterministic fixture for the requested scope.
// Files: two entries (one half-done, one done). Peers: one entry. Trackers: one entry.
func (f *FakeBackend) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return Detail{}, errors.New("not found")
	}
	d := Detail{Snapshot: *t}
	if scope.Files {
		d.Files = []FileEntry{
			{Index: 0, Path: "fake/disk1.iso", Size: 1 << 29, BytesDone: 1 << 28, Priority: PriorityNormal},
			{Index: 1, Path: "fake/README", Size: 4096, BytesDone: 4096, Priority: PriorityNormal},
		}
		if prios, ok := f.filePrios[id]; ok {
			for i := range d.Files {
				if p, set := prios[d.Files[i].Index]; set {
					d.Files[i].Priority = p
				}
			}
		}
	}
	if scope.Peers {
		d.Peers = []PeerEntry{
			{IP: "10.0.0.1", Port: 6881, ClientName: "FakeClient 1.0", Flags: "D K E", Progress: 0.5, DownloadRate: 1024, UploadRate: 256, CountryCode: "US"},
		}
	}
	if scope.Trackers {
		d.Trackers = []TrackerEntry{
			{URL: "https://tracker.example/announce", Status: "OK", Seeds: 10, Peers: 5, Downloaded: 100, LastAnnounce: time.Unix(1700000000, 0), NextAnnounce: time.Unix(1700001800, 0)},
		}
	}
	return d, nil
}

func (f *FakeBackend) SetGlobalRateLimits(downBPS, upBPS int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downBPS = downBPS
	f.upBPS = upBPS
	return nil
}

// GlobalRateLimits returns the most recently set down/up BPS values. Test-only.
func (f *FakeBackend) GlobalRateLimits() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.downBPS, f.upBPS
}

func (f *FakeBackend) SetIPBlocklist(_ io.Reader) error { return nil }

func (f *FakeBackend) SetQueuePosition(id TorrentID, pos int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.torrents[id]; ok {
		t.QueuePosition = pos
	}
}

func (f *FakeBackend) SetForceStart(id TorrentID, force bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.torrents[id]; ok {
		t.ForceStart = force
	}
}

func (f *FakeBackend) ScheduledPause(id TorrentID, paused bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.torrents[id]; ok {
		t.Queued = paused
	}
}

func (f *FakeBackend) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.torrents[id]; !ok {
		return errors.New("not found")
	}
	if f.filePrios[id] == nil {
		f.filePrios[id] = make(map[int]Priority)
	}
	for idx, p := range prios {
		f.filePrios[id][idx] = p
	}
	return nil
}

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
