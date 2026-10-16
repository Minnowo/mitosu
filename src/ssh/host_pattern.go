// Matches host names and user names against ssh_config patterns
// (ssh_config(5), PATTERNS).

package ssh

import "strings"

// hostMatches reports whether name matches the Host patterns: no negated
// pattern ('!') matches it and at least one plain pattern does. Matching
// ignores case.
func hostMatches(patterns []string, name string) bool {
	name = strings.ToLower(name)
	matched := false
	for _, p := range patterns {
		p = strings.ToLower(p)
		if strings.HasPrefix(p, "!") {
			if wildcardMatch(p[1:], name) {
				return false
			}
			continue
		}
		if wildcardMatch(p, name) {
			matched = true
		}
	}
	return matched
}

// wildcardMatch matches s against pattern, where '*' matches any run of
// characters and '?' exactly one. It runs in O(len(pattern)*len(s)) at
// worst (single backtrack point), never exponentially.
func wildcardMatch(pattern, s string) bool {
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[i]):
			p++
			i++
		case p < len(pattern) && pattern[p] == '*':
			star = p
			mark = i
			p++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// patternListMatches reports whether s matches the comma-separated
// pattern-list list: no negated pattern matches it and at least one plain
// pattern does. Matching ignores case.
func patternListMatches(list, s string) bool {
	return hostMatches(strings.Split(list, ","), s)
}
