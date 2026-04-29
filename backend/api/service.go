package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// Service is the only place business logic lives. Wails handlers and (later)
// HTTP handlers are thin adapters that translate transport shapes into Service
// calls.
type Service struct {
	engine          *engine.Engine
	torrents        *persistence.Torrents
	categories      *persistence.Categories
	tags            *persistence.Tags
	settings        *persistence.Settings
	defaultSavePath string

	focusMu    sync.RWMutex
	focusID    engine.TorrentID
	focusScope engine.DetailScope
}

func NewService(
	eng *engine.Engine,
	torrents *persistence.Torrents,
	categories *persistence.Categories,
	tags *persistence.Tags,
	settings *persistence.Settings,
	defaultSavePath string,
) *Service {
	return &Service{
		engine:          eng,
		torrents:        torrents,
		categories:      categories,
		tags:            tags,
		settings:        settings,
		defaultSavePath: defaultSavePath,
	}
}

const settingDefaultSavePath = "default_save_path"

func (s *Service) GetDefaultSavePath(ctx context.Context) (string, error) {
	v, err := s.settings.Get(ctx, settingDefaultSavePath)
	if errors.Is(err, persistence.ErrNotFound) {
		return s.defaultSavePath, nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Service) SetDefaultSavePath(ctx context.Context, path string) error {
	return s.settings.Set(ctx, settingDefaultSavePath, path)
}

func (s *Service) defaultPath(ctx context.Context) string {
	if v, err := s.GetDefaultSavePath(ctx); err == nil {
		return v
	}
	return s.defaultSavePath
}

// TorrentDTO is the shape returned to UI/transport callers.
type TorrentDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Magnet       string   `json:"magnet"`
	SavePath     string   `json:"save_path"`
	TotalBytes   int64    `json:"total_bytes"`
	BytesDone    int64    `json:"bytes_done"`
	Progress     float64  `json:"progress"` // 0..1
	DownloadRate int64    `json:"download_rate"`
	UploadRate   int64    `json:"upload_rate"`
	Peers        int      `json:"peers"`
	Seeds        int      `json:"seeds"`
	Paused       bool     `json:"paused"`
	Completed    bool     `json:"completed"`
	AddedAt      int64    `json:"added_at"` // unix seconds
	CategoryID   *int     `json:"category_id"`
	Tags         []TagDTO `json:"tags"`
}

func toDTO(s engine.Snapshot, addedAt time.Time) TorrentDTO {
	prog := 0.0
	if s.TotalBytes > 0 {
		prog = float64(s.BytesDone) / float64(s.TotalBytes)
	}
	return TorrentDTO{
		ID:           string(s.ID),
		Name:         s.Name,
		Magnet:       s.Magnet,
		SavePath:     s.SavePath,
		TotalBytes:   s.TotalBytes,
		BytesDone:    s.BytesDone,
		Progress:     prog,
		DownloadRate: s.RateDown,
		UploadRate:   s.RateUp,
		Peers:        s.Peers,
		Seeds:        s.Seeds,
		Paused:       s.Paused,
		Completed:    s.Completed,
		AddedAt:      addedAt.Unix(),
	}
}

func (s *Service) AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
	}
	id, err := s.engine.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", fmt.Errorf("add magnet: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		Magnet:   magnet,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) AddTorrentFile(ctx context.Context, filePath string) (engine.TorrentID, error) {
	blob, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read torrent file: %w", err)
	}
	id, err := s.engine.AddFile(ctx, blob, s.defaultSavePath)
	if err != nil {
		return "", fmt.Errorf("add torrent: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: s.defaultSavePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", fmt.Errorf("add torrent bytes: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) Pause(id engine.TorrentID) error  { return s.engine.Pause(id) }
func (s *Service) Resume(id engine.TorrentID) error { return s.engine.Resume(id) }

func (s *Service) Remove(ctx context.Context, id engine.TorrentID, deleteFiles bool) error {
	if err := s.engine.Remove(id, deleteFiles); err != nil {
		return err
	}
	return s.torrents.Remove(ctx, string(id))
}

func (s *Service) ListTorrents(ctx context.Context) ([]TorrentDTO, error) {
	records, err := s.torrents.List(ctx)
	if err != nil {
		return nil, err
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	snaps := s.engine.List()
	out := make([]TorrentDTO, 0, len(snaps))
	for _, snap := range snaps {
		rec, ok := byHash[string(snap.ID)]
		addedAt := time.Now()
		if ok {
			snap.SavePath = rec.SavePath
			if snap.Magnet == "" {
				snap.Magnet = rec.Magnet
			}
			addedAt = rec.AddedAt
		}
		dto := toDTO(snap, addedAt)
		if ok {
			dto.CategoryID = rec.CategoryID
		}
		tags, err := s.tags.ForTorrent(ctx, string(snap.ID))
		if err != nil {
			return nil, err
		}
		dto.Tags = make([]TagDTO, 0, len(tags))
		for _, tg := range tags {
			dto.Tags = append(dto.Tags, TagDTO{ID: tg.ID, Name: tg.Name, Color: tg.Color})
		}
		out = append(out, dto)
	}
	return out, nil
}

// GlobalStats is the snapshot displayed in the status bar.
type GlobalStats struct {
	TotalTorrents      int   `json:"total_torrents"`
	ActiveTorrents     int   `json:"active_torrents"`
	SeedingTorrents    int   `json:"seeding_torrents"`
	TotalDownloadRate  int64 `json:"total_download_rate"`
	TotalUploadRate    int64 `json:"total_upload_rate"`
	TotalPeers         int   `json:"total_peers"`
}

func (s *Service) GlobalStats(ctx context.Context) (GlobalStats, error) {
	snaps := s.engine.List()
	var st GlobalStats
	st.TotalTorrents = len(snaps)
	for _, snap := range snaps {
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

// SetInspectorFocus tells the service which torrent + tabs the UI is looking
// at. Subsequent DetailForFocus calls (and the inspector:tick event in app.go)
// will return the appropriately-scoped Detail. tabs is a subset of:
// "overview", "files", "peers", "trackers", "speed".
func (s *Service) SetInspectorFocus(id string, tabs []string) error {
	if id == "" {
		s.ClearInspectorFocus()
		return nil
	}
	scope := scopeForTabs(tabs)
	s.focusMu.Lock()
	s.focusID = engine.TorrentID(id)
	s.focusScope = scope
	s.focusMu.Unlock()
	return nil
}

func (s *Service) ClearInspectorFocus() {
	s.focusMu.Lock()
	s.focusID = ""
	s.focusScope = engine.DetailScope{}
	s.focusMu.Unlock()
}

// DetailForFocus returns the current focused torrent's detail, or nil if no
// inspector focus is set.
func (s *Service) DetailForFocus(ctx context.Context) (*DetailDTO, error) {
	s.focusMu.RLock()
	id := s.focusID
	scope := s.focusScope
	s.focusMu.RUnlock()
	if id == "" {
		return nil, nil
	}
	d, err := s.engine.DetailedSnapshot(id, scope)
	if err != nil {
		return nil, err
	}
	dto := detailToDTO(d, s.lookupAddedAt(ctx, id))
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

func (s *Service) lookupAddedAt(ctx context.Context, id engine.TorrentID) time.Time {
	rec, err := s.torrents.Get(ctx, string(id))
	if err != nil {
		return time.Time{}
	}
	return rec.AddedAt
}

func detailToDTO(d engine.Detail, addedAt time.Time) DetailDTO {
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
		Peers:      snap.Peers,
		Seeds:      snap.Seeds,
		AddedAt:    addedAt.Unix(),
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
	return s.categories.Update(ctx, persistence.Category{ID: id, Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) DeleteCategory(ctx context.Context, id int) error {
	return s.categories.Delete(ctx, id)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (int, error) {
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
	return s.tags.Delete(ctx, id)
}

func (s *Service) AssignTag(ctx context.Context, infohash string, tagID int) error {
	return s.tags.Assign(ctx, infohash, tagID)
}

func (s *Service) UnassignTag(ctx context.Context, infohash string, tagID int) error {
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
	return s.torrents.SetCategory(ctx, infohash, categoryID)
}

func (s *Service) SetFilePriorities(ctx context.Context, infohash string, prios map[int]string) error {
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
