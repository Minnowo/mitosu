// `mitosu check-config`: reads mitosu.conf and prints the settings serve
// would use, or the line that is wrong.

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"mitosu/src/config"
	"mitosu/src/ssh"
	"mitosu/src/store"
)

// CmdCheckConfig prints the effective settings from --config (default
// config.DefaultPath) to stdout, or returns the problem.
func CmdCheckConfig(ctx context.Context, c *cli.Command) error {
	path, cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		fmt.Printf("# %s does not exist: these are the defaults\n", path)
	} else {
		fmt.Printf("# %s is valid\n", path)
	}
	for _, h := range cfg.Hosts {
		fmt.Printf("host = %s\n", h)
	}
	if len(cfg.Hosts) == 0 {
		fmt.Println("# no host: serve needs at least one host line")
	}
	fmt.Printf("pull_interval = %s\n", formatDuration(cfg.PullInterval))
	fmt.Printf("db = %s\n", cfg.DB)
	for _, t := range cfg.Tiers {
		fmt.Printf("tier = %s\n", formatTier(t))
	}
	fmt.Printf("listen = %s\n", cfg.Listen)
	for _, h := range cfg.AllowedHosts {
		fmt.Printf("allowed_host = %s\n", h)
	}
	fmt.Printf("login = %s\n", onOff(cfg.Login))
	fmt.Printf("live_views = %s\nlive_sudo = %s\n", onOff(cfg.LiveViews), onOff(cfg.LiveSudo))
	if cfg.TLSCert != "" {
		fmt.Printf("tls_cert = %s\ntls_key = %s\n", cfg.TLSCert, cfg.TLSKey)
	}
	return nil
}

// loadConfig loads --config, or the default path, and applies --db (when
// the command has it and it is given), which overrides the file's db. A
// missing default file gives the defaults; a missing --config file is an
// error.
func loadConfig(c *cli.Command) (string, config.Config, error) {
	path := c.String("config")
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return path, config.Config{}, fmt.Errorf("--config: %w", err)
		}
	} else {
		p, err := config.DefaultPath()
		if err != nil {
			return "", config.Config{}, err
		}
		path = p
	}
	cfg, err := config.Load(path)
	if err != nil {
		return path, cfg, err
	}
	if db := c.String("db"); db != "" {
		abs, err := absPath(db)
		if err != nil {
			return path, cfg, fmt.Errorf("--db: %w", err)
		}
		cfg.DB = abs
	}
	return path, cfg, nil
}

// absPath expands ~ and makes path absolute.
func absPath(path string) (string, error) {
	p, err := ssh.ExpandPath(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// formatTier writes a tier the way the config file does.
func formatTier(t store.Tier) string {
	res := "raw"
	if t.Resolution != 0 {
		res = formatDuration(t.Resolution)
	}
	keep := "forever"
	if t.Keep != 0 {
		keep = formatDuration(t.Keep)
	}
	return res + " " + keep
}

// formatDuration writes d in the largest config unit that divides it
// (weeks are written as days).
func formatDuration(d time.Duration) string {
	units := []struct {
		suffix string
		size   time.Duration
	}{
		{"y", 365 * 24 * time.Hour}, {"d", 24 * time.Hour},
		{"h", time.Hour}, {"m", time.Minute}, {"s", time.Second},
	}
	for _, u := range units {
		if d%u.size == 0 {
			return fmt.Sprintf("%d%s", d/u.size, u.suffix)
		}
	}
	return strings.TrimSpace(d.String())
}

// onOff writes a switch the way the config file does.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
