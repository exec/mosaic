-- +goose Up
-- Index users.api_key_hash for bearer-token authentication. Every API-key
-- request resolves the caller via GetByAPIKeyHash, which is an equality lookup
-- on this column; without an index that is a full table scan of users.
--
-- Partial index: the vast majority of users have no API key (api_key_hash =
-- ''), so excluding the empty-string rows keeps the index small and means a
-- lookup never matches the no-key sentinel. SQLite supports partial indexes
-- with a WHERE clause, and GetByAPIKeyHash already filters `api_key_hash != ''`
-- so the optimizer can use this index for that exact query.
CREATE INDEX idx_users_api_key_hash ON users(api_key_hash) WHERE api_key_hash != '';

-- +goose Down
DROP INDEX idx_users_api_key_hash;
