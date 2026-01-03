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
				Name:        "stat",
				Description: "See stats of a server",
				Flags: []cli.Flag{
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
						Usage:    "Elevate the remote shell using sudo, and prompt for a root password.",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "ssh-config",
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
						Usage:    "The remote SSH port.",
						Value:    22,
						Required: false,
					},
					&cli.StringFlag{
						Name:     "user",
						Aliases:  []string{"u"},
						Usage:    "The user to SSH as.",
						Value:    "root",
						Required: false,
					},
					&cli.StringFlag{
						Name:     "key",
						Aliases:  []string{"i"},
						Usage:    "The SSH private key file path.",
						Required: false,
					},
				},
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
