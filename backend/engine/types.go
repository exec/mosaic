package engine

import (
	"context"
	"time"
)

// TorrentID identifies a torrent in the engine. It's the hex-encoded infohash.
type TorrentID string

// AddRequest holds the inputs needed to add a torrent (file path or magnet).
type AddRequest struct {
	Magnet      string // one of Magnet or TorrentFile
	TorrentFile []byte
	SavePath    string
	Paused      bool
}

// Snapshot is a point-in-time view of a torrent's state, suitable for the UI.
type Snapshot struct {
	ID           TorrentID
	Name         string
	Magnet       string
	SavePath     string
	TotalBytes   int64
	BytesDone    int64
	DownloadRate int64 // bytes/sec
	UploadRate   int64
	Peers        int
	Seeds        int
	Paused       bool
	Completed    bool
	AddedAt      time.Time
}

// EventKind enumerates the kinds of EngineEvent.
type EventKind int

const (
	EventAdded EventKind = iota + 1
	EventRemoved
	EventTick // periodic state update
	EventComplete
	EventError
)

type EngineEvent struct {
	Kind     EventKind
	ID       TorrentID
	Snapshot Snapshot // populated for Added/Tick/Complete
	Err      error    // populated for Error
}

// Backend is the minimal interface the engine wrapper needs from a torrent
// library. The production implementation wraps anacrolix/torrent; tests use a
// fake.
type Backend interface {
	AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error)
	AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error)
	Pause(id TorrentID) error
	Resume(id TorrentID) error
	Remove(id TorrentID, deleteFiles bool) error
	List() []Snapshot
	Snapshot(id TorrentID) (Snapshot, error)
	Close() error
}
