-- +goose Up
-- seed_policy stores per-torrent seeding stopping conditions as a JSON-encoded
-- SeedPolicy object: {"ratio_limit": <float>|null, "time_min_limit": <int>|null}.
-- NULL means "use the global default". An explicit object with null fields means
-- "no limit" (overrides the global default on a per-torrent basis).
ALTER TABLE torrents ADD COLUMN seed_policy TEXT;

-- seeding_started_at records the unix-second timestamp when a torrent first
-- reached 100% completion (Completed=true). Used by the seed-policy enforcement
-- loop to compute how long the torrent has been seeding.
ALTER TABLE torrents ADD COLUMN seeding_started_at INTEGER;

-- +goose Down
ALTER TABLE torrents DROP COLUMN seeding_started_at;
ALTER TABLE torrents DROP COLUMN seed_policy;
