-- +goose Up
CREATE TABLE torrent_trackers (
  infohash  TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
  url       TEXT NOT NULL,
  PRIMARY KEY (infohash, url)
);

-- +goose Down
DROP TABLE torrent_trackers;
