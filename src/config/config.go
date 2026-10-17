// Settings for `mitosu serve` from mitosu.conf (docs/brief.md section 3,
// "Config"): strict `key = value` lines like kanshi's config.

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"mitosu/src/store"
)

// maxFileBytes caps the config file read.
const maxFileBytes = 64 * 1024

// Config holds the settings. Default() gives the documented defaults,
// used for every key the file does not set.
type Config struct {
	// Hosts are the ssh config names (aliases) to pull, in file order.
	Hosts []string
	// PullInterval is the time between pulls of a host.
	PullInterval time.Duration
	// DB is the history database file.
	DB string
	// Tiers are the storage tiers (store.ValidateTiers holds).
	Tiers []store.Tier
	// Listen is the web server's address, host:port.
	Listen string
	// AllowedHosts are extra names the browser may use in the Host
	// header, besides the listen address and localhost.
	AllowedHosts []string
	// Login is false when the web UI needs no password.
	Login bool
	// TLSCert and TLSKey are PEM files; both set means HTTPS.
	TLSCert, TLSKey string
	// LiveViews turns the live views (logs, journal, logins, sudo,
	// system, container configuration) on.
	LiveViews bool
	// LiveSudo runs docker and journalctl in live views through sudo -n.
	LiveSudo bool
}

// Default returns the defaults: no hosts, pulls every 5 minutes,
// store.DefaultPath, store.DefaultTiers, 127.0.0.1:7878, login on,
// plain HTTP, live views on without sudo.
func Default() (Config, error) {
	db, err := store.DefaultPath()
	if err != nil {
		return Config{}, err
	}
	return Config{
		PullInterval: 5 * time.Minute,
		DB:           db,
		Tiers:        append([]store.Tier(nil), store.DefaultTiers...),
		Listen:       "127.0.0.1:7878",
		Login:        true,
		LiveViews:    true,
	}, nil
}

// DefaultPath returns $XDG_CONFIG_HOME/mitosu/mitosu.conf, or
// ~/.config/mitosu/mitosu.conf.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "mitosu", "mitosu.conf"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mitosu", "mitosu.conf"), nil
}

// Load reads the config at path. A missing file gives the defaults; any
// other problem is an error naming the file (and the line, see
// LineError), so serve never runs with settings the user did not mean.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default()
	}
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return Config{}, fmt.Errorf("%s: larger than %d bytes", path, maxFileBytes)
	}
	cfg, err := Parse(string(data))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// LineError is a problem on one line of the file (counted from 1).
type LineError struct {
	Line    int
	Message string
}

func (e *LineError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}
