# KanBoard

A fully open source kanban board, written in Go end to end: server,
web UI, native desktop clients (Linux, macOS, Windows), native mobile
clients (Android, iOS), and a built-in MCP server so AI agents can work
the board through the same permissions and activity log as people.

Kanban first. Jira-like structure (keys, hierarchy, custom fields, query
language, reports, optional workflows) is layered on version by version
without ever getting in the way of a plain board.

## Status

Pre-release. Version 0.1 is being built. Nothing here is usable yet.

Versioning is semver 0.x with no 1.0 planned: 0.9 is followed by 0.10,
0.11, and so on. Each minor release stacks features on the last.

## Principles

- One edition, no tiers, no telemetry, export always available. See
  [COMMITMENT.md](COMMITMENT.md).
- Single binary. SQLite or Postgres. One Docker image.
- Native means native: no HTML wrapped in a desktop window.
- Every mutation, by a person or an agent, goes through one service
  layer, one authorization check, and one append-only activity log.
- Translation-ready from the first release; English is the first locale.

## Building

Not yet. The toolchain (`just`, Go 1.25+, sqlc, templ, goose, fyne) and
the `justfile` arrive with the first code.

## License

[AGPL-3.0-only](LICENSE). Contributions are accepted under the Developer
Certificate of Origin (sign your commits with `git commit -s`).
