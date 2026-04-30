-- +goose Up
-- Persist the .torrent metainfo bytes for file-added torrents so the engine
-- can re-add them on next launch (magnet-added torrents already have enough
-- in `magnet` to round-trip).
ALTER TABLE torrents ADD COLUMN metainfo BLOB;

-- +goose Down
ALTER TABLE torrents DROP COLUMN metainfo;
