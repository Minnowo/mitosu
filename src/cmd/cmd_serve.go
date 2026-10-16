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

	"mitosu/src/auth"
	"mitosu/src/pull"
	"mitosu/src/scheduler"
	"mitosu/src/ssh"
	"mitosu/src/store"
	"mitosu/src/web"
	webui "mitosu/web"
)

// CmdServe loads the config (--config), opens the history database, pulls
// every configured host and serves the web API until SIGINT or SIGTERM.
// With login on (the default, unless login = off or --no-login) it
// refuses to start until a password is set with `mitosu passwd`.
func CmdServe(ctx context.Context, c *cli.Command) error {
	path, cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if len(cfg.Hosts) == 0 {
		return fmt.Errorf("%s: no host to pull; add a line such as: host = myserver", path)
	}
	var verifier *auth.Verifier
	if cfg.Login && !c.Bool("no-login") {
		verifier, err = auth.ReadFile(auth.PasswordPath(cfg.DB))
		if errors.Is(err, auth.ErrNoPassword) {
			return errors.New("login is on but no password is set: run `mitosu passwd`, or start with --no-login (or set login = off)")
		}
		if err != nil {
			return err
		}
	}
	db, err := store.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal, a second one gets the default behaviour
	// (the process ends at once).
	go func() {
		<-ctx.Done()
		stop()
	}()

	sched := scheduler.New(db, cfg.Hosts, cfg.Tiers, cfg.PullInterval, connectHost, pull.Pull)
	ui, err := webui.Files()
	if err != nil {
		return err
	}
	server := web.New(db, sched, web.Options{
		Listen: cfg.Listen, AllowedHosts: cfg.AllowedHosts, Verifier: verifier,
		TLSCert: cfg.TLSCert, TLSKey: cfg.TLSKey, Tiers: cfg.Tiers,
		PullInterval: cfg.PullInterval, UI: ui,
	})

	listener, err := server.Listen()
	if err != nil {
		return fmt.Errorf("web server on %s: %w", cfg.Listen, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scheme := "http"
	if cfg.TLSCert != "" {
		scheme = "https"
	}
	log.Info().Strs("hosts", cfg.Hosts).Dur("interval", cfg.PullInterval).Str("db", cfg.DB).
		Str("url", scheme+"://"+cfg.Listen).Bool("login", verifier != nil).Msg("Serving")

	pulled := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(pulled)
	}()
	serveErr := server.Serve(ctx, listener)
	// The web server ends on a signal or because it could not listen;
	// either way the pulls stop too.
	cancel()
	<-pulled
	if serveErr != nil {
		return fmt.Errorf("web server on %s: %w", cfg.Listen, serveErr)
	}
	log.Info().Msg("Stopped")
	return nil
}

// connectHost connects to a configured host by its ssh config name, never
// prompting (serve runs unattended): keys come from the agent or
// unencrypted identity files.
func connectHost(ctx context.Context, host string) (scheduler.Conn, error) {
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
	if err := client.ConnectContext(ctx); err != nil {
		return nil, err
	}
	if client.Client == nil {
		return nil, errors.New("no connection")
	}
	return client, nil
}
