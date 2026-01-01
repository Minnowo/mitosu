package shell

import "strings"

func escapeSingleQuotes(s string) string {
	// POSIX-safe single-quote escaping:
	// 'foo'"'"'bar'
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "'", `'"'"'`)
}
