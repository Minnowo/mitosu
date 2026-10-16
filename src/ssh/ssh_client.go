// An SSH connection to one host: connecting with host key checking and
// authentication, and running remote commands.

package ssh

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"mitosu/src/shell"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"
)

const (
	// DefaultConnectTimeout bounds the TCP connect.
	DefaultConnectTimeout = 30 * time.Second

	// DefaultHandshakeTimeout bounds the SSH handshake, authentication
	// included, when nobody can be prompted for a password. With prompting
	// the handshake has no deadline: a person is waiting and can cancel.
	DefaultHandshakeTimeout = 30 * time.Second
)

type SSHPasswords struct {
	UserPassword string
	KeyPassword  string
	CanPrompt    bool
}

type SSHClient struct {
	*gossh.Client
	Config               Section
	Passwords            SSHPasswords
	SudoRequiresPassword bool

	// ConnectTimeout and HandshakeTimeout override the defaults when not
	// zero.
	ConnectTimeout   time.Duration
	HandshakeTimeout time.Duration
}

// Connect dials s.Config, verifies the host key against
// s.Config.KnownHostsFiles (unknown or changed keys are refused, see
// HostKeyCheck) and authenticates with the agent and key files, then a
// password. The TCP connect gives up after DefaultConnectTimeout and,
// unless s.Passwords.CanPrompt, the handshake after
// DefaultHandshakeTimeout (see the fields to override them). On
// success it replaces any previous connection.
func (s *SSHClient) Connect() error {

	hostKeyCallback, hostKeyAlgorithms := HostKeyCheck(s.Config.KnownHostsFiles, s.Config.KnownHostsAddress(), s.Config.VerifyHint())

	keyAuth, closeAgent := PublicKeyAuth(s.Config, &s.Passwords)
	defer closeAgent()

	config := &gossh.ClientConfig{
		User: s.Config.User,
		Auth: []gossh.AuthMethod{
			keyAuth,
			GetPasswordAuthMethod(s.Config.User, s.Config.Hostname, &s.Passwords),
		},
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: hostKeyAlgorithms,
		Timeout:           DefaultConnectTimeout,
	}
	if s.ConnectTimeout != 0 {
		config.Timeout = s.ConnectTimeout
	}
	handshake := DefaultHandshakeTimeout
	if s.HandshakeTimeout != 0 {
		handshake = s.HandshakeTimeout
	}
	if s.Passwords.CanPrompt {
		handshake = 0
	}

	addr := net.JoinHostPort(s.Config.Hostname, strconv.Itoa(s.Config.Port))
	log.Debug().Str("addr", addr).Str("user", s.Config.User).Msg("Connecting")
	client, err := dial(addr, config, handshake)

	if err != nil {
		return err
	}

	if s.Client != nil {
		s.Client.Close()
	}

	s.Client = client

	return nil
}

// dial connects to addr within config.Timeout, then runs the SSH
// handshake within handshake (no limit if 0).
func dial(addr string, config *gossh.ClientConfig, handshake time.Duration) (*gossh.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, config.Timeout)
	if err != nil {
		return nil, err
	}
	if handshake > 0 {
		if err := conn.SetDeadline(time.Now().Add(handshake)); err != nil {
			conn.Close()
			return nil, err
		}
	}
	c, chans, reqs, err := gossh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		c.Close()
		return nil, err
	}
	return gossh.NewClient(c, chans, reqs), nil
}

func (s *SSHClient) Close() error {
	s.Client.Close()
	return nil
}

func (s *SSHClient) PromptRootPass() error {

	if s.SudoRequiresPassword && s.Passwords.UserPassword == "" {

		if !s.Passwords.CanPrompt {
			return fmt.Errorf("Root shell requires sudo password: %w", ErrUserEmptyPassword)
		}

		pass, err := PromptForPasswordF("Enter %s@%s's sudo password: ",
			s.Config.User, s.Config.Hostname)

		if err != nil {
			return fmt.Errorf("Root shell requires sudo password: %w", err)
		} else {
			s.Passwords.UserPassword = string(pass)
		}
	}

	return nil
}

func (s *SSHClient) RunCommand(command string) (string, error) {

	log.Debug().Str("command", command).Msg("Running command")

	session, err := s.Client.NewSession()

	if err != nil {
		return "", err
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf

	if err := session.Run(command); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (s *SSHClient) RunCommands(withRoot bool, sh shell.Shell, commands []shell.ShellCmd) ([]string, error) {

	log.Debug().
		Int("shell", int(sh.GetType())).
		Interface("command", commands).
		Msg("Running command")

	session, err := s.Client.NewSession()

	if err != nil {
		return nil, err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()

	if err != nil {
		return nil, err
	}

	var sepBytes [32]byte
	rand.Read(sepBytes[:])
	sep := fmt.Sprintf("[%x]\n", sepBytes)

	var buf bytes.Buffer
	var bufErr bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &bufErr

	var cmd string

	if withRoot {

		if err := s.PromptRootPass(); err != nil {
			return nil, err
		}

		cmd = sh.RootSh(s.SudoRequiresPassword)
		log.Error().Str("cmd", cmd).Bool("no-pass-sudo", !s.SudoRequiresPassword).Msg("Running root shell")

		if err := session.Start(cmd); err != nil {
			return nil, err
		}

		if s.SudoRequiresPassword {
			fmt.Fprintln(stdin, s.Passwords.UserPassword)
		}

	} else {

		cmd = sh.Sh()
		log.Debug().Str("cmd", cmd).Msg("Running shell")

		// none root shell
		if err := session.Start(sh.Sh()); err != nil {
			return nil, err
		}
	}

	for _, shCmd := range commands {

		cmd = sh.OrTrue(shCmd.Cmd)
		log.Debug().Str("cmd", cmd).Msg("Running")
		fmt.Fprintln(stdin, cmd)

		cmd = sh.Echo(sep)
		log.Debug().Str("cmd", cmd).Msg("Running")
		fmt.Fprintln(stdin, cmd)
	}
	stdin.Close()

	if err := session.Wait(); err != nil {
		return nil, err
	}

	stdout := buf.String()
	stderr := bufErr.String()

	results := strings.Split(stdout, sep)

	if len(results) == len(commands)+1 {
		results = results[0:len(commands)]
	}

	log.Debug().Str("stderr", stderr).Str("stdout", stdout).Msg("Got SSH output")

	return results, nil
}
