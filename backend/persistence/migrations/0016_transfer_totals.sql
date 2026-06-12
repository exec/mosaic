-- +goose Up
-- Cumulative transfer totals in bytes, across engine sessions. anacrolix's
-- BytesWrittenData/BytesReadData counters reset every process start, so a
-- ratio computed from them restarted from 0 on every launch and ratio-based
-- seeding limits effectively never fired. CheckSeedLimits checkpoints the
-- session deltas into these columns each pass (see Torrents.AddTransferTotals).
ALTER TABLE torrents ADD COLUMN total_uploaded INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN total_downloaded INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE torrents DROP COLUMN total_downloaded;
ALTER TABLE torrents DROP COLUMN total_uploaded;
