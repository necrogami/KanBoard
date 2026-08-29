package order_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/necrogami/kanboard/internal/core/order"
)

func TestBetweenExamples(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "", "V"},
		{"V", "", "k"},
		{"", "V", "F"},
		{"a", "b", "aV"},
		{"aV", "b", "ak"},
		{"", "1", "0V"},
		{"", "01", "00V"},
		{"A", "A1", "A0V"},
	}
	for _, c := range cases {
		got, err := order.Between(c.a, c.b)
		if err != nil {
			t.Fatalf("Between(%q,%q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Between(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestBetweenRejectsBadInput(t *testing.T) {
	if _, err := order.Between("b", "a"); err != order.ErrOrder {
		t.Errorf("reversed: err = %v", err)
	}
	if _, err := order.Between("a", "a"); err != order.ErrOrder {
		t.Errorf("equal: err = %v", err)
	}
	if _, err := order.Between("a0", ""); err != order.ErrKey {
		t.Errorf("trailing zero: err = %v", err)
	}
	if _, err := order.Between("a-b", ""); err != order.ErrKey {
		t.Errorf("bad char: err = %v", err)
	}
}

func TestRepeatedAppendAndPrependStayOrdered(t *testing.T) {
	var keys []string
	last := ""
	for i := 0; i < 500; i++ {
		k, err := order.Between(last, "")
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
		last = k
	}
	first := keys[0]
	for i := 0; i < 500; i++ {
		k, err := order.Between("", first)
		if err != nil {
			t.Fatal(err)
		}
		keys = append([]string{k}, keys...)
		first = k
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("keys not sorted after append and prepend")
	}
	for _, k := range keys {
		if strings.HasSuffix(k, "0") {
			t.Fatalf("key %q has trailing zero", k)
		}
	}
}

func TestRebalance(t *testing.T) {
	for _, n := range []int{1, 2, 10, 61, 62, 63, 1000} {
		keys := order.Rebalance(n)
		if len(keys) != n {
			t.Fatalf("Rebalance(%d) returned %d keys", n, len(keys))
		}
		for i := 1; i < n; i++ {
			if keys[i-1] >= keys[i] {
				t.Fatalf("Rebalance(%d): keys[%d]=%q >= keys[%d]=%q", n, i-1, keys[i-1], i, keys[i])
			}
		}
		for _, k := range keys {
			if k == "" || strings.HasSuffix(k, "0") {
				t.Fatalf("Rebalance(%d): bad key %q", n, k)
			}
		}
	}
	if order.Rebalance(0) != nil {
		t.Fatal("Rebalance(0) should be nil")
	}
}

func FuzzBetween(f *testing.F) {
	f.Add("", "")
	f.Add("a", "b")
	f.Add("", "0V")
	f.Add("Zz", "a")
	f.Fuzz(func(t *testing.T, a, b string) {
		got, err := order.Between(a, b)
		if err != nil {
			return
		}
		if a != "" && got <= a {
			t.Fatalf("Between(%q,%q)=%q not > a", a, b, got)
		}
		if b != "" && got >= b {
			t.Fatalf("Between(%q,%q)=%q not < b", a, b, got)
		}
		if got == "" || strings.HasSuffix(got, "0") {
			t.Fatalf("Between(%q,%q)=%q invalid", a, b, got)
		}
	})
}
