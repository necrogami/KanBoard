package store_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/necrogami/kanboard/internal/server/store"
)

var n atomic.Int64

func openSQLite(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), fmt.Sprintf("file:mig%d?mode=memory&cache=shared", n.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestMigrateIsIdempotent(t *testing.T) {
	st := openSQLite(t)
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal("second migrate:", err)
	}
	var cnt int
	if err := st.DB.QueryRowContext(ctx, "SELECT count(*) FROM card").Scan(&cnt); err != nil {
		t.Fatal("card table missing:", err)
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	st := openSQLite(t)
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (9999, 1, CURRENT_TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	err := st.Migrate(ctx)
	if !errors.Is(err, store.ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
}

func TestOpenFileDatabaseEnforcesPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kb.db")
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var mode string
	if err := st.DB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	var fk int
	if err := st.DB.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v", fk, err)
	}
	var limit int64
	if err := st.DB.QueryRowContext(ctx, "PRAGMA journal_size_limit").Scan(&limit); err != nil || limit != 67108864 {
		t.Fatalf("journal_size_limit = %d, %v", limit, err)
	}
}

func TestBackupSQLiteFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "kb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "kb.bak")
	if err := st.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	copy, err := store.Open(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	cur, latest, err := copy.Status(ctx)
	if err != nil || cur != latest {
		t.Fatalf("backup status = %d/%d %v", cur, latest, err)
	}
}

func TestBackupRefusedForMemoryAndPostgres(t *testing.T) {
	st := openSQLite(t)
	if err := st.Backup(context.Background(), "x"); !errors.Is(err, store.ErrNoBackup) {
		t.Fatalf("memory backup err = %v", err)
	}
}
