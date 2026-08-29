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
