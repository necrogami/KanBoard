// Command board is the KanBoard server binary. This plan ships version
// and migrate; serve and the operations commands arrive in later plans.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/necrogami/kanboard/internal/server/store"
)

// Set by goreleaser: -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: board <command> [flags]")
	fmt.Fprintln(w, "  version                 print version")
	fmt.Fprintln(w, "  migrate [up|status]     apply or inspect migrations (--db, KANBOARD_DB)")
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "board %s (%s)\n", version, commit)
		return 0
	case "migrate":
		return migrate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "board: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func migrate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	db := fs.String("db", envOr("KANBOARD_DB", "kanboard.db"), "SQLite path or postgres:// DSN")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sub := "up"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, *db)
	if err != nil {
		fmt.Fprintln(stderr, "board:", err)
		return 1
	}
	defer st.Close()
	switch sub {
	case "up":
		if err := st.Migrate(ctx); err != nil {
			fmt.Fprintln(stderr, "board:", err)
			return 1
		}
		cur, latest, _ := st.Status(ctx)
		fmt.Fprintf(stdout, "migrated to %d/%d (%s)\n", cur, latest, st.Dialect)
		return 0
	case "status":
		cur, latest, err := st.Status(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "board:", err)
			return 1
		}
		fmt.Fprintf(stdout, "schema %d/%d (%s)\n", cur, latest, st.Dialect)
		return 0
	default:
		fmt.Fprintf(stderr, "board migrate: unknown subcommand %q\n", sub)
		return 2
	}
}
