package bootstrap

import (
	"context"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// snapshotAdapter bridges persistence.VerifySnapshots and engine.SnapshotStore.
// Previously copy-pasted into both package mains (root/verify_snapshot_adapter.go
// and cmd/mosaicd/verify_snapshot_adapter.go); lives here once.
type snapshotAdapter struct {
	store *persistence.VerifySnapshots
}

func (a *snapshotAdapter) LoadVerifySnapshot(id engine.TorrentID) ([]byte, bool, bool) {
	snap, complete, ok, err := a.store.Get(context.Background(), string(id))
	if err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("load verify snapshot")
		return nil, false, false
	}
	return snap, complete, ok
}

func (a *snapshotAdapter) SaveVerifySnapshot(id engine.TorrentID, snapshot []byte, wasComplete bool) error {
	return a.store.Upsert(context.Background(), string(id), snapshot, wasComplete)
}

func (a *snapshotAdapter) DeleteVerifySnapshot(id engine.TorrentID) error {
	return a.store.Delete(context.Background(), string(id))
}

func (a *snapshotAdapter) LoadPieceBitmap(id engine.TorrentID) ([]byte, bool, []byte, bool) {
	snap, complete, bitmap, ok, err := a.store.GetWithBitmap(context.Background(), string(id))
	if err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("load piece bitmap")
		return nil, false, nil, false
	}
	return snap, complete, bitmap, ok
}

func (a *snapshotAdapter) SavePieceBitmap(id engine.TorrentID, snapshot []byte, wasComplete bool, bitmap []byte) error {
	return a.store.UpsertWithBitmap(context.Background(), string(id), snapshot, wasComplete, bitmap)
}
