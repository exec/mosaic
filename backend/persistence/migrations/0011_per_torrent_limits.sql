-- +goose Up
ALTER TABLE torrents ADD COLUMN down_rate_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN up_rate_limit INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE torrents DROP COLUMN up_rate_limit;
ALTER TABLE torrents DROP COLUMN down_rate_limit;
