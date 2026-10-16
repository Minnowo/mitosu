// Opens the SSH connection for a command from its connection flags.

package cmd

import (
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"mitosu/src/ssh"
)

// connect resolves c's connection flags (see resolveHost) and connects.
// sudoRequiresPassword is only used by commands that run sudo. The caller
// closes the returned client.
func connect(c *cli.Command, sudoRequiresPassword bool) (*ssh.SSHClient, error) {
	section, err := hostSection(c)
	if err != nil {
		return nil, err
	}

	log.Debug().
		Str("name", section.Name).
		Str("host", section.Hostname).
		Int("port", section.Port).
		Str("user", section.User).
		Strs("keys", section.IdentityFiles).
		Strs("known_hosts", section.KnownHostsFiles).
		Msg("Resolved host")

	client := &ssh.SSHClient{
		Config: section,
		Passwords: ssh.SSHPasswords{
			KeyPassword:  c.String("key-pass"),
			UserPassword: c.String("user-pass"),
			CanPrompt:    !c.Bool("no-prompt"),
		},
		SudoRequiresPassword: sudoRequiresPassword,
	}
	if err := client.Connect(); err != nil {
		return nil, err
	}
	return client, nil
}
