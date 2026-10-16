// Expands '~', ${VAR} and %-tokens in ssh_config paths (ssh_config(5),
// TOKENS and ENVIRONMENT VARIABLES).

package ssh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands a leading "~" or "~/" in path to the home directory.
// Other paths are returned unchanged. It returns an error if the home
// directory is needed but unknown, or for "~user" forms.
func ExpandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~") {
		return path, nil
	}
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return "", fmt.Errorf("%q: ~user paths are not supported", path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// expandPath expands '~', then ${VAR}, then %-tokens in a config path.
func expandPath(path string, tokens map[byte]string) (string, error) {
	p, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	p, err = expandEnv(p)
	if err != nil {
		return "", err
	}
	return expandTokens(p, tokens)
}

// expandEnv replaces each ${NAME} with the environment variable NAME. An
// unset variable or an unterminated "${" is an error, as in OpenSSH.
func expandEnv(s string) (string, error) {
	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		end := strings.IndexByte(s[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated ${ in %q", s)
		}
		name := s[start+2 : start+end]
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("environment variable %q is not set", name)
		}
		b.WriteString(s[:start])
		b.WriteString(value)
		s = s[start+end+1:]
	}
}

// expandTokens replaces %% with % and %c with tokens[c]. A token not in
// tokens is an error naming it.
func expandTokens(s string, tokens map[byte]string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("%q ends with a lone %%", s)
		}
		i++
		if s[i] == '%' {
			b.WriteByte('%')
			continue
		}
		value, ok := tokens[s[i]]
		if !ok {
			return "", fmt.Errorf("token %%%c is not supported", s[i])
		}
		b.WriteString(value)
	}
	return b.String(), nil
}
