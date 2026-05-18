package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Role values stored in users.role.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User is a persisted application-level account. All users run inside the same
// OS process / Linux user — this is a pseudo-permissions model, not OS-level
// isolation.
type User struct {
	ID           int
	Username     string
	PasswordHash string // PHC argon2id string; empty = no password yet
	PasswordSet  bool   // true once the operator explicitly set a password
	Role         string // RoleAdmin | RoleUser

	PermAddTorrents    bool
	PermManageRSS      bool
	PermManageCatTags  bool
	PermChangeSettings bool
	PermShare          bool

	APIKeyHash string // sha256 hex of the bearer key; empty = no key
	APIKeyHint string // last 4 chars, for display only
	Disabled   bool
	CreatedAt  time.Time
}

// Users is the DAO for the users table.
type Users struct{ db *DB }

func NewUsers(db *DB) *Users { return &Users{db: db} }

// ErrUsernameTaken is returned by Create/Update when the username collides
// with an existing account (the users.username UNIQUE constraint).
var ErrUsernameTaken = errors.New("username already taken")

const userColumns = `id, username, password_hash, password_set, role,
  perm_add_torrents, perm_manage_rss, perm_manage_cat_tags, perm_change_settings, perm_share,
  api_key_hash, api_key_hint, disabled, created_at`

func scanUser(s scanner) (User, error) {
	var u User
	var passwordSet, addT, mRSS, mCat, chg, share, disabled int
	var createdAt int64
	if err := s.Scan(&u.ID, &u.Username, &u.PasswordHash, &passwordSet, &u.Role,
		&addT, &mRSS, &mCat, &chg, &share,
		&u.APIKeyHash, &u.APIKeyHint, &disabled, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, ErrNotFound
		}
		return u, err
	}
	u.PasswordSet = passwordSet == 1
	u.PermAddTorrents = addT == 1
	u.PermManageRSS = mRSS == 1
	u.PermManageCatTags = mCat == 1
	u.PermChangeSettings = chg == 1
	u.PermShare = share == 1
	u.Disabled = disabled == 1
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// List returns every user ordered by id (so the admin seeded at id 1 is first).
func (d *Users) List(ctx context.Context) ([]User, error) {
	rows, err := d.db.SQL().QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Get returns a user by id. Returns ErrNotFound if absent.
func (d *Users) Get(ctx context.Context, id int) (User, error) {
	return scanUser(d.db.SQL().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// GetByUsername returns a user by exact username. Returns ErrNotFound if absent.
func (d *Users) GetByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(d.db.SQL().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = ?`, username))
}

// GetByAPIKeyHash returns the user owning the given sha256 key digest. Returns
// ErrNotFound if no user has that key (or hash is empty).
func (d *Users) GetByAPIKeyHash(ctx context.Context, hash string) (User, error) {
	if hash == "" {
		return User{}, ErrNotFound
	}
	return scanUser(d.db.SQL().QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE api_key_hash = ? AND api_key_hash != ''`, hash))
}

// Count returns the total number of users.
func (d *Users) Count(ctx context.Context) (int, error) {
	var n int
	err := d.db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountAdmins returns the number of enabled admin users. Used to refuse
// removing/demoting the last way into the system.
func (d *Users) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := d.db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&n)
	return n, err
}

// Create inserts a new user and returns its id. The caller supplies a
// fully-populated User (ID is ignored). Returns ErrUsernameTaken on collision.
func (d *Users) Create(ctx context.Context, u User) (int, error) {
	created := u.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	res, err := d.db.SQL().ExecContext(ctx, `
INSERT INTO users (username, password_hash, password_set, role,
  perm_add_torrents, perm_manage_rss, perm_manage_cat_tags, perm_change_settings, perm_share,
  api_key_hash, api_key_hint, disabled, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, b2i(u.PasswordSet), u.Role,
		b2i(u.PermAddTorrents), b2i(u.PermManageRSS), b2i(u.PermManageCatTags),
		b2i(u.PermChangeSettings), b2i(u.PermShare),
		u.APIKeyHash, u.APIKeyHint, b2i(u.Disabled), created.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUsernameTaken
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// Update writes username, role, permission flags and disabled state for an
// existing user. It does NOT touch password or api key — see SetPasswordHash /
// SetAPIKey for those. Returns ErrUsernameTaken on a username collision.
func (d *Users) Update(ctx context.Context, u User) error {
	_, err := d.db.SQL().ExecContext(ctx, `
UPDATE users SET username = ?, role = ?,
  perm_add_torrents = ?, perm_manage_rss = ?, perm_manage_cat_tags = ?,
  perm_change_settings = ?, perm_share = ?, disabled = ?
WHERE id = ?`,
		u.Username, u.Role,
		b2i(u.PermAddTorrents), b2i(u.PermManageRSS), b2i(u.PermManageCatTags),
		b2i(u.PermChangeSettings), b2i(u.PermShare), b2i(u.Disabled),
		u.ID)
	if err != nil && isUniqueViolation(err) {
		return ErrUsernameTaken
	}
	return err
}

// SetPasswordHash updates the stored hash and the password_set flag.
func (d *Users) SetPasswordHash(ctx context.Context, id int, hash string, userSet bool) error {
	_, err := d.db.SQL().ExecContext(ctx,
		`UPDATE users SET password_hash = ?, password_set = ? WHERE id = ?`,
		hash, b2i(userSet), id)
	return err
}

// SetAPIKey stores a new key digest + display hint (pass empty strings to
// clear). The plaintext key is never persisted.
func (d *Users) SetAPIKey(ctx context.Context, id int, hash, hint string) error {
	_, err := d.db.SQL().ExecContext(ctx,
		`UPDATE users SET api_key_hash = ?, api_key_hint = ? WHERE id = ?`,
		hash, hint, id)
	return err
}

// Delete removes a user. Their torrent_access rows are cascade-deleted by the
// foreign key; torrents they solely owned are handled by the caller.
func (d *Users) Delete(ctx context.Context, id int) error {
	_, err := d.db.SQL().ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
// modernc.org/sqlite surfaces these as errors whose message contains the
// phrase; we match on substring since it exposes no typed error here.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
