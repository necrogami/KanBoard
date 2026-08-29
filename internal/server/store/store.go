// Package store opens the database, applies migrations and exposes the
// engine-neutral Querier. SQLite is the default; Postgres is selected by
// a postgres:// DSN.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/necrogami/kanboard/migrations"
)

// Dialect names the SQL engine in use.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// ErrSchemaNewer means the database was migrated by a newer binary.
var ErrSchemaNewer = errors.New("store: database schema is newer than this binary")

// ErrNoBackup means Backup is not possible for this database (in-memory
// SQLite or Postgres, which is backed up with pg_dump; see docs/backup.md).
var ErrNoBackup = errors.New("store: backup is only supported for file-based sqlite databases")

// ErrIntegrity means PRAGMA integrity_check reported corruption.
var ErrIntegrity = errors.New("store: sqlite integrity check failed")

// Store is an open database.
type Store struct {
	DB      *sql.DB
	Dialect Dialect
	path    string // sqlite file path, empty for memory databases and postgres
}

// Open connects and pings. A DSN starting with postgres:// or
// postgresql:// selects Postgres; anything else is a SQLite path or
// file: URI, to which the standard pragmas are appended.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(16)
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: postgres ping: %w", err)
		}
		return &Store{DB: db, Dialect: Postgres}, nil
	}
	db, err := sql.Open("sqlite", sqliteDSN(dsn))
	if err != nil {
		return nil, err
	}
	memory := isMemoryDSN(dsn)
	if memory {
		db.SetMaxOpenConns(1)
	} else {
		// One connection writes at a time (BEGIN IMMEDIATE); the others
		// serve reads under WAL. Four is the bounded pool from spec 5.2.
		db.SetMaxOpenConns(4)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: sqlite ping: %w", err)
	}
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: integrity check: %w", err)
	}
	if check != "ok" {
		db.Close()
		return nil, fmt.Errorf("%w: %s", ErrIntegrity, check)
	}
	st := &Store{DB: db, Dialect: SQLite, path: sqlitePath(dsn)}
	return st, nil
}

// isMemoryDSN reports whether dsn refers to an in-memory SQLite database,
// in any of the forms sqlite accepts: a bare ":memory:", a "file::memory:"
// URI (with or without a query string), or a "mode=memory" query
// parameter on a named DSN.
func isMemoryDSN(dsn string) bool {
	if dsn == ":memory:" {
		return true
	}
	if strings.HasPrefix(dsn, "file::memory:") {
		return true
	}
	return strings.Contains(dsn, "mode=memory")
}

func sqliteDSN(dsn string) string {
	if !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(67108864)&_txlock=immediate"
}

// sqlitePath extracts the file path from a plain path or file: URI. It
// returns "" for a memory DSN, which has no backing file.
func sqlitePath(dsn string) string {
	if isMemoryDSN(dsn) {
		return ""
	}
	dsn = strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		dsn = dsn[:i]
	}
	return dsn
}

// Backup writes a consistent copy of a file-based SQLite database to
// dest using VACUUM INTO (an online backup that does not block writers
// for long). Migrate calls it before applying migrations; the
// operations commands (plan 7) call it for scheduled backups.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if s.Dialect != SQLite || s.path == "" {
		return ErrNoBackup
	}
	// VACUUM INTO takes a string expression; a bound parameter is allowed.
	if _, err := s.DB.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("store: backup: %w", err)
	}
	return nil
}

func (s *Store) provider() (*goose.Provider, error) {
	dialect, dir := goose.DialectSQLite3, "sqlite"
	if s.Dialect == Postgres {
		dialect, dir = goose.DialectPostgres, "postgres"
	}
	sub, err := fs.Sub(migrations.FS, dir)
	if err != nil {
		return nil, err
	}
	p, err := goose.NewProvider(dialect, s.DB, sub)
	if err != nil {
		return nil, fmt.Errorf("store: migrations: %w", err)
	}
	return p, nil
}

// Status returns the applied and the latest embedded migration version.
func (s *Store) Status(ctx context.Context) (current, latest int64, err error) {
	p, err := s.provider()
	if err != nil {
		return 0, 0, err
	}
	current, err = p.GetDBVersion(ctx)
	if err != nil {
		return 0, 0, err
	}
	srcs := p.ListSources()
	if len(srcs) == 0 {
		return current, 0, errors.New("store: no embedded migrations")
	}
	return current, srcs[len(srcs)-1].Version, nil
}

// Migrate applies pending migrations. It refuses to run against a
// database whose version is newer than the embedded migrations, and
// backs up a file-based SQLite database first (spec 5.2).
func (s *Store) Migrate(ctx context.Context) error {
	current, latest, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if current > latest {
		return fmt.Errorf("%w: database at %d, binary knows %d", ErrSchemaNewer, current, latest)
	}
	if current > 0 && current < latest && s.path != "" {
		dest := fmt.Sprintf("%s.pre-migrate-%d.bak", s.path, current)
		if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("store: remove stale backup: %w", err)
		}
		if err := s.Backup(ctx, dest); err != nil {
			return err
		}
	}
	p, err := s.provider()
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }
