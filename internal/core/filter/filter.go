// Package filter is the structured card query shared by the REST API,
// MCP card_search and the web filter bar. The text query language (0.9)
// compiles to this same struct; SQL never leaks through it.
package filter

import (
	"errors"
	"strings"
	"time"
)

const (
	DefaultLimit = 50
	MaxLimit     = 200
	MaxText      = 200
	MaxIDs       = 50
)

// Filter selects cards. Nil or empty slices mean "any".
type Filter struct {
	Text         string
	ColumnIDs    []string
	LabelIDs     []string
	AssigneeIDs  []string
	Archived     *bool
	UpdatedAfter *time.Time
	Limit        int
	Cursor       string
}

// Normalize trims text, applies the default and maximum limit, and
// rejects oversized inputs.
func (f *Filter) Normalize() error {
	f.Text = strings.TrimSpace(f.Text)
	if len(f.Text) > MaxText {
		return errors.New("filter: text longer than 200 bytes")
	}
	if f.Limit < 0 {
		return errors.New("filter: negative limit")
	}
	if f.Limit == 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit > MaxLimit {
		f.Limit = MaxLimit
	}
	for _, ids := range [][]string{f.ColumnIDs, f.LabelIDs, f.AssigneeIDs} {
		if len(ids) > MaxIDs {
			return errors.New("filter: more than 50 ids in one list")
		}
	}
	return nil
}
