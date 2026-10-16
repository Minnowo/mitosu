// Resolves the connection settings for one host name from OpenSSH client
// config files, following ssh_config(5) (docs/reference/ssh_config.5.txt):
// the first value obtained for a keyword wins, Host and Match blocks apply
// when they match, Include reads more files in place.

package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// maxIncludeDepth bounds nested Include directives, as OpenSSH does.
	maxIncludeDepth = 16

	// maxConfigLine caps one config line; longer lines are refused.
	maxConfigLine = 64 * 1024

	// SystemConfigPath is the system-wide config, read after the user's.
	SystemConfigPath = "/etc/ssh/ssh_config"
)

// singleValue lists the keywords mitosu reads that take exactly one
// argument; more is an error, as in OpenSSH.
var singleValue = map[string]bool{
	"hostname":       true,
	"port":           true,
	"user":           true,
	"identityfile":   true,
	"identitiesonly": true,
	"hostkeyalias":   true,
	"proxyjump":      true,
}

// routeKeywords change where mitosu connects or how it checks the host.
// A Match block mitosu cannot evaluate must not set one of them while it is
// still unset, since mitosu cannot tell whether the setting applies.
var routeKeywords = map[string]bool{
	"hostname":             true,
	"port":                 true,
	"user":                 true,
	"hostkeyalias":         true,
	"userknownhostsfile":   true,
	"globalknownhostsfile": true,
	"proxyjump":            true,
	"proxycommand":         true,
}

// ConfigFile is one config file to read and the directory its relative
// Include paths are resolved against (~/.ssh for a user config, /etc/ssh
// for the system config).
type ConfigFile struct {
	Path        string
	IncludeBase string
}

// Section holds the settings mitosu uses to reach one host.
type Section struct {
	// Name is the host name as given by the user (the ssh "alias").
	Name string
	// Hostname is the address to connect to.
	Hostname string
	Port     int
	User     string
	// IdentityFiles are private key files to try, in order.
	IdentityFiles []string
	// DefaultIdentities is true when IdentityFiles are OpenSSH's default
	// names rather than configured ones; missing files are then skipped
	// without a warning.
	DefaultIdentities bool
	// IdentitiesOnly offers only IdentityFiles, never other agent keys.
	IdentitiesOnly bool
	// KnownHostsFiles are the user and global known_hosts files, in order.
	KnownHostsFiles []string
	// HostKeyAlias, if set, is the name looked up in known_hosts instead of
	// Hostname.
	HostKeyAlias string
}

// ErrProxyNotSupported is returned when the config asks for ProxyJump or
// ProxyCommand: mitosu cannot honour them and must not connect directly
// instead.
var ErrProxyNotSupported = errors.New("ProxyJump and ProxyCommand are not supported")

// Resolve returns the settings for name from files, read in order. A
// missing file counts as empty. overrides are values given on the command
// line, keyed by lower-case keyword (like `ssh -o`); they come first, so
// they win, and "identityfile" there replaces every configured
// IdentityFile. Unset values get OpenSSH's defaults: Hostname is name,
// Port 22, User the local user, the default identity and known_hosts
// files. Match criteria all, host, originalhost, user and localuser are
// evaluated; a Match block with other criteria is skipped with a warning,
// or refused if it would set where or how mitosu connects. It returns an
// error for a malformed line, an Include nested deeper than 16 files, an
// unsupported token, or a proxy setting. Warnings are returned even with an
// error.
func Resolve(files []ConfigFile, name string, overrides map[string]string) (Section, []string, error) {
	r := resolver{name: name, values: map[string][]string{}}
	for k, v := range overrides {
		r.values[k] = []string{v}
	}
	_, r.identityOverride = overrides["identityfile"]
	for _, f := range files {
		if err := r.readFile(f.Path, f.IncludeBase, blockState{active: true}, 0); err != nil {
			return Section{}, r.warnings, err
		}
	}
	s, err := r.section()
	return s, r.warnings, err
}

// resolver collects the first value of each keyword for one host name.
type resolver struct {
	name string
	// values holds the arguments of the first occurrence of each keyword
	// that applied, keyed by lower-case keyword; "identityfile" collects
	// every occurrence instead.
	values           map[string][]string
	identityOverride bool
	warnings         []string
}

// blockState tells whether the lines of the current Host or Match block
// apply. unknown is set (to the file:line of the Match) when a Match block
// has a criterion mitosu cannot evaluate.
type blockState struct {
	active  bool
	unknown string
}

// readFile reads one config file. state applies to the lines before the
// first Host or Match line: the top level applies to every host, an
// Include continues the including block.
func (r *resolver) readFile(path, base string, state blockState, depth int) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ssh config: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), maxConfigLine)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		keyword, args, err := splitConfigLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("ssh config %s:%d: %w", path, lineNum, err)
		}
		if keyword == "" {
			continue
		}
		where := fmt.Sprintf("%s:%d", path, lineNum)
		if singleValue[keyword] && len(args) > 1 {
			return fmt.Errorf("ssh config %s: %s takes one argument, got %d", where, keyword, len(args))
		}
		switch keyword {
		case "host":
			state = blockState{active: hostMatches(args, r.name)}
		case "match":
			state, err = r.matchState(args, where)
			if err != nil {
				return err
			}
		case "include":
			if state.unknown != "" {
				return fmt.Errorf("ssh config %s: Include inside a Match block mitosu cannot evaluate (%s)", where, state.unknown)
			}
			if !state.active {
				continue
			}
			if depth+1 > maxIncludeDepth {
				return fmt.Errorf("ssh config %s: Include nested deeper than %d files", where, maxIncludeDepth)
			}
			if err := r.include(args, base, state, depth+1); err != nil {
				return fmt.Errorf("ssh config %s: %w", where, err)
			}
		default:
			if state.unknown != "" && routeKeywords[keyword] && r.values[keyword] == nil {
				return fmt.Errorf("ssh config %s: %s is set inside a Match block mitosu cannot evaluate (%s), so mitosu cannot tell whether it applies",
					where, keyword, state.unknown)
			}
			if state.active {
				r.set(keyword, args)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ssh config %s: %w", path, err)
	}
	return nil
}

// include reads every file matched by the Include arguments, each glob in
// lexical order.
func (r *resolver) include(args []string, base string, state blockState, depth int) error {
	for _, arg := range args {
		pattern, err := expandPath(arg, r.tokens(nil))
		if err != nil {
			return fmt.Errorf("Include %q: %w", arg, err)
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(base, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("Include %q: %w", arg, err)
		}
		for _, m := range matches {
			if err := r.readFile(m, base, state, depth); err != nil {
				return err
			}
		}
	}
	return nil
}

// set records a keyword's arguments if it has no value yet.
func (r *resolver) set(keyword string, args []string) {
	if keyword == "identityfile" {
		if !r.identityOverride {
			r.values[keyword] = append(r.values[keyword], args...)
		}
		return
	}
	if _, ok := r.values[keyword]; !ok {
		r.values[keyword] = args
	}
}

// first returns the first argument recorded for keyword, or "".
func (r *resolver) first(keyword string) string {
	args := r.values[keyword]
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// section applies defaults and token expansion to the recorded values.
func (r *resolver) section() (Section, error) {
	for _, proxy := range []string{"proxyjump", "proxycommand"} {
		if v := r.first(proxy); v != "" && !strings.EqualFold(v, "none") {
			return Section{}, fmt.Errorf("host %q: %w", r.name, ErrProxyNotSupported)
		}
	}

	s := Section{Name: r.name, Port: 22}

	hostname, err := r.hostname()
	if err != nil {
		return Section{}, err
	}
	s.Hostname = hostname

	if v := r.first("port"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Section{}, fmt.Errorf("Port %q: not a port number", v)
		}
		s.Port = port
	}

	s.User = r.first("user")
	if s.User == "" {
		u, err := LocalUser()
		if err != nil {
			return Section{}, err
		}
		s.User = u
	}

	s.HostKeyAlias = r.first("hostkeyalias")

	switch v := strings.ToLower(r.first("identitiesonly")); v {
	case "", "no":
	case "yes":
		s.IdentitiesOnly = true
	default:
		return Section{}, fmt.Errorf("IdentitiesOnly %q: want yes or no", v)
	}

	tokens := r.tokens(&s)

	files := r.values["identityfile"]
	if len(files) == 0 {
		files = []string{"~/.ssh/id_rsa", "~/.ssh/id_ecdsa", "~/.ssh/id_ecdsa_sk", "~/.ssh/id_ed25519", "~/.ssh/id_ed25519_sk"}
		s.DefaultIdentities = true
	}
	for _, f := range files {
		if strings.EqualFold(f, "none") {
			continue
		}
		p, err := expandPath(f, tokens)
		if err != nil {
			return Section{}, fmt.Errorf("IdentityFile %q: %w", f, err)
		}
		s.IdentityFiles = append(s.IdentityFiles, p)
	}

	known := []struct {
		keyword  string
		defaults []string
	}{
		{"userknownhostsfile", []string{"~/.ssh/known_hosts", "~/.ssh/known_hosts2"}},
		{"globalknownhostsfile", []string{"/etc/ssh/ssh_known_hosts", "/etc/ssh/ssh_known_hosts2"}},
	}
	for _, k := range known {
		files, ok := r.values[k.keyword]
		if !ok {
			files = k.defaults
		}
		for _, f := range files {
			if strings.EqualFold(f, "none") {
				continue
			}
			p, err := expandPath(f, tokens)
			if err != nil {
				return Section{}, fmt.Errorf("%s %q: %w", k.keyword, f, err)
			}
			s.KnownHostsFiles = append(s.KnownHostsFiles, p)
		}
	}
	return s, nil
}

// hostname returns the HostName obtained so far, expanded, or the name.
func (r *resolver) hostname() (string, error) {
	v := r.first("hostname")
	if v == "" {
		return r.name, nil
	}
	h, err := expandTokens(v, map[byte]string{'h': r.name})
	if err != nil {
		return "", fmt.Errorf("HostName %q: %w", v, err)
	}
	return h, nil
}

// tokens returns the ssh_config TOKENS mitosu supports. With s nil (while
// reading Include lines) the remote values are not known yet and only the
// local tokens and %h / %n are set.
func (r *resolver) tokens(s *Section) map[byte]string {
	t := map[byte]string{'n': r.name, 'h': r.name}
	if home, err := os.UserHomeDir(); err == nil {
		t['d'] = home
	}
	if u, err := LocalUser(); err == nil {
		t['u'] = u
	}
	t['i'] = strconv.Itoa(os.Getuid())
	if s != nil {
		t['h'] = s.Hostname
		t['p'] = strconv.Itoa(s.Port)
		t['r'] = s.User
	}
	return t
}

// DefaultConfigFiles returns ~/.ssh/config and the system config, with
// their Include bases, in the order ssh reads them.
func DefaultConfigFiles() ([]ConfigFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	userBase := filepath.Join(home, ".ssh")
	return []ConfigFile{
		{Path: filepath.Join(userBase, "config"), IncludeBase: userBase},
		{Path: SystemConfigPath, IncludeBase: filepath.Dir(SystemConfigPath)},
	}, nil
}

// KnownHostsAddress returns the host:port looked up in known_hosts: the
// HostKeyAlias without a port (OpenSSH ignores the port for an alias), or
// Hostname and Port.
func (s Section) KnownHostsAddress() string {
	if s.HostKeyAlias != "" {
		return net.JoinHostPort(s.HostKeyAlias, "22")
	}
	return net.JoinHostPort(s.Hostname, strconv.Itoa(s.Port))
}

// VerifyHint returns the ssh command that reaches the host the way mitosu
// does, for the user to verify and record its host key.
func (s Section) VerifyHint() string {
	if s.Name != s.Hostname {
		return "ssh " + s.Name
	}
	return fmt.Sprintf("ssh -p %d %s@%s", s.Port, s.User, s.Hostname)
}

// LocalUser returns the name of the user running mitosu.
func LocalUser() (string, error) {
	u, err := user.Current()
	if err == nil && u.Username != "" {
		return u.Username, nil
	}
	if name := os.Getenv("USER"); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("cannot find the local user name: %v", err)
}
