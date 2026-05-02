package main

import (
	"context"

	"github.com/rs/zerolog/log"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// verifySnapshotAdapter bridges the persistence DAO and the engine's
// SnapshotStore interface. The engine calls these synchronously off the
// verify-and-start goroutine; SQLite latency on a tiny indexed-by-PK row is
// well under a millisecond, so context.Background() is fine — the engine
// isn't context-aware on these calls and propagating one would just be
// boilerplate. Errors are logged and swallowed: a missing or unreadable
// snapshot just means we fall back to the full piece-by-piece verify, and a
// failed Save just means the next startup has to re-verify once.
type verifySnapshotAdapter struct {
	store *persistence.VerifySnapshots
}

func (a *verifySnapshotAdapter) LoadVerifySnapshot(id engine.TorrentID) ([]byte, bool, bool) {
	snap, complete, ok, err := a.store.Get(context.Background(), string(id))
	if err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("load verify snapshot")
		return nil, false, false
	}
	return snap, complete, ok
}

func (a *verifySnapshotAdapter) SaveVerifySnapshot(id engine.TorrentID, snapshot []byte, wasComplete bool) error {
	return a.store.Upsert(context.Background(), string(id), snapshot, wasComplete)
}

func (a *verifySnapshotAdapter) DeleteVerifySnapshot(id engine.TorrentID) error {
	return a.store.Delete(context.Background(), string(id))
}

func (a *verifySnapshotAdapter) LoadPieceBitmap(id engine.TorrentID) ([]byte, bool, []byte, bool) {
	snap, complete, bitmap, ok, err := a.store.GetWithBitmap(context.Background(), string(id))
	if err != nil {
		log.Warn().Err(err).Str("id", string(id)).Msg("load piece bitmap")
		return nil, false, nil, false
	}
	return snap, complete, bitmap, ok
}

func (a *verifySnapshotAdapter) SavePieceBitmap(id engine.TorrentID, snapshot []byte, wasComplete bool, bitmap []byte) error {
	return a.store.UpsertWithBitmap(context.Background(), string(id), snapshot, wasComplete, bitmap)
}
