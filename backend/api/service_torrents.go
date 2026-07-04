package api

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// TorrentDTO is the shape returned to UI/transport callers.
type TorrentDTO struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Magnet        string   `json:"magnet"`
	SavePath      string   `json:"save_path"`
	TotalBytes    int64    `json:"total_bytes"`
	BytesDone     int64    `json:"bytes_done"`
	Progress      float64  `json:"progress"` // 0..1
	DownloadRate  int64    `json:"download_rate"`
	UploadRate    int64    `json:"upload_rate"`
	Peers         int      `json:"peers"`
	Seeds         int      `json:"seeds"`
	Paused        bool     `json:"paused"`
	Completed     bool     `json:"completed"`
	AddedAt       int64    `json:"added_at"` // unix seconds
	CategoryID    *int     `json:"category_id"`
	Tags          []TagDTO `json:"tags"`
	QueuePosition int      `json:"queue_position"`
	ForceStart    bool     `json:"force_start"`
	Sequential    bool     `json:"sequential"`
	Queued        bool     `json:"queued"`
	Verifying     bool     `json:"verifying"`
	FilesMissing  bool     `json:"files_missing"`
	// Access is the requesting caller's access level on this torrent
	// ("owner" | "editor" | "viewer"). Admins always see "owner". The SPA
	// uses it to gate per-row controls.
	Access string `json:"access"`
}

// unixOrZero serializes a record timestamp for the wire, mapping the zero
// time (record missing or lookup failed) to 0. time.Time{}.Unix() is
// -62135596800, which the SPA would render as a date in year 1.
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func toDTO(s engine.Snapshot, addedAt time.Time) TorrentDTO {
	prog := 0.0
	if s.TotalBytes > 0 {
		prog = float64(s.BytesDone) / float64(s.TotalBytes)
	}
	return TorrentDTO{
		ID:            string(s.ID),
		Name:          s.Name,
		Magnet:        s.Magnet,
		SavePath:      s.SavePath,
		TotalBytes:    s.TotalBytes,
		BytesDone:     s.BytesDone,
		Progress:      prog,
		DownloadRate:  s.RateDown,
		UploadRate:    s.RateUp,
		Peers:         s.Peers,
		Seeds:         s.Seeds,
		Paused:        s.Paused,
		Completed:     s.Completed,
		AddedAt:       unixOrZero(addedAt),
		QueuePosition: s.QueuePosition,
		ForceStart:    s.ForceStart,
		Sequential:    s.Sequential,
		Queued:        s.Queued,
		Verifying:     s.Verifying,
		FilesMissing:  s.FilesMissing,
	}
}

func (s *Service) AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error) {
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
	}
	id, err := s.engine.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", fmt.Errorf("add magnet: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddMagnet: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		Magnet:   magnet,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", err
	}
	return id, nil
}

// grantOwner records that the caller owns a torrent. Adding an infohash that
// another user already added makes the caller a co-owner (Grant upserts).
func (s *Service) grantOwner(ctx context.Context, infohash string, caller Caller) error {
	uid := caller.UserID
	return s.access.Grant(ctx, infohash, uid, persistence.AccessOwner, &uid)
}

// persistAndGrantTorrent runs the post-engine-add steps (persist record,
// record ownership) with rollback semantics. The caller has already added the
// torrent to the engine and provides the persistence record. Without this
// rollback, a failed Save left the torrent in the engine but absent from the
// DB — it would download for the rest of this session, then vanish on next
// restart (RestoreOnStartup walks the DB, not the engine). A failed grantOwner
// left an orphaned, ownerless record visible to admins only.
//
// engine.Remove is called with deleteFiles=false: the operation just failed,
// nothing material has been downloaded yet, and we don't want to clobber
// anything that might have. Rollback errors are logged but not returned —
// the original failure is what the caller needs to see.
func (s *Service) persistAndGrantTorrent(ctx context.Context, id engine.TorrentID, caller Caller, rec persistence.TorrentRecord) error {
	if err := s.torrents.Save(ctx, rec); err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: engine rollback after persist failure also failed; torrent will linger until restart")
		}
		return fmt.Errorf("persist: %w", err)
	}
	if err := s.grantOwner(ctx, string(id), caller); err != nil {
		if rmErr := s.torrents.Remove(ctx, string(id)); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: persist rollback after grant failure also failed; record will survive as orphaned")
		}
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("add torrent: engine rollback after grant failure also failed; torrent will linger until restart")
		}
		return fmt.Errorf("grant owner: %w", err)
	}
	return nil
}

// resolveSavePath returns the cleaned absolute save path the engine should
// MkdirAll into, after applying the per-user containment policy.
//
// Admins (including the system caller used by the Wails desktop and internal
// workers) keep unrestricted behavior — the operator already has shell access
// to the host. Non-admin callers are pinned to <defaultSavePath>/<username>:
// an unconstrained save_path would let a low-privilege multi-user mosaicd
// account write torrent data into arbitrary directories the daemon process
// can reach (/etc/cron.d, another user's home, the daemon's data dir).
func (s *Service) resolveSavePath(ctx context.Context, caller Caller, savePath string) (string, error) {
	if caller.IsAdmin() {
		if savePath == "" {
			savePath = s.defaultPath(ctx)
		}
		return engine.ValidateSavePath("", savePath, false)
	}
	if caller.Username == "" || s.defaultSavePath == "" {
		return "", ErrForbidden
	}
	userRoot := filepath.Join(s.defaultSavePath, caller.Username)
	if savePath == "" {
		savePath = userRoot
	}
	return engine.ValidateSavePath(userRoot, savePath, true)
}

// requireTorrentAccess returns ErrForbidden unless the caller holds at least
// minLevel access on the torrent. Admins and the system caller always pass.
func (s *Service) requireTorrentAccess(ctx context.Context, infohash, minLevel string) error {
	caller := CallerFrom(ctx)
	if caller.SeesAllTorrents() {
		return nil
	}
	lvl, err := s.access.AccessFor(ctx, infohash, caller.UserID)
	if err != nil {
		return err
	}
	if persistence.AccessRank(lvl) < persistence.AccessRank(minLevel) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) AddTorrentFile(ctx context.Context, filePath, savePath string) (engine.TorrentID, error) {
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
	}
	blob, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read torrent file: %w", err)
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddTorrentFile: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error) {
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	savePath, err := s.resolveSavePath(ctx, caller, savePath)
	if err != nil {
		return "", err
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent bytes: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		if rmErr := s.engine.Remove(id, false); rmErr != nil {
			log.Warn().Err(rmErr).Str("infohash", string(id)).Msg("AddTorrentBytes: engine rollback after snapshot failure also failed")
		}
		return "", err
	}
	if err := s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
		Metainfo: blob,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// Pause/Resume/Recheck require editor-or-higher access on the torrent.
func (s *Service) Pause(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.engine.Pause(id); err != nil {
		return err
	}
	if err := s.torrents.SetPaused(ctx, string(id), true); err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("Pause: persist paused state failed")
	}
	return nil
}

func (s *Service) Resume(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.engine.Resume(id); err != nil {
		return err
	}
	if err := s.torrents.SetPaused(ctx, string(id), false); err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("Resume: persist paused state failed")
	}
	return nil
}

func (s *Service) Recheck(ctx context.Context, id engine.TorrentID) error {
	if err := s.requireTorrentAccess(ctx, string(id), persistence.AccessEditor); err != nil {
		return err
	}
	return s.engine.Recheck(id)
}

// PauseAll pauses every torrent currently known to the engine. Errors on
// individual torrents are logged but don't abort the loop — best-effort
// semantics so a single missing/dead torrent can't strand the rest.
// Used by the system-tray "Pause all" item.
func (s *Service) PauseAll(ctx context.Context) {
	for _, snap := range s.engine.List() {
		if snap.Paused {
			continue
		}
		if err := s.engine.Pause(snap.ID); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("PauseAll: pause failed")
			continue
		}
		if err := s.torrents.SetPaused(ctx, string(snap.ID), true); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("PauseAll: persist paused state failed")
		}
	}
}

// ResumeAll is the mirror of PauseAll. Used by the system-tray "Resume all"
// item when the engine is in the globally-paused state.
func (s *Service) ResumeAll(ctx context.Context) {
	for _, snap := range s.engine.List() {
		if !snap.Paused {
			continue
		}
		if err := s.engine.Resume(snap.ID); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("ResumeAll: resume failed")
			continue
		}
		if err := s.torrents.SetPaused(ctx, string(snap.ID), false); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("ResumeAll: persist paused state failed")
		}
	}
}

// UpdateInstalledNotifier is the post-install hook the desktop-integration
// notifications package implements. Defining the contract as an interface
// here keeps the api package free of a notifications-package import.
type UpdateInstalledNotifier interface {
	NotifyUpdateInstalled(version string)
}

// AttachUpdateInstalledNotifier wires the notifications subscriber into the
// Service so InstallUpdate can fire the OS-level notification on success.
// Pass nil (or never call) to disable the post-install notification.
func (s *Service) AttachUpdateInstalledNotifier(n UpdateInstalledNotifier) {
	s.updateInstalledNotifier = n
}

// Remove tears a torrent down or detaches it from the caller's view. An admin
// (or the last remaining owner) fully removes it from the engine + database;
// any other user with access just loses their own grant — the torrent keeps
// running for everyone else. A caller with no access gets ErrForbidden.
func (s *Service) Remove(ctx context.Context, id engine.TorrentID, deleteFiles bool) error {
	caller := CallerFrom(ctx)
	hash := string(id)
	if !caller.SeesAllTorrents() {
		lvl, err := s.access.AccessFor(ctx, hash, caller.UserID)
		if err != nil {
			return err
		}
		if lvl == "" {
			return ErrForbidden
		}
		if lvl != persistence.AccessOwner {
			return s.access.Revoke(ctx, hash, caller.UserID)
		}
		owners, err := s.access.OwnerCount(ctx, hash)
		if err != nil {
			return err
		}
		if owners > 1 {
			return s.access.Revoke(ctx, hash, caller.UserID)
		}
	}
	if err := s.engine.Remove(id, deleteFiles); err != nil {
		return err
	}
	// torrent_access rows cascade-delete with the torrents row.
	return s.torrents.Remove(ctx, hash)
}

// TorrentTickSnapshot is the per-tick state shared across every connected
// user: the engine's torrent snapshots, the persistence records keyed by
// infohash, and the tags map. None of it is caller-dependent, so mosaicd's
// streamTicks builds it ONCE per tick (see BuildTorrentTickSnapshot) and then
// fans it out — instead of re-running torrents.List + tags.ForAllTorrents +
// the full engine walk once per connected user.
type TorrentTickSnapshot struct {
	snaps      []engine.Snapshot
	byHash     map[string]persistence.TorrentRecord
	tagsByHash map[string][]persistence.Tag
}

// BuildTorrentTickSnapshot gathers the caller-independent torrent state for
// one tick. Call it once, then pass the result to ListTorrentsFromSnapshot /
// GlobalStatsFromSnapshot for each connected user.
func (s *Service) BuildTorrentTickSnapshot(ctx context.Context) (TorrentTickSnapshot, error) {
	records, err := s.torrents.List(ctx)
	if err != nil {
		return TorrentTickSnapshot{}, err
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	// Pre-v0.4.3 we called s.tags.ForTorrent(ctx, infohash) inside the DTO
	// loop — N+1 SELECTs every tick (~500ms cadence). One bulk fetch instead,
	// hashed by infohash so assembly is an O(1) lookup.
	tagsByHash, err := s.tags.ForAllTorrents(ctx)
	if err != nil {
		return TorrentTickSnapshot{}, err
	}
	snaps := s.engine.List()
	// Persist completed_at the first time we observe a torrent complete.
	// Every flavor's tick path funnels through here, so this is the one
	// choke point where the service sees the completion transition. The
	// record's nil check keeps the write to exactly once per torrent —
	// RestoreOnStartup reads the column to arm missing-files detection, and
	// the inspector surfaces it as DetailDTO.CompletedAt.
	now := time.Now()
	for _, snap := range snaps {
		rec, ok := byHash[string(snap.ID)]
		if !ok || !snap.Completed || rec.CompletedAt != nil {
			continue
		}
		if err := s.torrents.SetCompletedAt(ctx, string(snap.ID), now); err != nil {
			log.Warn().Err(err).Str("id", string(snap.ID)).Msg("tick: persist completed_at failed")
			continue
		}
		t := now
		rec.CompletedAt = &t
		byHash[string(snap.ID)] = rec
	}
	return TorrentTickSnapshot{
		snaps:      snaps,
		byHash:     byHash,
		tagsByHash: tagsByHash,
	}, nil
}

func (s *Service) ListTorrents(ctx context.Context) ([]TorrentDTO, error) {
	tick, err := s.BuildTorrentTickSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListTorrentsFromSnapshot(ctx, tick)
}

// ListTorrentsFromSnapshot assembles a caller's torrent DTO list from an
// already-built TorrentTickSnapshot. The shared snapshot is byte-identical
// across users; only the access filter, per-user Access level, sort, and DTO
// assembly happen here — so streamTicks pays the DB/engine cost once per tick
// rather than once per connected user. Output matches the old per-user
// ListTorrents exactly.
func (s *Service) ListTorrentsFromSnapshot(ctx context.Context, tick TorrentTickSnapshot) ([]TorrentDTO, error) {
	caller := CallerFrom(ctx)
	var accessByHash map[string]string
	if !caller.SeesAllTorrents() {
		var err error
		accessByHash, err = s.access.InfohashesForUser(ctx, caller.UserID)
		if err != nil {
			return nil, err
		}
	}
	out := make([]TorrentDTO, 0, len(tick.snaps))
	for _, snap := range tick.snaps {
		hash := string(snap.ID)
		access := persistence.AccessOwner
		if !caller.SeesAllTorrents() {
			lvl, visible := accessByHash[hash]
			if !visible {
				continue
			}
			access = lvl
		}
		rec, ok := tick.byHash[hash]
		var addedAt time.Time
		if ok {
			snap.SavePath = rec.SavePath
			if snap.Magnet == "" {
				snap.Magnet = rec.Magnet
			}
			addedAt = rec.AddedAt
		}
		dto := toDTO(snap, addedAt)
		dto.Access = access
		if ok {
			dto.CategoryID = rec.CategoryID
		}
		tags := tick.tagsByHash[hash]
		dto.Tags = make([]TagDTO, 0, len(tags))
		for _, tg := range tags {
			dto.Tags = append(dto.Tags, TagDTO{ID: tg.ID, Name: tg.Name, Color: tg.Color})
		}
		out = append(out, dto)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AddedAt != out[j].AddedAt {
			return out[i].AddedAt > out[j].AddedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GlobalStats is the snapshot displayed in the status bar.
type GlobalStats struct {
	TotalTorrents     int   `json:"total_torrents"`
	ActiveTorrents    int   `json:"active_torrents"`
	SeedingTorrents   int   `json:"seeding_torrents"`
	TotalDownloadRate  int64 `json:"total_download_rate"`
	TotalUploadRate    int64 `json:"total_upload_rate"`
	TotalPeers         int   `json:"total_peers"`
}

// EngineSnapshots returns the engine's current per-torrent snapshot slice.
// It is caller-independent: streamTicks walks the engine once per tick and
// passes the result to GlobalStatsFromSnapshot for each connected user.
func (s *Service) EngineSnapshots() []engine.Snapshot {
	return s.engine.List()
}

func (s *Service) GlobalStats(ctx context.Context) (GlobalStats, error) {
	return s.GlobalStatsFromSnapshot(ctx, s.engine.List())
}

// GlobalStatsFromSnapshot computes a caller's status-bar aggregate from an
// already-fetched engine snapshot slice. The snapshot is identical across
// users; per-user stats are just a filtered reduction of it, so streamTicks
// can walk the engine once per tick and reuse the slice for every connected
// user. Output matches the old per-user GlobalStats exactly.
func (s *Service) GlobalStatsFromSnapshot(ctx context.Context, snaps []engine.Snapshot) (GlobalStats, error) {
	caller := CallerFrom(ctx)
	var visible map[string]string
	if !caller.SeesAllTorrents() {
		var err error
		visible, err = s.access.InfohashesForUser(ctx, caller.UserID)
		if err != nil {
			return GlobalStats{}, err
		}
	}
	var st GlobalStats
	for _, snap := range snaps {
		if visible != nil {
			if _, ok := visible[string(snap.ID)]; !ok {
				continue
			}
		}
		st.TotalTorrents++
		if !snap.Paused && !snap.Completed {
			st.ActiveTorrents++
		}
		if snap.Completed {
			st.SeedingTorrents++
		}
		st.TotalDownloadRate += snap.RateDown
		st.TotalUploadRate += snap.RateUp
		st.TotalPeers += snap.Peers
	}
	return st, nil
}

// ─── Inspector ───────────────────────────────────────────────────────────────

// DetailDTO is the inspector tick payload, returned from DetailForFocus or
// emitted via the inspector:tick event.
type DetailDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Overview-tab fields — always present
	Magnet      string  `json:"magnet"`
	SavePath    string  `json:"save_path"`
	TotalBytes  int64   `json:"total_bytes"`
	BytesDone   int64   `json:"bytes_done"`
	Progress    float64 `json:"progress"`
	Ratio       float64 `json:"ratio"`
	TotalDown   int64   `json:"total_down"`
	TotalUp     int64   `json:"total_up"`
	Peers       int     `json:"peers"`
	Seeds       int     `json:"seeds"`
	AddedAt     int64   `json:"added_at"`
	CompletedAt int64   `json:"completed_at,omitempty"`
	// Status flags so the inspector can color the progress bar without
	// re-deriving from the torrents list.
	Paused       bool `json:"paused"`
	Completed    bool `json:"completed"`
	FilesMissing bool `json:"files_missing"`

	Files     []FileDTO    `json:"files,omitempty"`
	PeersList []PeerDTO    `json:"peers_list,omitempty"`
	Trackers  []TrackerDTO `json:"trackers,omitempty"`
}

type FileDTO struct {
	Index     int     `json:"index"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	BytesDone int64   `json:"bytes_done"`
	Progress  float64 `json:"progress"`
	Priority  string  `json:"priority"` // "skip" | "normal" | "high" | "max"
}

type PeerDTO struct {
	IP           string  `json:"ip"`
	Port         int     `json:"port"`
	Client       string  `json:"client"`
	Flags        string  `json:"flags"`
	Progress     float64 `json:"progress"`
	DownloadRate int64   `json:"download_rate"`
	UploadRate   int64   `json:"upload_rate"`
	Country      string  `json:"country"`
}

type TrackerDTO struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Seeds        int    `json:"seeds"`
	Peers        int    `json:"peers"`
	Downloaded   int    `json:"downloaded"`
	LastAnnounce int64  `json:"last_announce"`
	NextAnnounce int64  `json:"next_announce"`
}

// SetInspectorFocus tells the service which torrent + tabs the caller's UI is
// looking at. Focus is tracked per user, so concurrent mosaicd sessions don't
// clobber each other. Subsequent DetailForFocus calls (and the inspector:tick
// event) return the appropriately-scoped Detail. tabs is a subset of:
// "overview", "files", "peers", "trackers", "speed".
func (s *Service) SetInspectorFocus(ctx context.Context, id string, tabs []string) error {
	uid := CallerFrom(ctx).UserID
	if id == "" {
		s.ClearInspectorFocus(ctx)
		return nil
	}
	scope := scopeForTabs(tabs)
	s.focusMu.Lock()
	s.focus[uid] = focusState{id: engine.TorrentID(id), scope: scope}
	s.focusMu.Unlock()
	return nil
}

// ClearInspectorFocus drops the caller's inspector focus.
func (s *Service) ClearInspectorFocus(ctx context.Context) {
	uid := CallerFrom(ctx).UserID
	s.focusMu.Lock()
	delete(s.focus, uid)
	s.focusMu.Unlock()
}

// DetailForFocus returns the caller's focused torrent detail, or nil if no
// focus is set or the caller no longer has access to that torrent.
func (s *Service) DetailForFocus(ctx context.Context) (*DetailDTO, error) {
	caller := CallerFrom(ctx)
	s.focusMu.RLock()
	f, ok := s.focus[caller.UserID]
	s.focusMu.RUnlock()
	if !ok || f.id == "" {
		return nil, nil
	}
	if !caller.SeesAllTorrents() {
		lvl, err := s.access.AccessFor(ctx, string(f.id), caller.UserID)
		if err != nil {
			return nil, err
		}
		if lvl == "" {
			return nil, nil
		}
	}
	d, err := s.engine.DetailedSnapshot(f.id, f.scope)
	if err != nil {
		return nil, err
	}
	addedAt, completedAt := s.lookupRecordTimes(ctx, f.id)
	dto := detailToDTO(d, addedAt, completedAt)
	return &dto, nil
}

func scopeForTabs(tabs []string) engine.DetailScope {
	scope := engine.DetailScope{}
	for _, t := range tabs {
		switch t {
		case "files":
			scope.Files = true
		case "peers":
			scope.Peers = true
		case "trackers":
			scope.Trackers = true
		}
	}
	return scope
}

func (s *Service) lookupRecordTimes(ctx context.Context, id engine.TorrentID) (time.Time, *time.Time) {
	rec, err := s.torrents.Get(ctx, string(id))
	if err != nil {
		return time.Time{}, nil
	}
	return rec.AddedAt, rec.CompletedAt
}

func detailToDTO(d engine.Detail, addedAt time.Time, completedAt *time.Time) DetailDTO {
	snap := d.Snapshot
	prog := 0.0
	if snap.TotalBytes > 0 {
		prog = float64(snap.BytesDone) / float64(snap.TotalBytes)
	}
	dto := DetailDTO{
		ID:         string(snap.ID),
		Name:       snap.Name,
		Magnet:     snap.Magnet,
		SavePath:   snap.SavePath,
		TotalBytes: snap.TotalBytes,
		BytesDone:  snap.BytesDone,
		Progress:   prog,
		Ratio:      ratioOf(snap.BytesDown, snap.BytesUp),
		TotalDown:  snap.BytesDown,
		TotalUp:    snap.BytesUp,
		Peers:        snap.Peers,
		Seeds:        snap.Seeds,
		AddedAt:      unixOrZero(addedAt),
		Paused:       snap.Paused,
		Completed:    snap.Completed,
		FilesMissing: snap.FilesMissing,
	}
	if completedAt != nil {
		dto.CompletedAt = completedAt.Unix()
	}
	for _, f := range d.Files {
		fp := 0.0
		if f.Size > 0 {
			fp = float64(f.BytesDone) / float64(f.Size)
		}
		dto.Files = append(dto.Files, FileDTO{
			Index: f.Index, Path: f.Path, Size: f.Size, BytesDone: f.BytesDone, Progress: fp,
			Priority: priorityToString(f.Priority),
		})
	}
	for _, p := range d.Peers {
		dto.PeersList = append(dto.PeersList, PeerDTO{
			IP: p.IP, Port: p.Port, Client: p.ClientName, Flags: p.Flags,
			Progress: p.Progress, DownloadRate: p.DownloadRate, UploadRate: p.UploadRate, Country: p.CountryCode,
		})
	}
	for _, t := range d.Trackers {
		dto.Trackers = append(dto.Trackers, TrackerDTO{
			URL: t.URL, Status: t.Status, Seeds: t.Seeds, Peers: t.Peers, Downloaded: t.Downloaded,
			LastAnnounce: t.LastAnnounce.Unix(), NextAnnounce: t.NextAnnounce.Unix(),
		})
	}
	return dto
}

func ratioOf(down, up int64) float64 {
	if down == 0 {
		return 0
	}
	return float64(up) / float64(down)
}

func priorityToString(p engine.Priority) string {
	switch p {
	case engine.PrioritySkip:
		return "skip"
	case engine.PriorityHigh:
		return "high"
	case engine.PriorityMax:
		return "max"
	}
	return "normal"
}

// ─── Categories / Tags ───────────────────────────────────────────────────────

type CategoryDTO struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DefaultSavePath string `json:"default_save_path"`
	Color           string `json:"color"`
}

type TagDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Service) CreateCategory(ctx context.Context, name, defaultPath, color string) (int, error) {
	if !CallerFrom(ctx).CanManageCatTags() {
		return 0, ErrForbidden
	}
	return s.categories.Create(ctx, persistence.Category{Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) ListCategories(ctx context.Context) ([]CategoryDTO, error) {
	cats, err := s.categories.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryDTO, 0, len(cats))
	for _, c := range cats {
		out = append(out, CategoryDTO{ID: c.ID, Name: c.Name, DefaultSavePath: c.DefaultSavePath, Color: c.Color})
	}
	return out, nil
}

func (s *Service) UpdateCategory(ctx context.Context, id int, name, defaultPath, color string) error {
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.categories.Update(ctx, persistence.Category{ID: id, Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) DeleteCategory(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.categories.Delete(ctx, id)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (int, error) {
	if !CallerFrom(ctx).CanManageCatTags() {
		return 0, ErrForbidden
	}
	return s.tags.Create(ctx, persistence.Tag{Name: name, Color: color})
}

func (s *Service) ListTags(ctx context.Context) ([]TagDTO, error) {
	tags, err := s.tags.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) DeleteTag(ctx context.Context, id int) error {
	if !CallerFrom(ctx).CanManageCatTags() {
		return ErrForbidden
	}
	return s.tags.Delete(ctx, id)
}

// AssignTag/UnassignTag attach an existing tag to a torrent — that is per-
// torrent organisation, so editor access on the torrent is sufficient (you
// don't need the global manage-cat-tags permission, which gates the tag set
// itself).
func (s *Service) AssignTag(ctx context.Context, infohash string, tagID int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	return s.tags.Assign(ctx, infohash, tagID)
}

func (s *Service) UnassignTag(ctx context.Context, infohash string, tagID int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	return s.tags.Unassign(ctx, infohash, tagID)
}

func (s *Service) ListTagsFor(ctx context.Context, infohash string) ([]TagDTO, error) {
	tags, err := s.tags.ForTorrent(ctx, infohash)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) SetTorrentCategory(ctx context.Context, infohash string, categoryID *int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	return s.torrents.SetCategory(ctx, infohash, categoryID)
}

// ─── Per-torrent rate limits ───────────────────────────────────────────────

// TorrentRateLimitsDTO is the per-torrent bandwidth cap transport shape.
// Down/Up are in KB/s; 0 means unlimited.
type TorrentRateLimitsDTO struct {
	DownKbps int64 `json:"down_kbps"`
	UpKbps   int64 `json:"up_kbps"`
}

// GetTorrentRateLimits returns the persisted per-torrent rate limits in KB/s.
func (s *Service) GetTorrentRateLimits(ctx context.Context, infohash string) (TorrentRateLimitsDTO, error) {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessViewer); err != nil {
		return TorrentRateLimitsDTO{}, err
	}
	rec, err := s.torrents.Get(ctx, infohash)
	if err != nil {
		return TorrentRateLimitsDTO{}, err
	}
	return TorrentRateLimitsDTO{
		DownKbps: rec.DownRateLimit / 1024,
		UpKbps:   rec.UpRateLimit / 1024,
	}, nil
}

// SetTorrentRateLimits sets per-torrent download/upload caps. downKbps and
// upKbps are in KB/s; 0 means unlimited. Changes are persisted and applied
// to the live engine immediately.
func (s *Service) SetTorrentRateLimits(ctx context.Context, infohash string, downKbps, upKbps int64) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if downKbps < 0 || upKbps < 0 {
		return fmt.Errorf("rate limits must be >= 0")
	}
	downBPS := downKbps * 1024
	upBPS := upKbps * 1024
	if err := s.torrents.SetRateLimits(ctx, infohash, downBPS, upBPS); err != nil {
		return fmt.Errorf("persist rate limits: %w", err)
	}
	return s.engine.SetTorrentRateLimits(engine.TorrentID(infohash), downBPS, upBPS)
}

// ─── Trackers ───────────────────────────────────────────────────────────────

// validateTrackerURL returns an error if the URL is not a valid http, https, or
// udp tracker URL. Trackers over ws/wss are also accepted (WebSocket trackers).
func validateTrackerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "udp", "ws", "wss":
		// accepted
	default:
		return fmt.Errorf("tracker URL must use http, https, udp, ws, or wss scheme (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("tracker URL has no host")
	}
	return nil
}

// AddTracker adds a user-supplied tracker URL to a torrent and persists it so
// it survives restarts. Requires editor access on the torrent.
func (s *Service) AddTracker(ctx context.Context, infohash, trackerURL string) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := validateTrackerURL(trackerURL); err != nil {
		return err
	}
	if err := s.engine.AddTracker(engine.TorrentID(infohash), trackerURL); err != nil {
		return fmt.Errorf("add tracker to engine: %w", err)
	}
	if s.trackers != nil {
		if err := s.trackers.Add(ctx, infohash, trackerURL); err != nil {
			return fmt.Errorf("persist tracker: %w", err)
		}
	}
	return nil
}

// RemoveTracker removes a user-supplied tracker URL from a torrent and deletes
// its persisted record. Requires editor access on the torrent.
func (s *Service) RemoveTracker(ctx context.Context, infohash, trackerURL string) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.engine.RemoveTracker(engine.TorrentID(infohash), trackerURL); err != nil {
		return fmt.Errorf("remove tracker from engine: %w", err)
	}
	if s.trackers != nil {
		if err := s.trackers.Remove(ctx, infohash, trackerURL); err != nil {
			return fmt.Errorf("remove persisted tracker: %w", err)
		}
	}
	return nil
}

func (s *Service) SetFilePriorities(ctx context.Context, infohash string, prios map[int]string) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	mapped := make(map[int]engine.Priority, len(prios))
	for idx, p := range prios {
		switch p {
		case "skip":
			mapped[idx] = engine.PrioritySkip
		case "high":
			mapped[idx] = engine.PriorityHigh
		case "max":
			mapped[idx] = engine.PriorityMax
		default:
			mapped[idx] = engine.PriorityNormal
		}
	}
	return s.engine.SetFilePriorities(engine.TorrentID(infohash), mapped)
}

// ─── Queue ───────────────────────────────────────────────────────────────────

func (s *Service) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.torrents.SetQueuePosition(ctx, infohash, pos); err != nil {
		return err
	}
	s.engine.SetQueuePosition(engine.TorrentID(infohash), pos)
	return nil
}

func (s *Service) SetForceStart(ctx context.Context, infohash string, force bool) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.torrents.SetForceStart(ctx, infohash, force); err != nil {
		return err
	}
	s.engine.SetForceStart(engine.TorrentID(infohash), force)
	return nil
}

// SetSequential enables or disables sequential piece download for a torrent.
// When enabled, pieces are requested in order from first to last, allowing
// media to be previewed or streamed before the download completes. When
// disabled, the default rarest-first strategy is restored.
func (s *Service) SetSequential(ctx context.Context, infohash string, enabled bool) error {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessEditor); err != nil {
		return err
	}
	if err := s.torrents.SetSequential(ctx, infohash, enabled); err != nil {
		return err
	}
	s.engine.SetSequential(engine.TorrentID(infohash), enabled)
	return nil
}

// ─── Restore on startup ─────────────────────────────────────────────────────

// RestoreOnStartup hydrates engine + scheduler limits from persisted settings
// AND re-adds every persisted torrent to the engine so prior-session downloads
// resume on next launch. Call this once after constructing the Service.
//
// Installs SystemCaller on the context so callers don't have to — this is the
// trusted startup hydration path, not a user-driven action.
func (s *Service) RestoreOnStartup(ctx context.Context) error {
	ctx = WithCaller(ctx, SystemCaller)
	if err := s.recoverInvalidAdminPasswordHash(ctx); err != nil {
		log.Warn().Err(err).Msg("restore: recover invalid admin password hash failed")
	}
	q := s.GetQueueLimits(ctx)
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	if err := s.applyLimits(ctx); err != nil {
		return err
	}

	records, err := s.torrents.List(ctx)
	if err != nil {
		return fmt.Errorf("list persisted torrents: %w", err)
	}
	failed := 0
	orphaned := 0
	for _, r := range records {
		var id engine.TorrentID
		var addErr error
		switch {
		case len(r.Metainfo) > 0:
			id, addErr = s.engine.AddFile(ctx, r.Metainfo, r.SavePath)
		case r.Magnet != "":
			id, addErr = s.engine.AddMagnet(ctx, r.Magnet, r.SavePath)
		default:
			log.Warn().Str("infohash", r.InfoHash).Str("name", r.Name).Msg("restore: skipping orphan record (no magnet, no metainfo)")
			orphaned++
			continue
		}
		if addErr != nil {
			log.Warn().Err(addErr).Str("infohash", r.InfoHash).Str("name", r.Name).Msg("restore: re-add failed")
			failed++
			continue
		}
		if r.CompletedAt != nil {
			s.engine.MarkExpectedComplete(id)
		}
		s.engine.SetQueuePosition(id, r.QueuePosition)
		s.engine.SetForceStart(id, r.ForceStart)
		if r.Sequential {
			s.engine.SetSequential(id, true)
		}
		if r.Paused {
			if err := s.engine.Pause(id); err != nil {
				log.Warn().Err(err).Str("infohash", r.InfoHash).Msg("restore: re-pause failed")
			}
		}
		if r.DownRateLimit != 0 || r.UpRateLimit != 0 {
			if err := s.engine.SetTorrentRateLimits(id, r.DownRateLimit, r.UpRateLimit); err != nil {
				log.Warn().Err(err).Str("infohash", r.InfoHash).Msg("restore: re-apply rate limits failed")
			}
		}
		if s.trackers != nil {
			urls, err := s.trackers.List(ctx, r.InfoHash)
			if err != nil {
				log.Warn().Err(err).Str("infohash", r.InfoHash).Msg("restore: list user trackers failed")
			} else {
				for _, u := range urls {
					if err := s.engine.AddTracker(id, u); err != nil {
						log.Warn().Err(err).Str("infohash", r.InfoHash).Str("url", u).Msg("restore: re-add tracker failed")
					}
				}
			}
		}
	}
	if failed > 0 || orphaned > 0 {
		log.Warn().Int("failed", failed).Int("orphaned", orphaned).Int("total", len(records)).Msg("restore: not all torrents could be re-added")
	}
	return nil
}