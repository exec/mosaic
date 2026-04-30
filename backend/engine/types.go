package engine

import (
	"context"
	"io"
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
	ID            TorrentID
	Name          string
	Magnet        string
	SavePath      string
	TotalBytes    int64
	BytesDone     int64
	BytesDown     int64 // cumulative bytes downloaded this session
	BytesUp       int64 // cumulative bytes uploaded this session
	RateDown      int64 // instantaneous bytes/sec
	RateUp        int64
	Peers         int
	Seeds         int
	Paused        bool
	Completed     bool
	AddedAt       time.Time
	QueuePosition int  // 0 = top of queue
	ForceStart    bool
	Queued        bool // true if scheduler is holding it back
	Verifying     bool // hashing existing files against the metainfo
	FilesMissing  bool // was-complete on prior session, now isn't (user deleted files)
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
	DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error)
	SetFilePriorities(id TorrentID, prios map[int]Priority) error
	SetGlobalRateLimits(downBytesPerSec, upBytesPerSec int) error // 0 = unlimited
	SetIPBlocklist(reader io.Reader) error                        // nil clears
	SetQueuePosition(id TorrentID, pos int)
	SetForceStart(id TorrentID, force bool)
	ScheduledPause(id TorrentID, paused bool) // distinct from manual Pause
	// MarkExpectedComplete tells the backend that this torrent was 100%
	// complete on a prior session — so if VerifyData on add finds <100%,
	// flag it as FilesMissing (user deleted files) and skip auto-download.
	MarkExpectedComplete(id TorrentID)
	Close() error
}

// FileEntry is one file inside a torrent's content tree.
type FileEntry struct {
	Index     int    // anacrolix file index, stable across the torrent's life
	Path      string // forward-slash relative path within the torrent
	Size      int64
	BytesDone int64
	Priority  Priority // Skip/Normal/High/Max
}

// Priority maps to anacrolix's piece-priority levels.
type Priority int

const (
	PrioritySkip   Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityMax    Priority = 3
)

// PeerEntry is one connected (or recently connected) peer.
type PeerEntry struct {
	IP           string
	Port         int
	ClientName   string  // e.g. "qBittorrent 4.6.2"
	Flags        string  // BitTorrent client flags string ("D K E I O X cd" etc.)
	Progress     float64 // 0..1
	DownloadRate int64   // bytes/sec from this peer
	UploadRate   int64   // bytes/sec to this peer
	CountryCode  string  // ISO-3166 alpha-2; empty if unknown
}

// TrackerEntry is one tracker URL announced by the torrent.
type TrackerEntry struct {
	URL          string
	Status       string // "OK", "Updating", "Not contacted", "Error: ..."
	Seeds        int    // last reported
	Peers        int
	Downloaded   int // last reported total downloads
	LastAnnounce time.Time
	NextAnnounce time.Time
}

// DetailScope controls how much detail the engine packs into a Detail.
type DetailScope struct {
	Files    bool
	Peers    bool
	Trackers bool
}

// Detail is a Snapshot plus optional per-tab heavy data. Empty slices when
// the corresponding scope flag is false.
type Detail struct {
	Snapshot Snapshot
	Files    []FileEntry
	Peers    []PeerEntry
	Trackers []TrackerEntry
}
