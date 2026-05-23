-- +goose Up
ALTER TABLE torrents ADD COLUMN sequential INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE torrents DROP COLUMN sequential;
