package telemetry

import (
	"regexp"
	"strings"
)

// emailRegex is deliberately loose — we just want "looks like an email".
// Fully validating RFC 5322 is a trap. The server does real validation
// on signup; here we just stop the most obvious typos at install time.
var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// ValidateEmail returns true if s looks like an email address.
func ValidateEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return emailRegex.MatchString(s)
}
