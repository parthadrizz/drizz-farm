package telemetry

import "testing"

func TestValidateEmail(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"partha@drizz.ai", true},
		{"a@b.co", true},
		{"first.last+tag@example.com", true},
		{"  spaces@example.com  ", true}, // trimmed
		{"", false},
		{"noatsign", false},
		{"@nodomain", false},
		{"no.tld@example", false},
		{"two@@signs.com", false},
		{"spaces in@example.com", false},
	}
	for _, c := range cases {
		if got := ValidateEmail(c.in); got != c.want {
			t.Errorf("ValidateEmail(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
