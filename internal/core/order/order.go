// Package order implements fractional indexing over base-62 strings.
// A key is compared bytewise; the alphabet is chosen so that ASCII order
// equals digit order. Keys never end in '0' so that every key has room
// after it, and the empty string is the open bound on either side.
package order

import (
	"errors"
	"strings"
)

// Alphabet lists the digits in ascending order.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// MaxKeyLen is the length past which a container is rebalanced (spec 4.4).
const MaxKeyLen = 64

var (
	// ErrOrder means the left bound is not strictly less than the right.
	ErrOrder = errors.New("order: left key must be less than right key")
	// ErrKey means a key contains a character outside Alphabet or ends in '0'.
	ErrKey = errors.New("order: invalid key")
)

func valid(k string) bool {
	if k == "" {
		return true
	}
	if strings.HasSuffix(k, "0") {
		return false
	}
	for i := 0; i < len(k); i++ {
		if strings.IndexByte(Alphabet, k[i]) < 0 {
			return false
		}
	}
	return true
}

// Between returns a key strictly between a and b. An empty a means
// "before everything" and an empty b means "after everything".
func Between(a, b string) (string, error) {
	if !valid(a) || !valid(b) {
		return "", ErrKey
	}
	if b != "" && a >= b {
		return "", ErrOrder
	}
	return mid(a, b), nil
}

func at(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return '0'
}

func mid(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) && at(a, n) == b[n] {
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + mid(rest, b[n:])
		}
	}
	da := 0
	if a != "" {
		da = strings.IndexByte(Alphabet, a[0])
	}
	db := len(Alphabet)
	if b != "" {
		db = strings.IndexByte(Alphabet, b[0])
	}
	if db-da > 1 {
		return string(Alphabet[(da+db)/2])
	}
	if len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(Alphabet[da]) + mid(rest, "")
}

// Rebalance returns n evenly spaced keys of minimal equal length, sorted
// ascending. Used by the rank.rebalance job when keys grow too long.
func Rebalance(n int) []string {
	if n <= 0 {
		return nil
	}
	base := int64(len(Alphabet))
	l := 1
	span := base
	for span < int64(n)+2 {
		span *= base
		l++
	}
	keys := make([]string, n)
	for i := 1; i <= n; i++ {
		v := int64(i) * span / int64(n+1)
		keys[i-1] = strings.TrimRight(encode(v, l), "0")
	}
	return keys
}

func encode(v int64, l int) string {
	buf := make([]byte, l)
	base := int64(len(Alphabet))
	for i := l - 1; i >= 0; i-- {
		buf[i] = Alphabet[v%base]
		v /= base
	}
	return string(buf)
}
