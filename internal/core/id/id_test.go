package id_test

import (
	"sort"
	"testing"

	"github.com/necrogami/kanboard/internal/core/id"
)

func TestNewIsValidV7(t *testing.T) {
	s := id.New()
	if len(s) != 36 {
		t.Fatalf("len = %d, want 36: %q", len(s), s)
	}
	if !id.Valid(s) {
		t.Fatalf("Valid(%q) = false", s)
	}
	if s[14] != '7' {
		t.Fatalf("version nibble = %c, want 7", s[14])
	}
}

func TestNewIsMonotonic(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = id.New()
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatal("ids generated in sequence are not lexicographically sorted")
	}
}

func TestValidRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "abc", "123e4567-e89b-12d3-a456-426614174000"} {
		if id.Valid(s) {
			t.Errorf("Valid(%q) = true", s)
		}
	}
}
