-- +goose Up
CREATE TABLE categories (
  id                INTEGER PRIMARY KEY,
  name              TEXT UNIQUE NOT NULL,
  default_save_path TEXT,
  color             TEXT NOT NULL DEFAULT '#71717a'
);

CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  name  TEXT UNIQUE NOT NULL,
  color TEXT NOT NULL DEFAULT '#71717a'
);

CREATE TABLE torrent_tags (
  infohash TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (infohash, tag_id)
);

ALTER TABLE torrents ADD COLUMN category_id INTEGER REFERENCES categories(id);

-- +goose Down
ALTER TABLE torrents DROP COLUMN category_id;
DROP TABLE torrent_tags;
DROP TABLE tags;
DROP TABLE categories;
