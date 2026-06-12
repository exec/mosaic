-- +goose Up
-- Per-feed GUIDs of RSS items the poller has already handled. Previously the
-- seen set was in-memory only, so every restart re-added every matching item
-- still present in the feed. Rows cascade-delete with their feed; the poller
-- caps each feed at ~1000 rows by pruning the oldest.
CREATE TABLE rss_seen (
  id       INTEGER PRIMARY KEY,
  feed_id  INTEGER NOT NULL REFERENCES rss_feeds(id) ON DELETE CASCADE,
  guid     TEXT NOT NULL,
  seen_at  INTEGER NOT NULL,
  UNIQUE (feed_id, guid)
);

-- +goose Down
DROP TABLE rss_seen;
