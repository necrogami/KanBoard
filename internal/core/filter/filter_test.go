package filter_test

import (
	"strings"
	"testing"

	"github.com/necrogami/kanboard/internal/core/filter"
)

func TestNormalizeDefaults(t *testing.T) {
	f := filter.Filter{Text: "  hello  "}
	if err := f.Normalize(); err != nil {
		t.Fatal(err)
	}
	if f.Limit != filter.DefaultLimit || f.Text != "hello" {
		t.Fatalf("normalized = %+v", f)
	}
}

func TestNormalizeCapsLimit(t *testing.T) {
	f := filter.Filter{Limit: 5000}
	if err := f.Normalize(); err != nil {
		t.Fatal(err)
	}
	if f.Limit != filter.MaxLimit {
		t.Fatalf("limit = %d", f.Limit)
	}
}

func TestNormalizeRejects(t *testing.T) {
	if err := (&filter.Filter{Limit: -1}).Normalize(); err == nil {
		t.Fatal("negative limit accepted")
	}
	if err := (&filter.Filter{Text: strings.Repeat("x", 201)}).Normalize(); err == nil {
		t.Fatal("long text accepted")
	}
	ids := make([]string, 51)
	if err := (&filter.Filter{LabelIDs: ids}).Normalize(); err == nil {
		t.Fatal("51 labels accepted")
	}
}

func TestNormalizeRejectsColumnIDsOverMax(t *testing.T) {
	ids := make([]string, 51)
	if err := (&filter.Filter{ColumnIDs: ids}).Normalize(); err == nil {
		t.Fatal("51 column ids accepted")
	}
}

func TestNormalizeRejectsAssigneeIDsOverMax(t *testing.T) {
	ids := make([]string, 51)
	if err := (&filter.Filter{AssigneeIDs: ids}).Normalize(); err == nil {
		t.Fatal("51 assignee ids accepted")
	}
}

// FuzzFilterNormalize checks that Normalize survives any input without
// panicking and is idempotent: the filter it produces normalizes to
// itself. Normalize is the boundary every adapter runs untrusted input
// through, and plan 3 normalizes once and then hands the result to the
// store, so a second pass must not change it (spec 12.2 asks for a fuzz
// target on the filter).
func FuzzFilterNormalize(f *testing.F) {
	f.Add("hello", 0, 0)
	f.Add("  padded  ", 500, 3)
	f.Add(strings.Repeat("x", 201), -1, 51)
	f.Add("", 200, 50)
	f.Fuzz(func(t *testing.T, text string, limit, ids int) {
		if ids < 0 || ids > 100 {
			return
		}
		build := func() filter.Filter {
			return filter.Filter{Text: text, Limit: limit, ColumnIDs: make([]string, ids), LabelIDs: make([]string, ids), AssigneeIDs: make([]string, ids)}
		}
		got := build()
		if err := got.Normalize(); err != nil {
			// A rejected filter must be rejected for the same reason twice.
			again := build()
			if err2 := again.Normalize(); err2 == nil || err2.Error() != err.Error() {
				t.Fatalf("second Normalize = %v, first = %v", err2, err)
			}
			return
		}
		if got.Limit < 1 || got.Limit > filter.MaxLimit {
			t.Fatalf("limit = %d after Normalize", got.Limit)
		}
		if len(got.Text) > filter.MaxText {
			t.Fatalf("text is %d bytes after Normalize", len(got.Text))
		}
		if strings.TrimSpace(got.Text) != got.Text {
			t.Fatalf("text %q is not trimmed", got.Text)
		}
		second := got
		if err := second.Normalize(); err != nil {
			t.Fatalf("normalized filter rejected on the second pass: %v", err)
		}
		if second.Text != got.Text || second.Limit != got.Limit {
			t.Fatalf("Normalize is not idempotent: %+v then %+v", got, second)
		}
	})
}
