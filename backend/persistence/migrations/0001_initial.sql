-- +goose Up
CREATE TABLE torrents (
  infohash       TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  magnet         TEXT,
  save_path      TEXT NOT NULL,
  added_at       INTEGER NOT NULL,
  completed_at   INTEGER,
  paused         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- +goose Down
DROP TABLE settings;
DROP TABLE torrents;
