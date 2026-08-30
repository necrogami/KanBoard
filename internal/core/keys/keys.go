// Package keys defines project keys (PROJ) and card keys (PROJ-42).
package keys

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// ErrProjectKey is returned for keys that are not 2-10 uppercase
// alphanumerics starting with a letter.
var ErrProjectKey = errors.New("keys: project key must be 2-10 uppercase letters or digits, starting with a letter")

// ErrCardKey is returned for strings that are not PROJ-N with N >= 1.
var ErrCardKey = errors.New("keys: card key must look like PROJ-42")

// ValidateProjectKey checks the project key format.
func ValidateProjectKey(k string) error {
	if !projectKeyRe.MatchString(k) {
		return ErrProjectKey
	}
	return nil
}

// Card builds the display key for card number n in project.
func Card(project string, n int64) string {
	return project + "-" + strconv.FormatInt(n, 10)
}

// ParseCard splits PROJ-42 into its project key and number.
func ParseCard(s string) (string, int64, error) {
	i := strings.LastIndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return "", 0, ErrCardKey
	}
	project, num := s[:i], s[i+1:]
	if err := ValidateProjectKey(project); err != nil {
		return "", 0, ErrCardKey
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 1 {
		return "", 0, ErrCardKey
	}
	return project, n, nil
}
