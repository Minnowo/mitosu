// Tests for reading mitosu.conf: defaults, every key, and the mistakes
// that must be refused with their line.

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mitosu/src/store"
)

const fullConfig = `# mitosu.conf
host = OVH_4gb
host = backup.example.com
pull_interval = 2m
db = /srv/mitosu/history.db
tier = raw 180d
tier = 1m 2y
tier = 1h forever
listen = 0.0.0.0:8443
allowed_host = mitosu.lan
login = off
tls_cert = /etc/mitosu/cert.pem
tls_key = /etc/mitosu/key.pem
`

func TestDefaultsWhenFileIsMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hosts) != 0 || cfg.PullInterval != 5*time.Minute || cfg.Listen != "127.0.0.1:7878" || !cfg.Login ||
		cfg.TLSCert != "" || len(cfg.Tiers) != len(store.DefaultTiers) {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestEveryKey(t *testing.T) {
	cfg, err := Parse(fullConfig)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Tier{
		{Resolution: 0, Keep: 180 * 24 * time.Hour},
		{Resolution: time.Minute, Keep: 2 * 365 * 24 * time.Hour},
		{Resolution: time.Hour, Keep: 0},
	}
	if strings.Join(cfg.Hosts, ",") != "OVH_4gb,backup.example.com" || cfg.PullInterval != 2*time.Minute ||
		cfg.DB != "/srv/mitosu/history.db" || cfg.Listen != "0.0.0.0:8443" || cfg.Login ||
		strings.Join(cfg.AllowedHosts, ",") != "mitosu.lan" || cfg.TLSCert != "/etc/mitosu/cert.pem" || cfg.TLSKey != "/etc/mitosu/key.pem" {
		t.Fatalf("got %+v", cfg)
	}
	if len(cfg.Tiers) != 3 || cfg.Tiers[0] != want[0] || cfg.Tiers[1] != want[1] || cfg.Tiers[2] != want[2] {
		t.Fatalf("tiers %+v", cfg.Tiers)
	}
}

func TestHomeInPaths(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg, err := Parse("db = ~/data/m.db\n")
	if err != nil || cfg.DB != "/home/tester/data/m.db" {
		t.Fatalf("db %q, %v", cfg.DB, err)
	}
}

func TestMistakesAreRefusedWithTheirLine(t *testing.T) {
	cases := map[string]int{
		"hots = web\n":                             1,
		"host web\n":                               1,
		"host =\n":                                 1,
		"# c\nhost = -oProxyCommand=x\n":           2,
		"host = a b\n":                             1,
		"host = web\nhost = web\n":                 2,
		"pull_interval = 10s\n":                    1,
		"pull_interval = 2d\n":                     1,
		"pull_interval = 5m\npull_interval = 6m\n": 2,
		"pull_interval = 05m\n":                    1,
		"pull_interval = 5\n":                      1,
		"db = relative/m.db\n":                     1,
		"tier = raw\n":                             1,
		"tier = 1x 2d\n":                           1,
		"tier = 1m 1d\ntier = raw 2d\n":            2,
		"listen = 7878\n":                          1,
		"allowed_host = a.lan:80\n":                1,
		"login = yes\n":                            1,
	}
	for text, line := range cases {
		_, err := Parse(text)
		var le *LineError
		if !errors.As(err, &le) || le.Line != line {
			t.Errorf("%q: err %v, want an error on line %d", text, err, line)
		}
	}
	for _, text := range []string{"tls_cert = /c.pem\n", "\xff\n", "\xef\xbb\xbfhost = web\n"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}

func TestDurations(t *testing.T) {
	good := map[string]time.Duration{"30s": 30 * time.Second, "5m": 5 * time.Minute, "2h": 2 * time.Hour,
		"7d": 7 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "1y": 365 * 24 * time.Hour}
	for s, want := range good {
		if d, err := parseDuration(s); err != nil || d != want {
			t.Errorf("%s: %v %v", s, d, err)
		}
	}
	for _, s := range []string{"", "m", "0m", "-1m", "1.5h", "1M", "101y", "99999999999s", "+5m"} {
		if _, err := parseDuration(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.conf")
	if err := os.WriteFile(path, []byte(strings.Repeat("# x\n", maxFileBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("oversized file accepted")
	}
}
