package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

type PrepareTorrentRequest struct {
	Magnet      string `json:"magnet"`
	TorrentFile []byte `json:"torrent_file"`
	SavePath    string `json:"save_path"`
}

// Prepared entries are persisted paused so closing the app cannot start an unconfirmed download.
func (s *Service) PrepareTorrent(ctx context.Context, req PrepareTorrentRequest) (string, error) {
	s.torrentMu.Lock()
	defer s.torrentMu.Unlock()
	caller := CallerFrom(ctx)
	if !caller.CanAddTorrents() {
		return "", ErrForbidden
	}
	path, err := s.resolveSavePath(ctx, caller, req.SavePath)
	if err != nil {
		return "", err
	}
	if (req.Magnet == "") == (len(req.TorrentFile) == 0) {
		return "", errors.New("provide one magnet link or torrent file")
	}
	var spec *torrent.TorrentSpec
	if len(req.TorrentFile) > 0 {
		mi, e := metainfo.Load(bytes.NewReader(req.TorrentFile))
		if e != nil {
			return "", e
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
	} else {
		spec, err = torrent.TorrentSpecFromMagnetUri(req.Magnet)
	}
	if err != nil {
		return "", err
	}
	if spec.InfoHash == (metainfo.Hash{}) && spec.InfoHashV2.Ok {
		spec.InfoHash = *spec.InfoHashV2.Value.ToShort()
	}
	hash := spec.InfoHash.HexString()
	for _, snap := range s.engine.List() {
		if string(snap.ID) == hash {
			return "", errors.New("torrent already exists")
		}
	}
	if _, e := s.torrents.Get(ctx, hash); e == nil {
		return "", errors.New("torrent already exists")
	} else if !errors.Is(e, persistence.ErrNotFound) {
		return "", e
	}
	id, err := s.engine.Add(ctx, engine.AddRequest{Magnet: req.Magnet, TorrentFile: req.TorrentFile, SavePath: path, Paused: true})
	if err != nil {
		return "", err
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		_ = s.engine.Remove(id, false)
		return "", err
	}
	err = s.persistAndGrantTorrent(ctx, id, caller, persistence.TorrentRecord{InfoHash: string(id), Name: snap.Name, Magnet: req.Magnet, Metainfo: req.TorrentFile, SavePath: path, AddedAt: time.Now(), Paused: true})
	return string(id), err
}

func (s *Service) GetTorrentDetail(ctx context.Context, id string) (*DetailDTO, error) {
	s.torrentMu.Lock()
	defer s.torrentMu.Unlock()
	if err := s.requireTorrentAccess(ctx, id, persistence.AccessViewer); err != nil {
		return nil, err
	}
	d, err := s.engine.DetailedSnapshot(engine.TorrentID(id), engine.DetailScope{Files: true})
	if err != nil {
		return nil, err
	}
	rec, err := s.torrents.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	d.Snapshot.SavePath = rec.SavePath
	d.Snapshot.Magnet = rec.Magnet
	d.Snapshot.BytesDown, d.Snapshot.BytesUp = s.transferTotals(rec, d.Snapshot)
	dto := detailToDTO(d, rec.AddedAt, rec.CompletedAt)
	return &dto, nil
}

type StorageDTO struct {
	FreeBytes   int64 `json:"free_bytes"`
	Preallocate bool  `json:"preallocate"`
}

func (s *Service) GetStorage(ctx context.Context, id string) (StorageDTO, error) {
	if err := s.requireTorrentAccess(ctx, id, persistence.AccessViewer); err != nil {
		return StorageDTO{}, err
	}
	rec, err := s.torrents.Get(ctx, id)
	if err != nil {
		return StorageDTO{}, err
	}
	v, _ := s.settings.Get(ctx, "storage.preallocate_full_files")
	return StorageDTO{FreeBytes: engine.FreeSpace(rec.SavePath), Preallocate: v == "true"}, nil
}
func (s *Service) SetPreallocate(ctx context.Context, enabled bool) error {
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	return s.settings.Set(ctx, "storage.preallocate_full_files", fmt.Sprint(enabled))
}
func (s *Service) RelocateTorrent(ctx context.Context, id, path string, move bool) error {
	if err := s.requireTorrentAccess(ctx, id, persistence.AccessOwner); err != nil {
		return err
	}
	s.torrentMu.Lock()
	defer s.torrentMu.Unlock()
	caller := CallerFrom(ctx)
	rec, err := s.torrents.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err = s.resolveSavePath(ctx, caller, rec.SavePath); err != nil {
		return err
	}
	path, err = s.resolveSavePath(ctx, caller, path)
	if err != nil {
		return err
	}
	snap, err := s.engine.Snapshot(engine.TorrentID(id))
	if err != nil {
		return err
	}
	if _, err := s.checkpointTransferTotals(ctx, &rec, snap); err != nil {
		return err
	}
	// Persist first: a crash may leave a duplicate copy, but never an active torrent at the old path.
	if err = s.torrents.SetSavePath(ctx, id, path); err != nil {
		return err
	}
	if err = s.engine.Relocate(ctx, engine.TorrentID(id), path, move); err != nil {
		if rollback := s.torrents.SetSavePath(context.WithoutCancel(ctx), id, rec.SavePath); rollback != nil {
			return fmt.Errorf("%w; restoring saved path: %v", err, rollback)
		}
		return err
	}
	s.seedCheckMu.Lock()
	delete(s.lastSeedCounters, id)
	s.seedCheckMu.Unlock()
	return nil
}

// TorrentFilePath accepts an index, not an arbitrary path, and refuses incomplete files.
func (s *Service) TorrentFilePath(ctx context.Context, id string, index int) (string, error) {
	d, err := s.GetTorrentDetail(ctx, id)
	if err != nil {
		return "", err
	}
	for _, f := range d.Files {
		if f.Index == index {
			if f.BytesDone < f.Size {
				return "", errors.New("file is not complete yet")
			}
			// The engine file path includes the torrent root, matching storage layout.
			relative := filepath.FromSlash(f.Path)
			if !filepath.IsLocal(relative) {
				return "", errors.New("file escapes download folder")
			}
			path := filepath.Join(d.SavePath, relative)
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", nativeFileAccessError(err)
			}
			root, err := filepath.EvalSymlinks(d.SavePath)
			if err != nil {
				return "", nativeFileAccessError(err)
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(os.PathSeparator) {
				return "", errors.New("file escapes download folder")
			}
			st, err := os.Stat(resolved)
			if err != nil {
				return "", nativeFileAccessError(err)
			}
			if !st.Mode().IsRegular() {
				return "", errors.New("not a regular file")
			}
			return resolved, nil
		}
	}
	return "", errors.New("file not found")
}

func nativeFileAccessError(err error) error {
	if os.IsNotExist(err) {
		return errors.New("file not found in its download folder. It may have been moved or renamed; use Locate existing files in Storage & location, then recheck")
	}
	if os.IsPermission(err) {
		return errors.New("Mosaic cannot access this file. Check the download folder permissions and try again")
	}
	return errors.New("file could not be accessed. Check the download folder and try again")
}
