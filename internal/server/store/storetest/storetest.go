// Package storetest opens throwaway databases for tests: a fresh
// in-memory SQLite database per test, and a fresh Postgres schema per
// test when KANBOARD_TEST_PG_DSN is set.
package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/necrogami/kanboard/internal/server/store"
)

var counter atomic.Int64

// Each runs fn against SQLite and, when configured, Postgres.
func Each(t *testing.T, fn func(t *testing.T, st *store.Store)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		fn(t, OpenSQLite(t))
	})
	dsn := os.Getenv("KANBOARD_TEST_PG_DSN")
	if dsn == "" {
		return
	}
	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		fn(t, OpenPostgres(t, dsn))
	})
}

// OpenSQLite returns a migrated in-memory database private to this test.
func OpenSQLite(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("kbtest%d", counter.Add(1))
	st, err := store.Open(ctx, "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return st
}

// OpenPostgres returns a migrated store bound to a fresh schema that is
// dropped when the test ends.
func OpenPostgres(t *testing.T, dsn string) *store.Store {
	t.Helper()
	ctx := context.Background()
	schema := fmt.Sprintf("t_%d_%d", os.Getpid(), counter.Add(1))
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	st, err := store.Open(ctx, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		_, _ = base.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = base.Close()
	})
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return st
}
