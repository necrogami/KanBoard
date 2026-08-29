package keys_test

import (
	"testing"

	"github.com/necrogami/kanboard/internal/core/keys"
)

func TestValidateProjectKey(t *testing.T) {
	ok := []string{"AB", "PROJ", "A1", "ABCDEFGHIJ"}
	bad := []string{"", "A", "ab", "1AB", "A-B", "ABCDEFGHIJK", "PR OJ"}
	for _, k := range ok {
		if err := keys.ValidateProjectKey(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	for _, k := range bad {
		if err := keys.ValidateProjectKey(k); err == nil {
			t.Errorf("%q: expected error", k)
		}
	}
}

func TestCardRoundTrip(t *testing.T) {
	s := keys.Card("PROJ", 42)
	if s != "PROJ-42" {
		t.Fatalf("Card = %q", s)
	}
	p, n, err := keys.ParseCard(s)
	if err != nil || p != "PROJ" || n != 42 {
		t.Fatalf("ParseCard = %q,%d,%v", p, n, err)
	}
	for _, bad := range []string{"", "PROJ", "PROJ-", "-42", "proj-42", "PROJ-0", "PROJ-x", "PROJ-42-1"} {
		if _, _, err := keys.ParseCard(bad); err == nil {
			t.Errorf("ParseCard(%q): expected error", bad)
		}
	}
}
