// Splits one ssh_config line into its keyword and arguments
// (ssh_config(5), DESCRIPTION).

package ssh

import (
	"errors"
	"strings"
)

// splitConfigLine returns the lower-case keyword and the arguments of one
// config line, or "" for an empty or comment line. The keyword is
// separated from its arguments by whitespace or one '=' with optional
// whitespace. Arguments are separated by whitespace and may be enclosed in
// double quotes; an argument starting with '#' ends the line.
func splitConfigLine(line string) (string, []string, error) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", nil, nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return "", nil, errors.New("keyword without an argument")
	}
	keyword := strings.ToLower(line[:end])
	rest := strings.TrimLeft(line[end:], " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}

	var args []string
	for rest != "" {
		if rest[0] == '#' {
			break
		}
		var arg string
		if rest[0] == '"' {
			close := strings.IndexByte(rest[1:], '"')
			if close < 0 {
				return "", nil, errors.New("unterminated quote")
			}
			arg = rest[1 : 1+close]
			rest = rest[2+close:]
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			arg = rest[:end]
			rest = rest[end:]
		}
		args = append(args, arg)
		rest = strings.TrimLeft(rest, " \t")
	}
	if len(args) == 0 {
		return "", nil, errors.New("keyword without an argument")
	}
	return keyword, args, nil
}
