// Package id generates and validates UUIDv7 identifiers. Every entity in
// KanBoard uses one as its primary key; v7 keeps them time-ordered so
// inserts are append-friendly and offline clients can mint ids safely.
package id

import "github.com/google/uuid"

// New returns a new UUIDv7 as a lower-case 36-character string.
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}

// Valid reports whether s is a canonical 36-character UUIDv7 string.
func Valid(s string) bool {
	if len(s) != 36 {
		return false
	}
	u, err := uuid.Parse(s)
	return err == nil && u.Version() == 7
}
