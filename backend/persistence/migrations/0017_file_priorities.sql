-- +goose Up
CREATE TABLE torrent_file_priorities (
 infohash TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
 file_index INTEGER NOT NULL,
 priority INTEGER NOT NULL CHECK(priority BETWEEN 0 AND 3),
 PRIMARY KEY(infohash,file_index)
);
-- +goose Down
DROP TABLE torrent_file_priorities;
