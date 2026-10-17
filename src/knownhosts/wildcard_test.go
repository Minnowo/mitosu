// Tests for the known_hosts host pattern matcher (mitosu's replacement
// for the recursive one in x/crypto).

package knownhosts

import (
	"strings"
	"testing"
	"time"
)

func TestWildcardMatch(t *testing.T) {
	cases := []struct {
		pat, str string
		want     bool
	}{
		{"", "", true},
		{"", "a", false},
		{"a", "", false},
		{"web.example.com", "web.example.com", true},
		{"web.example.com", "web.example.org", false},
		{"*.example.com", "web.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", false},
		{"web?.example.com", "web1.example.com", true},
		{"web?.example.com", "web.example.com", false},
		{"*", "anything", true},
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXbY", false},
		{"*ab", "aab", true},
		{"a**", "a", true},
		{"*a*a*a*b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
	}
	for _, c := range cases {
		if got := wildcardMatch([]byte(c.pat), []byte(c.str)); got != c.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", c.pat, c.str, got, c.want)
		}
	}
}

// The recursive matcher took exponential time in the number of '*'s; a
// fuzzed known_hosts line hung it.
func TestWildcardMatchManyStars(t *testing.T) {
	pat := []byte(strings.Repeat("*a", 2000) + "b")
	str := []byte(strings.Repeat("a", 4000))
	start := time.Now()
	if wildcardMatch(pat, str) {
		t.Fatal("matched")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
}
