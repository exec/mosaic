-- +goose Up
-- Per-torrent piece-completion bitmap, stored alongside the file snapshot
-- so we can repopulate anacrolix's bolt piece-completion store after its
-- per-add storage init wipes complete entries for files whose final
-- safeOsPath is missing or wrong size (the case for every partial
-- download with UsePartFiles=true).
--
-- One bit per piece: bit `i` is 1 iff piece `i` was complete at save time.
-- Length is `(NumPieces + 7) / 8` bytes. NULL = no bitmap saved (legacy
-- rows pre-this-migration, or fresh torrents with no recorded state).
ALTER TABLE verify_snapshots ADD COLUMN piece_bitmap BLOB;

-- +goose Down
ALTER TABLE verify_snapshots DROP COLUMN piece_bitmap;
