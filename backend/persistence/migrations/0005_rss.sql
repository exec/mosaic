-- +goose Up
CREATE TABLE rss_feeds (
  id            INTEGER PRIMARY KEY,
  url           TEXT NOT NULL,
  name          TEXT NOT NULL,
  interval_min  INTEGER NOT NULL DEFAULT 30,
  last_polled   INTEGER NOT NULL DEFAULT 0,
  etag          TEXT NOT NULL DEFAULT '',
  enabled       INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE rss_filters (
  id           INTEGER PRIMARY KEY,
  feed_id      INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
  regex        TEXT NOT NULL,
  category_id  INTEGER REFERENCES categories(id),
  save_path    TEXT,
  enabled      INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE rss_filters;
DROP TABLE rss_feeds;
