package api

import (
	"context"
	"fmt"
	"os"
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
	defaultSavePath string
}

func NewService(eng *engine.Engine, torrents *persistence.Torrents, defaultSavePath string) *Service {
	return &Service{engine: eng, torrents: torrents, defaultSavePath: defaultSavePath}
}

// TorrentDTO is the shape returned to UI/transport callers.
type TorrentDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Magnet       string  `json:"magnet"`
	SavePath     string  `json:"save_path"`
	TotalBytes   int64   `json:"total_bytes"`
	BytesDone    int64   `json:"bytes_done"`
	Progress     float64 `json:"progress"` // 0..1
	DownloadRate int64   `json:"download_rate"`
	UploadRate   int64   `json:"upload_rate"`
	Peers        int     `json:"peers"`
	Seeds        int     `json:"seeds"`
	Paused       bool    `json:"paused"`
	Completed    bool    `json:"completed"`
	AddedAt      int64   `json:"added_at"` // unix seconds
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
		DownloadRate: s.DownloadRate,
		UploadRate:   s.UploadRate,
		Peers:        s.Peers,
		Seeds:        s.Seeds,
		Paused:       s.Paused,
		Completed:    s.Completed,
		AddedAt:      addedAt.Unix(),
	}
}

func (s *Service) AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultSavePath
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
		out = append(out, toDTO(snap, addedAt))
	}
	return out, nil
}
