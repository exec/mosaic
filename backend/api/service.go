package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	scheduleRules   *persistence.ScheduleRules
	scheduler       *engine.Scheduler
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
	scheduleRules *persistence.ScheduleRules,
	scheduler *engine.Scheduler,
	defaultSavePath string,
) *Service {
	return &Service{
		engine:          eng,
		torrents:        torrents,
		categories:      categories,
		tags:            tags,
		settings:        settings,
		scheduleRules:   scheduleRules,
		scheduler:       scheduler,
		defaultSavePath: defaultSavePath,
	}
}

const (
	settingDefaultSavePath = "default_save_path"
	settingMaxActiveDL     = "max_active_downloads"
	settingMaxActiveSeeds  = "max_active_seeds"
	settingDownKbps        = "down_kbps"
	settingUpKbps          = "up_kbps"
	settingAltDownKbps     = "alt_down_kbps"
	settingAltUpKbps       = "alt_up_kbps"
	settingAltActive       = "alt_active"
)

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
	Queued        bool     `json:"queued"`
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
		AddedAt:       addedAt.Unix(),
		QueuePosition: s.QueuePosition,
		ForceStart:    s.ForceStart,
		Queued:        s.Queued,
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

func (s *Service) AddTorrentFile(ctx context.Context, filePath, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultPath(ctx)
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

// LimitsDTO is the bandwidth-limits transport shape (kbps units).
type LimitsDTO struct {
	DownKbps    int  `json:"down_kbps"`
	UpKbps      int  `json:"up_kbps"`
	AltDownKbps int  `json:"alt_down_kbps"`
	AltUpKbps   int  `json:"alt_up_kbps"`
	AltActive   bool `json:"alt_active"`
}

// QueueLimitsDTO is the queue-slot transport shape.
type QueueLimitsDTO struct {
	MaxActiveDownloads int `json:"max_active_downloads"`
	MaxActiveSeeds     int `json:"max_active_seeds"`
}

func (s *Service) GetLimits(ctx context.Context) (LimitsDTO, error) {
	return LimitsDTO{
		DownKbps:    s.intSetting(ctx, settingDownKbps),
		UpKbps:      s.intSetting(ctx, settingUpKbps),
		AltDownKbps: s.intSetting(ctx, settingAltDownKbps),
		AltUpKbps:   s.intSetting(ctx, settingAltUpKbps),
		AltActive:   s.boolSetting(ctx, settingAltActive),
	}, nil
}

func (s *Service) SetLimits(ctx context.Context, l LimitsDTO) error {
	if err := s.setIntSetting(ctx, settingDownKbps, l.DownKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingUpKbps, l.UpKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingAltDownKbps, l.AltDownKbps); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingAltUpKbps, l.AltUpKbps); err != nil {
		return err
	}
	if err := s.setBoolSetting(ctx, settingAltActive, l.AltActive); err != nil {
		return err
	}
	return s.applyLimits(ctx)
}

// ToggleAltSpeed flips the alt-speed flag and reapplies engine limits. Returns
// the new alt_active state.
func (s *Service) ToggleAltSpeed(ctx context.Context) (bool, error) {
	cur := s.boolSetting(ctx, settingAltActive)
	next := !cur
	if err := s.setBoolSetting(ctx, settingAltActive, next); err != nil {
		return cur, err
	}
	return next, s.applyLimits(ctx)
}

func (s *Service) applyLimits(ctx context.Context) error {
	l, _ := s.GetLimits(ctx)
	down, up := l.DownKbps*1024, l.UpKbps*1024
	if l.AltActive {
		down, up = l.AltDownKbps*1024, l.AltUpKbps*1024
	}
	return s.engine.SetGlobalRateLimits(down, up)
}

func (s *Service) GetQueueLimits(ctx context.Context) QueueLimitsDTO {
	return QueueLimitsDTO{
		MaxActiveDownloads: s.intSetting(ctx, settingMaxActiveDL),
		MaxActiveSeeds:     s.intSetting(ctx, settingMaxActiveSeeds),
	}
}

func (s *Service) SetQueueLimits(ctx context.Context, q QueueLimitsDTO) error {
	if err := s.setIntSetting(ctx, settingMaxActiveDL, q.MaxActiveDownloads); err != nil {
		return err
	}
	if err := s.setIntSetting(ctx, settingMaxActiveSeeds, q.MaxActiveSeeds); err != nil {
		return err
	}
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	return nil
}

func (s *Service) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	if err := s.torrents.SetQueuePosition(ctx, infohash, pos); err != nil {
		return err
	}
	s.engine.SetQueuePosition(engine.TorrentID(infohash), pos)
	return nil
}

func (s *Service) SetForceStart(ctx context.Context, infohash string, force bool) error {
	if err := s.torrents.SetForceStart(ctx, infohash, force); err != nil {
		return err
	}
	s.engine.SetForceStart(engine.TorrentID(infohash), force)
	return nil
}

// ScheduleRuleDTO is the transport shape for a time-of-day bandwidth rule.
type ScheduleRuleDTO struct {
	ID       int  `json:"id"`
	DaysMask int  `json:"days_mask"`
	StartMin int  `json:"start_min"`
	EndMin   int  `json:"end_min"`
	DownKbps int  `json:"down_kbps"`
	UpKbps   int  `json:"up_kbps"`
	AltOnly  bool `json:"alt_only"`
	Enabled  bool `json:"enabled"`
}

func toScheduleRuleDTO(r persistence.ScheduleRule) ScheduleRuleDTO {
	return ScheduleRuleDTO{
		ID: r.ID, DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	}
}

func (s *Service) ListScheduleRules(ctx context.Context) ([]ScheduleRuleDTO, error) {
	if s.scheduleRules == nil {
		return nil, nil
	}
	rules, err := s.scheduleRules.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleRuleDTO, 0, len(rules))
	for _, r := range rules {
		out = append(out, toScheduleRuleDTO(r))
	}
	return out, nil
}

func (s *Service) CreateScheduleRule(ctx context.Context, r ScheduleRuleDTO) (int, error) {
	return s.scheduleRules.Create(ctx, persistence.ScheduleRule{
		DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) UpdateScheduleRule(ctx context.Context, r ScheduleRuleDTO) error {
	return s.scheduleRules.Update(ctx, persistence.ScheduleRule{
		ID: r.ID, DaysMask: r.DaysMask, StartMin: r.StartMin, EndMin: r.EndMin,
		DownKbps: r.DownKbps, UpKbps: r.UpKbps, AltOnly: r.AltOnly, Enabled: r.Enabled,
	})
}

func (s *Service) DeleteScheduleRule(ctx context.Context, id int) error {
	return s.scheduleRules.Delete(ctx, id)
}

// RestoreOnStartup hydrates engine + scheduler limits from persisted settings.
// Call this once after constructing the Service so the fresh process picks up
// what the user configured last time.
func (s *Service) RestoreOnStartup(ctx context.Context) error {
	q := s.GetQueueLimits(ctx)
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	return s.applyLimits(ctx)
}

func (s *Service) intSetting(ctx context.Context, key string) int {
	v, err := s.settings.Get(ctx, key)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

func (s *Service) setIntSetting(ctx context.Context, key string, n int) error {
	return s.settings.Set(ctx, key, strconv.Itoa(n))
}

func (s *Service) boolSetting(ctx context.Context, key string) bool {
	v, _ := s.settings.Get(ctx, key)
	return v == "true"
}

func (s *Service) setBoolSetting(ctx context.Context, key string, b bool) error {
	v := "false"
	if b {
		v = "true"
	}
	return s.settings.Set(ctx, key, v)
}
