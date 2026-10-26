-- +goose Up
CREATE TABLE transfer_daily (
 infohash TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
 day TEXT NOT NULL,
 uploaded INTEGER NOT NULL DEFAULT 0,
 downloaded INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(infohash,day)
);
CREATE INDEX transfer_daily_day ON transfer_daily(day);
-- +goose Down
DROP TABLE transfer_daily;
