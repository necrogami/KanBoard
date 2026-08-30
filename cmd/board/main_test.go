package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "board ") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestMigrateUpAndStatus(t *testing.T) {
	db := filepath.Join(t.TempDir(), "kb.db")
	var out, errb bytes.Buffer
	if code := run([]string{"migrate", "--db", db, "status"}, &out, &errb); code != 0 {
		t.Fatalf("status before: exit %d: %s", code, errb.String())
	}
	if cur, latest := parseStatus(t, out.String()); cur != 0 || latest < 1 {
		t.Fatalf("status before = %q", out.String())
	}
	out.Reset()
	if code := run([]string{"migrate", "--db", db, "up"}, &out, &errb); code != 0 {
		t.Fatalf("up: exit %d: %s", code, errb.String())
	}
	out.Reset()
	if code := run([]string{"migrate", "--db", db, "status"}, &out, &errb); code != 0 {
		t.Fatal(errb.String())
	}
	if cur, latest := parseStatus(t, out.String()); cur != latest {
		t.Fatalf("status after = %q", out.String())
	}
}

// parseStatus reads "schema <cur>/<latest> (<dialect>)".
func parseStatus(t *testing.T, s string) (int64, int64) {
	t.Helper()
	var cur, latest int64
	var dialect string
	if _, err := fmt.Sscanf(s, "schema %d/%d (%s", &cur, &latest, &dialect); err != nil {
		t.Fatalf("cannot parse %q: %v", s, err)
	}
	return cur, latest
}

func TestUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"nope"}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d", code)
	}
}

// TestMigrateFlagsAfterSubcommand: flag.Parse stops at the first
// non-flag argument, so "migrate up --db X" used to migrate the
// default database and silently ignore the flag.
func TestMigrateFlagsAfterSubcommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "after.db")
	var out, errb bytes.Buffer
	if code := run([]string{"migrate", "up", "--db", db}, &out, &errb); code != 0 {
		t.Fatalf("up: exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "migrated to") {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("--db after the subcommand was ignored: %v", err)
	}
	out.Reset()
	if code := run([]string{"migrate", "status", "--db", db}, &out, &errb); code != 0 {
		t.Fatal(errb.String())
	}
	if cur, latest := parseStatus(t, out.String()); cur != latest || cur == 0 {
		t.Fatalf("status = %q; the --db after the subcommand was ignored", out.String())
	}
}

func TestUnknownMigrateSubcommandPrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"migrate", "bogus", "--db", filepath.Join(t.TempDir(), "x.db")}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errb.String(), "usage:") {
		t.Fatalf("stderr = %q, want the usage text", errb.String())
	}
}
