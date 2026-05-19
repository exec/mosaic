package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Access levels stored in torrent_access.access, ordered weakest to strongest.
const (
	AccessViewer = "viewer" // read-only
	AccessEditor = "editor" // may pause/resume/recheck/set priorities & queue
	AccessOwner  = "owner"  // may additionally delete & re-share
)

// AccessRank maps an access level to a comparable integer. Higher = more
// capable. An unknown/empty level ranks 0 (no access).
func AccessRank(access string) int {
	switch access {
	case AccessOwner:
		return 3
	case AccessEditor:
		return 2
	case AccessViewer:
		return 1
	default:
		return 0
	}
}

// TorrentAccessRow is a single (torrent, user) access grant.
type TorrentAccessRow struct {
	InfoHash  string
	UserID    int
	Access    string
	GrantedBy *int
	GrantedAt time.Time
}

// TorrentAccess is the DAO for the torrent_access table.
type TorrentAccess struct{ db *DB }

func NewTorrentAccess(db *DB) *TorrentAccess { return &TorrentAccess{db: db} }

// Grant inserts or updates a (torrent, user) access row. grantedBy may be nil
// (e.g. system / migration grants).
func (a *TorrentAccess) Grant(ctx context.Context, infohash string, userID int, access string, grantedBy *int) error {
	var by sql.NullInt64
	if grantedBy != nil {
		by = sql.NullInt64{Int64: int64(*grantedBy), Valid: true}
	}
	_, err := a.db.SQL().ExecContext(ctx, `
INSERT INTO torrent_access (infohash, user_id, access, granted_by, granted_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(infohash, user_id) DO UPDATE SET
  access = excluded.access,
  granted_by = excluded.granted_by,
  granted_at = excluded.granted_at`,
		infohash, userID, access, by, time.Now().Unix())
	return err
}

// Revoke removes a single (torrent, user) grant. Missing rows are not an error.
func (a *TorrentAccess) Revoke(ctx context.Context, infohash string, userID int) error {
	_, err := a.db.SQL().ExecContext(ctx,
		`DELETE FROM torrent_access WHERE infohash = ? AND user_id = ?`, infohash, userID)
	return err
}

// RevokeAllForTorrent drops every grant for a torrent (used when the torrent
// itself is removed from the engine).
func (a *TorrentAccess) RevokeAllForTorrent(ctx context.Context, infohash string) error {
	_, err := a.db.SQL().ExecContext(ctx,
		`DELETE FROM torrent_access WHERE infohash = ?`, infohash)
	return err
}

// AccessFor returns the access level a user has on a torrent, or "" if none.
func (a *TorrentAccess) AccessFor(ctx context.Context, infohash string, userID int) (string, error) {
	var access string
	err := a.db.SQL().QueryRowContext(ctx,
		`SELECT access FROM torrent_access WHERE infohash = ? AND user_id = ?`,
		infohash, userID).Scan(&access)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return access, err
}

// ListForTorrent returns every access grant for a torrent, owners first.
func (a *TorrentAccess) ListForTorrent(ctx context.Context, infohash string) ([]TorrentAccessRow, error) {
	rows, err := a.db.SQL().QueryContext(ctx, `
SELECT infohash, user_id, access, granted_by, granted_at
FROM torrent_access WHERE infohash = ?
ORDER BY CASE access WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, user_id`, infohash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TorrentAccessRow
	for rows.Next() {
		var r TorrentAccessRow
		var by sql.NullInt64
		var grantedAt int64
		if err := rows.Scan(&r.InfoHash, &r.UserID, &r.Access, &by, &grantedAt); err != nil {
			return nil, err
		}
		if by.Valid {
			v := int(by.Int64)
			r.GrantedBy = &v
		}
		r.GrantedAt = time.Unix(grantedAt, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// TorrentShareRow is one access grant joined with the grantee's username, for
// display in the share UI.
type TorrentShareRow struct {
	UserID   int
	Username string
	Access   string
}

// ListSharesForTorrent returns every access grant for a torrent with the
// grantee's username resolved in a single JOIN, owners first. This replaces an
// N+1 of per-row user lookups.
func (a *TorrentAccess) ListSharesForTorrent(ctx context.Context, infohash string) ([]TorrentShareRow, error) {
	rows, err := a.db.SQL().QueryContext(ctx, `
SELECT ta.user_id, u.username, ta.access
FROM torrent_access ta
JOIN users u ON u.id = ta.user_id
WHERE ta.infohash = ?
ORDER BY CASE ta.access WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, ta.user_id`, infohash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TorrentShareRow
	for rows.Next() {
		var r TorrentShareRow
		if err := rows.Scan(&r.UserID, &r.Username, &r.Access); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InfohashesForUser returns the set of infohashes a user has any access to.
func (a *TorrentAccess) InfohashesForUser(ctx context.Context, userID int) (map[string]string, error) {
	rows, err := a.db.SQL().QueryContext(ctx,
		`SELECT infohash, access FROM torrent_access WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var hash, access string
		if err := rows.Scan(&hash, &access); err != nil {
			return nil, err
		}
		out[hash] = access
	}
	return out, rows.Err()
}

// OwnerCount returns how many users hold 'owner' access on a torrent.
func (a *TorrentAccess) OwnerCount(ctx context.Context, infohash string) (int, error) {
	var n int
	err := a.db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM torrent_access WHERE infohash = ? AND access = 'owner'`,
		infohash).Scan(&n)
	return n, err
}
