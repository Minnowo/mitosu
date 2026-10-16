package main

import (
	"context"
	"mitosu/src/cmd"
	"mitosu/src/data"
	"mitosu/src/logger"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func main() {

	logger.Init(zerolog.DebugLevel)

	cmd := &cli.Command{
		Name:        "mitosu",
		Description: "A simple pure SSH server monitoring tool.",
		Usage:       "See through your servers at a glance",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "log-level",
				Usage:    "Set the log level, ranges from 0 (debug) - 5 (panic). All logging happens on stderr.",
				Value:    1,
				Required: false,
			},
		},
		Before: logger.CliInit,
		Commands: []*cli.Command{
			{
				Name:        "kanshi",
				Usage:       "Read the history kanshi keeps on a server",
				Description: "Run kanshi requests over SSH and check the responses (protocol 1.x).",
				Commands: []*cli.Command{
					{
						Name:   "version",
						Usage:  "Show the server's kanshi and protocol versions",
						Flags:  connectionFlags(),
						Action: cmd.CmdKanshiVersion,
					},
					{
						Name:  "export",
						Usage: "Print the checked samples after --since (JSON lines) and a summary on stderr",
						Flags: append(connectionFlags(),
							&cli.Uint64Flag{
								Name:  "since",
								Usage: "Return samples with a sequence number above this.",
								Value: 0,
							},
							&cli.Uint64Flag{
								Name:  "limit",
								Usage: "Return at most this many samples (up to 1000000; 0 means no limit).",
								Value: 100,
							},
						),
						Action: cmd.CmdKanshiExport,
					},
				},
			},
			{
				Name:  "check-config",
				Usage: "Check mitosu.conf and print the settings serve would use",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "config",
						Usage: "The config file. Default: $XDG_CONFIG_HOME/mitosu/mitosu.conf, else ~/.config/mitosu/mitosu.conf.",
					},
				},
				Action: cmd.CmdCheckConfig,
			},
			{
				Name:  "pull",
				Usage: "Store a server's new kanshi samples in the local history",
				Flags: append(connectionFlags(),
					&cli.StringFlag{
						Name:  "db",
						Usage: "The history database. Default: $XDG_DATA_HOME/mitosu/mitosu.db, else ~/.local/share/mitosu/mitosu.db.",
					},
				),
				Action: cmd.CmdPull,
			},
			{
				Name:        "stat",
				Description: "See stats of a server",
				Flags: append(connectionFlags(),
					&cli.BoolFlag{
						Name:     "no-pass-sudo",
						Usage:    "Should be set if the remote user doesn't need a password for sudo.",
						Required: false,
					},
					&cli.BoolFlag{
						Name:     "json",
						Aliases:  []string{"j"},
						Usage:    "Output in JSON for parsing by another tool.",
						Required: false,
					},
					&cli.BoolFlag{
						Name:     "no-color",
						Aliases:  []string{"n"},
						Usage:    "When set, don't show any color or use ANSI Escape Codes.",
						Required: false,
					},
					&cli.UintFlag{
						Name:     "poll",
						Aliases:  []string{"P"},
						Usage:    "Poll every n seconds. Output is updated in a basic navigator.",
						Value:    0,
						Required: false,
					},
					&cli.BoolFlag{
						Name:     "with-root",
						Aliases:  []string{"R"},
						Usage:    "Elevate the remote shell using sudo.",
						Required: false,
					},
				),
				Commands: []*cli.Command{
					{
						Name:        "all",
						Description: "See all stats",
						Action: func(ctx context.Context, c *cli.Command) error {
							return cmd.CmdStat(ctx, c, []data.SystemStat{
								&data.ProcInfoSystemStat{},
								&data.DockerSystemStat{},
								&data.FSSystemStat{},
								&data.NetIntfSystemStat{},
							})
						},
					},
					{
						Name:        "docker",
						Description: "See Docker stats",
						Action: func(ctx context.Context, c *cli.Command) error {
							return cmd.CmdStat(ctx, c, []data.SystemStat{
								&data.DockerSystemStat{},
							})
						},
					},
					{
						Name:        "fs",
						Description: "See file system stats",
						Action: func(ctx context.Context, c *cli.Command) error {
							return cmd.CmdStat(ctx, c, []data.SystemStat{
								&data.FSSystemStat{},
							})
						},
					},
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal().Err(err).Msg("")
	}

}

// connectionFlags are the flags that say which server to reach and how to
// log in, shared by every command that connects.
func connectionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:     "no-prompt",
			Usage:    "Never prompt for passwords, all passwords must be supplied via environment variables or command flags.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "config",
			Aliases:  []string{"c"},
			Usage:    "The SSH config file path.",
			Value:    "~/.ssh/config",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "alias",
			Aliases:  []string{"a"},
			Usage:    "The SSH config host alias.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "host",
			Aliases:  []string{"H"},
			Usage:    "The remote host (IP address or domain).",
			Required: false,
		},
		&cli.IntFlag{
			Name:     "port",
			Aliases:  []string{"p"},
			Usage:    "The remote SSH port. Default: the ssh config's Port, else 22.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "user",
			Aliases:  []string{"u"},
			Usage:    "The user to SSH as. Default: the ssh config's User, else the local user.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "user-pass",
			Usage:    "The remote user password.",
			Sources:  cli.EnvVars("MITOSU_USER_PASSWORD"),
			Required: false,
		},
		&cli.StringFlag{
			Name:     "key",
			Aliases:  []string{"i"},
			Usage:    "The SSH private key file path.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "known-hosts",
			Usage:    "The known_hosts file to check the server's host key against. Default: the ssh config's, else ~/.ssh/known_hosts and /etc/ssh/ssh_known_hosts. Unknown or changed keys are refused.",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "key-pass",
			Usage:    "The SSH private key password.",
			Sources:  cli.EnvVars("MITOSU_KEY_PASSWORD"),
			Required: false,
		},
	}
}
