-- +goose Up
-- Multi-user accounts. Pre-0009, web auth was a single global username /
-- password / api-key living in the settings table. This migration introduces
-- real user accounts and per-torrent ownership/sharing.
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  username      TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL DEFAULT '',
  password_set  INTEGER NOT NULL DEFAULT 0,  -- supersedes web_password_user_set
  role          TEXT NOT NULL DEFAULT 'user', -- 'admin' | 'user'
  perm_add_torrents    INTEGER NOT NULL DEFAULT 1,
  perm_manage_rss      INTEGER NOT NULL DEFAULT 0,
  perm_manage_cat_tags INTEGER NOT NULL DEFAULT 0,
  perm_change_settings INTEGER NOT NULL DEFAULT 0,
  perm_share           INTEGER NOT NULL DEFAULT 1,
  api_key_hash  TEXT NOT NULL DEFAULT '',  -- sha256 hex; empty = no key
  api_key_hint  TEXT NOT NULL DEFAULT '',  -- last 4 chars, for display
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

-- Per-torrent access grants. A torrent (one infohash = one engine entity) can
-- be reachable by several users at different access levels.
CREATE TABLE torrent_access (
  infohash   TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  access     TEXT NOT NULL DEFAULT 'viewer',  -- 'owner' | 'editor' | 'viewer'
  granted_by INTEGER REFERENCES users(id),
  granted_at INTEGER NOT NULL,
  PRIMARY KEY (infohash, user_id)
);

CREATE INDEX idx_torrent_access_user ON torrent_access(user_id);

-- Seed the admin user (id 1) from the legacy single-user web_* settings so an
-- upgraded install keeps its existing credentials. The legacy api key cannot
-- be hashed in SQL; Service.ReconcileLegacyAPIKey handles it at startup.
INSERT INTO users (id, username, password_hash, password_set, role,
  perm_add_torrents, perm_manage_rss, perm_manage_cat_tags, perm_change_settings, perm_share,
  disabled, created_at)
VALUES (
  1,
  COALESCE(NULLIF((SELECT value FROM settings WHERE key = 'web_username'), ''), 'admin'),
  COALESCE((SELECT value FROM settings WHERE key = 'web_password_hash'), ''),
  CASE WHEN (SELECT value FROM settings WHERE key = 'web_password_user_set') = 'true' THEN 1 ELSE 0 END,
  'admin',
  1, 1, 1, 1, 1,
  0,
  CAST(strftime('%s', 'now') AS INTEGER)
);

-- Every pre-existing torrent becomes owned by the admin user.
INSERT INTO torrent_access (infohash, user_id, access, granted_by, granted_at)
SELECT infohash, 1, 'owner', 1, CAST(strftime('%s', 'now') AS INTEGER) FROM torrents;

-- +goose Down
DROP INDEX idx_torrent_access_user;
DROP TABLE torrent_access;
DROP TABLE users;
