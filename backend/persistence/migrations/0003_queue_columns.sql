-- +goose Up
ALTER TABLE torrents ADD COLUMN queue_position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN force_start INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE torrents DROP COLUMN force_start;
ALTER TABLE torrents DROP COLUMN queue_position;
