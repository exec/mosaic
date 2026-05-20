package persistence

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB is a thin wrapper exposing a *sql.DB to consumers and owning teardown.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs all pending
// goose migrations from the embedded migrations dir. WAL mode is enabled so
// concurrent reads don't block writes.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Bound the connection pool. This is a single-file embedded SQLite
	// database in WAL mode: WAL allows many concurrent readers but still
	// serializes writers, so an unbounded pool (database/sql's default) only
	// invites "database is locked" contention and wasted file descriptors
	// under load. A small fixed pool is the right shape here.
	//   MaxOpenConns(8)  — enough parallelism for the readers (per-user WS
	//                      ticks, HTTP handlers); writers still serialize and
	//                      wait on busy_timeout rather than erroring out.
	//   MaxIdleConns(8)  — keep the whole pool warm; opening a sqlite conn is
	//                      cheap but pointless churn for a long-lived daemon.
	//   ConnMaxLifetime(1h) — recycle connections occasionally so a leaked
	//                      per-connection pragma or memory growth can't
	//                      accumulate for the lifetime of the process.
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// PingContext above forced the connection open, which in turn caused
	// sqlite to lazily create the database file at `path`. Tighten its mode
	// to 0600 before any sensitive data lands in it: this DB holds
	// argon2id password hashes (users.password_hash), API-key SHA-256s
	// (users.api_key_hash), and stored web-interface credentials. On a
	// multi-user host the default umask leaves the file world-readable,
	// which means any local account can read those hashes and run them
	// through hashcat at leisure.
	//
	// WAL mode also lays down `<path>-wal` and `<path>-shm` sibling files
	// the first time a write happens; they pick up the same default umask,
	// so we chmod them too. They may not exist yet on a brand-new
	// database (WAL gets created on the first write, not on open), so a
	// missing-file error is expected and silently ignored.
	//
	// Chmod failures are LOGGED, not fatal. The file might be on a
	// filesystem that doesn't support unix modes (network FS, FAT), or
	// the process might not have permission to chmod it; in either case
	// the daemon is still functional with looser permissions, and we'd
	// rather log a warning than refuse to start. The Linux daemon
	// deployment path (which is the multi-user surface that actually
	// matters here) runs on ext4/xfs/btrfs and won't hit this fallback.
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		log.Printf("persistence: chmod %s 0600 failed: %v (db is functional but file permissions are looser than recommended on a multi-user host)", path, err)
	}
	for _, sibling := range []string{path + "-wal", path + "-shm"} {
		if err := os.Chmod(sibling, 0o600); err != nil && !os.IsNotExist(err) {
			log.Printf("persistence: chmod %s 0600 failed: %v", sibling, err)
		}
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// Re-chmod after migrations: the first WRITE to a WAL-mode SQLite
	// creates `<path>-wal` and `<path>-shm` if they didn't already exist.
	// Goose's UpContext runs migrations whose writes produce both sibling
	// files on a fresh database, so the post-migration pass is what
	// actually catches them. Pre-existing files (subsequent process
	// starts) are also re-chmod'd, which is idempotent.
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !os.IsNotExist(err) {
			log.Printf("persistence: chmod %s 0600 failed: %v", p, err)
		}
	}

	return &DB{db: sqlDB}, nil
}

// SQL returns the underlying *sql.DB.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the underlying connection.
func (d *DB) Close() error { return d.db.Close() }
