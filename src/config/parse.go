// Parses mitosu.conf: one `key = value` per line, # comments, every key
// known, single-value keys at most once, repeatable keys (host, tier,
// allowed_host) in order.

package config

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"mitosu/src/ssh"
	"mitosu/src/store"
)

const (
	minPullInterval = 30 * time.Second
	maxPullInterval = 24 * time.Hour
	maxHosts        = 64
	maxValue        = 1024
)

// hostPattern is what a host (an ssh config name) may look like: no
// spaces, quotes or shell characters, and no leading '-'.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`)

// Parse parses a config file's text over the defaults. Tiers given in the
// file replace the default tiers as a whole.
func Parse(text string) (Config, error) {
	cfg, err := Default()
	if err != nil {
		return Config{}, err
	}
	if !utf8.ValidString(text) {
		return Config{}, fmt.Errorf("not UTF-8")
	}
	if strings.HasPrefix(text, "\xef\xbb\xbf") {
		return Config{}, fmt.Errorf("starts with a byte order mark")
	}
	seen := map[string]int{}
	var tiers []store.Tier
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, &LineError{n, "want key = value"}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			return Config{}, &LineError{n, key + " has no value"}
		}
		if len(value) > maxValue {
			return Config{}, &LineError{n, key + " value is too long"}
		}
		repeatable := key == "host" || key == "tier" || key == "allowed_host"
		if first, dup := seen[key]; dup && !repeatable {
			return Config{}, &LineError{n, fmt.Sprintf("%s is already set on line %d", key, first)}
		}
		seen[key] = n

		switch key {
		case "host":
			if !hostPattern.MatchString(value) {
				return Config{}, &LineError{n, fmt.Sprintf("host %q is not an ssh config name", value)}
			}
			for _, h := range cfg.Hosts {
				if h == value {
					return Config{}, &LineError{n, "host " + value + " is listed twice"}
				}
			}
			if len(cfg.Hosts) == maxHosts {
				return Config{}, &LineError{n, fmt.Sprintf("more than %d hosts", maxHosts)}
			}
			cfg.Hosts = append(cfg.Hosts, value)
		case "pull_interval":
			d, err := parseDuration(value)
			if err != nil || d < minPullInterval || d > maxPullInterval {
				return Config{}, &LineError{n, fmt.Sprintf("pull_interval %q: want a duration from 30s to 24h", value)}
			}
			cfg.PullInterval = d
		case "db":
			p, err := absPath(value)
			if err != nil {
				return Config{}, &LineError{n, "db: " + err.Error()}
			}
			cfg.DB = p
		case "tier":
			t, err := parseTier(value)
			if err != nil {
				return Config{}, &LineError{n, err.Error()}
			}
			tiers = append(tiers, t)
		case "listen":
			if _, _, err := net.SplitHostPort(value); err != nil {
				return Config{}, &LineError{n, fmt.Sprintf("listen %q: want host:port", value)}
			}
			cfg.Listen = value
		case "allowed_host":
			if !hostPattern.MatchString(value) || strings.Contains(value, ":") {
				return Config{}, &LineError{n, fmt.Sprintf("allowed_host %q: want a host name without port", value)}
			}
			cfg.AllowedHosts = append(cfg.AllowedHosts, value)
		case "login":
			switch value {
			case "on":
				cfg.Login = true
			case "off":
				cfg.Login = false
			default:
				return Config{}, &LineError{n, "login: want on or off"}
			}
		case "tls_cert", "tls_key":
			p, err := absPath(value)
			if err != nil {
				return Config{}, &LineError{n, key + ": " + err.Error()}
			}
			if key == "tls_cert" {
				cfg.TLSCert = p
			} else {
				cfg.TLSKey = p
			}
		default:
			return Config{}, &LineError{n, fmt.Sprintf("unknown key %q", key)}
		}
	}
	if tiers != nil {
		if err := store.ValidateTiers(tiers); err != nil {
			return Config{}, &LineError{seen["tier"], "tiers: " + err.Error()}
		}
		cfg.Tiers = tiers
	}
	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return Config{}, fmt.Errorf("tls_cert and tls_key must be set together")
	}
	return cfg, nil
}

// parseTier parses "<resolution> <keep>": resolution is raw or a
// duration, keep a duration or forever.
func parseTier(value string) (store.Tier, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return store.Tier{}, fmt.Errorf("tier %q: want <raw|duration> <duration|forever>", value)
	}
	var t store.Tier
	if fields[0] != "raw" {
		d, err := parseDuration(fields[0])
		if err != nil {
			return store.Tier{}, fmt.Errorf("tier %q: %v", value, err)
		}
		t.Resolution = d
	}
	if fields[1] != "forever" {
		d, err := parseDuration(fields[1])
		if err != nil {
			return store.Tier{}, fmt.Errorf("tier %q: %v", value, err)
		}
		t.Keep = d
	}
	return t, nil
}

// absPath expands ~ and requires an absolute path.
func absPath(value string) (string, error) {
	p, err := ssh.ExpandPath(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q is not an absolute path", value)
	}
	return filepath.Clean(p), nil
}
