package persistence

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
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

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &DB{db: sqlDB}, nil
}

// SQL returns the underlying *sql.DB.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the underlying connection.
func (d *DB) Close() error { return d.db.Close() }
