// Package migrations embeds the goose SQL migrations for both engines.
// Files under sqlite/ and postgres/ share version numbers and column
// order; only the type keywords differ.
package migrations

import "embed"

//go:embed sqlite/*.sql postgres/*.sql
var FS embed.FS
