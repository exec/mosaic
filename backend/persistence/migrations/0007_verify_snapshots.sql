-- +goose Up
-- Per-torrent file snapshot used to skip the full piece-by-piece verify on
-- startup when the on-disk file state is unchanged since shutdown. The
-- snapshot blob is a SHA-256 of `path|size|mtime_ns` lines; was_complete
-- gates the fast-resume path (we only skip verify when the prior session
-- ended fully complete). Cascade delete with the parent torrent.
CREATE TABLE IF NOT EXISTS verify_snapshots (
    infohash     TEXT PRIMARY KEY,
    snapshot     BLOB    NOT NULL,
    was_complete INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    FOREIGN KEY (infohash) REFERENCES torrents(infohash) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS verify_snapshots;
