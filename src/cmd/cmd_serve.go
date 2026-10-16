// `mitosu serve`: the long-running server mode (docs/brief.md section 3,
// "Server mode"). Runs in the foreground anywhere, logs to stderr and
// stops cleanly on SIGINT or SIGTERM.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"mitosu/src/pull"
	"mitosu/src/scheduler"
	"mitosu/src/ssh"
	"mitosu/src/store"
)

// CmdServe loads the config (--config), opens the history database and
// pulls every configured host until SIGINT or SIGTERM.
func CmdServe(ctx context.Context, c *cli.Command) error {
	path, cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if len(cfg.Hosts) == 0 {
		return fmt.Errorf("%s: no host to pull; add a line such as: host = myserver", path)
	}
	db, err := store.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	sched := scheduler.New(db, cfg.Hosts, cfg.Tiers, cfg.PullInterval, connectHost, pull.Pull)
	log.Info().Strs("hosts", cfg.Hosts).Dur("interval", cfg.PullInterval).Str("db", cfg.DB).Msg("Serving")
	sched.Run(ctx)
	log.Info().Msg("Stopped")
	return nil
}

// connectHost connects to a configured host by its ssh config name, never
// prompting (serve runs unattended): keys come from the agent or
// unencrypted identity files.
func connectHost(host string) (scheduler.Conn, error) {
	files, err := ssh.DefaultConfigFiles()
	if err != nil {
		return nil, err
	}
	section, warnings, err := ssh.Resolve(files, host, nil)
	for _, w := range warnings {
		log.Warn().Str("host", host).Msg(w)
	}
	if err != nil {
		return nil, err
	}
	client := &ssh.SSHClient{Config: section, Passwords: ssh.SSHPasswords{CanPrompt: false}}
	if err := client.Connect(); err != nil {
		return nil, err
	}
	if client.Client == nil {
		return nil, errors.New("no connection")
	}
	return client, nil
}
