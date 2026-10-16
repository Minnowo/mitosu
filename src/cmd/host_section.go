// Builds the connection settings for a command from its flags and, when
// --alias is given, the OpenSSH config files.

package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"mitosu/src/ssh"
)

// hostFlags are the connection flags of a command. The *Set fields tell
// whether the flag was given explicitly.
type hostFlags struct {
	alias, host string
	config      string
	configSet   bool
	port        int
	portSet     bool
	user        string
	key         string
	knownHosts  string
}

// hostSection returns the settings to connect with, from c's flags (see
// resolveHost), logging config warnings.
func hostSection(c *cli.Command) (ssh.Section, error) {
	f := hostFlags{
		alias:      c.String("alias"),
		host:       c.String("host"),
		config:     c.String("config"),
		configSet:  c.IsSet("config"),
		port:       c.Int("port"),
		portSet:    c.IsSet("port"),
		user:       c.String("user"),
		key:        c.String("key"),
		knownHosts: c.String("known-hosts"),
	}
	section, warnings, err := resolveHost(f)
	for _, w := range warnings {
		log.Warn().Msg(w)
	}
	return section, err
}

// resolveHost resolves the flags like ssh would. --alias reads
// ~/.ssh/config and /etc/ssh/ssh_config, or only --config if it was given
// (like `ssh -F`); --host alone reads no config. Flags given explicitly
// come first, so they win over the config: --host (with --alias) as
// HostName, --port, --user, --key (the only identity file) and
// --known-hosts (the only known_hosts file).
func resolveHost(f hostFlags) (ssh.Section, []string, error) {
	if f.alias == "" && f.host == "" {
		return ssh.Section{}, nil, errors.New("give --alias (a name from your ssh config) or --host")
	}

	overrides := map[string]string{}
	if f.portSet {
		if f.port < 1 || f.port > 65535 {
			return ssh.Section{}, nil, fmt.Errorf("--port %d: not a port number", f.port)
		}
		overrides["port"] = strconv.Itoa(f.port)
	}
	if f.user != "" {
		overrides["user"] = f.user
	}
	if f.key != "" {
		overrides["identityfile"] = f.key
	}
	if f.knownHosts != "" {
		overrides["userknownhostsfile"] = f.knownHosts
		overrides["globalknownhostsfile"] = "none"
	}

	if f.alias == "" {
		return ssh.Resolve(nil, f.host, overrides)
	}

	if f.host != "" {
		overrides["hostname"] = f.host
	}
	files, err := ssh.DefaultConfigFiles()
	if err != nil {
		return ssh.Section{}, nil, err
	}
	if f.configSet {
		path, err := ssh.ExpandPath(f.config)
		if err != nil {
			return ssh.Section{}, nil, fmt.Errorf("--config: %w", err)
		}
		files = []ssh.ConfigFile{{Path: path, IncludeBase: files[0].IncludeBase}}
	}
	return ssh.Resolve(files, f.alias, overrides)
}
