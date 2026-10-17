// An SSH connection to one host: connecting with host key checking and
// authentication, and running remote commands.

package ssh

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mitosu/src/shell"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
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

// SSHClient is one SSH connection. Exec, ExecContext, Alive and Closed
// may be called from several goroutines; Connect may not run while any
// of them does.
type SSHClient struct {
	*gossh.Client
	Config               Section
	Passwords            SSHPasswords
	SudoRequiresPassword bool

	// ConnectTimeout and HandshakeTimeout override the defaults when not
	// zero.
	ConnectTimeout   time.Duration
	HandshakeTimeout time.Duration

	// closed is closed when the current connection has ended.
	closed chan struct{}
}

// Connect dials s.Config, verifies the host key against
// s.Config.KnownHostsFiles (unknown or changed keys are refused, see
// HostKeyCheck) and authenticates with the agent and key files, then a
// password. The TCP connect gives up after DefaultConnectTimeout and,
// unless s.Passwords.CanPrompt, the handshake after
// DefaultHandshakeTimeout (see the fields to override them). On
// success it replaces any previous connection.
func (s *SSHClient) Connect() error {
	return s.ConnectContext(context.Background())
}

// ConnectContext is Connect that gives up as soon as ctx is done.
func (s *SSHClient) ConnectContext(ctx context.Context) error {

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
	client, err := dial(ctx, addr, config, handshake)

	if err != nil {
		return err
	}

	if s.Client != nil {
		s.Client.Close()
	}

	s.Client = client
	closed := make(chan struct{})
	s.closed = closed
	go func() {
		client.Wait()
		close(closed)
	}()

	return nil
}

// Closed reports whether the connection has ended (closed by either side
// or lost), without sending anything. It is true before Connect.
func (s *SSHClient) Closed() bool {
	if s.closed == nil {
		return true
	}
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// dial connects to addr within config.Timeout, then runs the SSH
// handshake within handshake (no limit if 0).
func dial(ctx context.Context, addr string, config *gossh.ClientConfig, handshake time.Duration) (*gossh.Client, error) {
	dialer := net.Dialer{Timeout: config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Closing the connection ends a handshake still running when ctx ends.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
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

// Alive reports whether the connection still answers, by sending an
// OpenSSH keepalive request and waiting at most timeout for the reply.
// A dead or silent connection is closed.
func (s *SSHClient) Alive(timeout time.Duration) bool {
	if s.Client == nil {
		return false
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := s.Client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			return true
		}
	case <-time.After(timeout):
	}
	s.Client.Close()
	return false
}

// maxStderr caps the stderr kept from a remote command.
const maxStderr = 16 * 1024

// execGrace is how long Exec waits for the server to close a session it
// was asked to close, before dropping the whole connection.
const execGrace = 5 * time.Second

// ExecResult is the outcome of a command run by Exec.
type ExecResult struct {
	// Status is the exit status, or -1 if the command ended without one:
	// killed by a signal (Signal says which), cut off by Exec, or the
	// connection was lost.
	Status int
	// Signal names the signal that ended the command, if one did.
	Signal string
	// TimedOut is true if Exec stopped the command at its deadline.
	TimedOut bool
	// Stderr holds the first 16 KiB of the command's stderr.
	Stderr string
	// ReadErr is the error returned by the read function.
	ReadErr error
}

// Exec runs command in a new session on the connection, without a PTY or
// stdin, and calls read with the command's stdout while it runs. When read
// returns, the session is closed (a command still writing is cut off) and
// waited for. If the whole run takes longer than timeout, the session is
// closed, so read sees the end of stdout. A server that does not close a
// session within 5 s of being asked has the whole connection dropped
// (later calls on this client fail). The error is only for a session that
// could not be started.
func (s *SSHClient) Exec(command string, timeout time.Duration, read func(io.Reader) error) (ExecResult, error) {
	return s.ExecContext(context.Background(), command, timeout, read)
}

// ExecContext is Exec that also stops the command, as at its deadline,
// when ctx is done (for example the browser that asked went away). If ctx
// ends before the session is open, it returns ctx.Err() at once and keeps
// the connection (the session is closed when it opens; the connection is
// dropped only if it does not open by the deadline).
func (s *SSHClient) ExecContext(ctx context.Context, command string, timeout time.Duration, read func(io.Reader) error) (ExecResult, error) {
	log.Debug().Str("command", command).Msg("Running command")

	// The deadline covers opening the session too: a server can leave a
	// channel open request unanswered, and then the only way to stop
	// waiting is to drop the connection. A cancelled ctx alone does not
	// drop it (it is shared): the session is closed when it opens.
	deadline := time.Now().Add(timeout)
	type opened struct {
		session *gossh.Session
		err     error
	}
	openedCh := make(chan opened, 1)
	go func() {
		sess, err := s.Client.NewSession()
		openedCh <- opened{sess, err}
	}()
	var session *gossh.Session
	select {
	case o := <-openedCh:
		if o.err != nil {
			return ExecResult{Status: -1}, o.err
		}
		session = o.session
	case <-ctx.Done():
		go func() {
			select {
			case o := <-openedCh:
				if o.err == nil {
					closeSession(o.session)
				}
			case <-time.After(time.Until(deadline)):
				s.Client.Close()
			}
		}()
		return ExecResult{Status: -1}, ctx.Err()
	case <-time.After(time.Until(deadline)):
		s.Client.Close()
		return ExecResult{Status: -1, TimedOut: true}, errors.New("ssh: opening a session timed out")
	}

	done := make(chan struct{})
	var timedOut atomic.Bool
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		case <-time.After(time.Until(deadline)):
		}
		timedOut.Store(true)
		closeSession(session)
		select {
		case <-done:
		case <-time.After(execGrace):
			s.Client.Close()
		}
	}()
	defer close(done)

	stderr := &cappedBuffer{max: maxStderr}
	session.Stderr = stderr
	stdout, err := session.StdoutPipe()
	if err != nil {
		closeSession(session)
		return ExecResult{Status: -1}, err
	}
	if err := session.Start(command); err != nil {
		closeSession(session)
		return ExecResult{Status: -1}, err
	}

	readErr := read(stdout)
	// Stop a command that is still sending (read gave up early); after a
	// complete read this only ends an idle session.
	closeSession(session)
	waited := make(chan error, 1)
	go func() {
		waited <- session.Wait()
	}()
	var waitErr error
	select {
	case waitErr = <-waited:
	case <-time.After(execGrace):
		s.Client.Close()
		waitErr = <-waited
	}

	res := ExecResult{Stderr: stderr.String(), ReadErr: readErr, TimedOut: timedOut.Load()}
	var exitErr *gossh.ExitError
	switch {
	case errors.As(waitErr, &exitErr):
		res.Status = exitErr.ExitStatus()
		if exitErr.Signal() != "" {
			res.Status = -1
			res.Signal = exitErr.Signal()
		}
	case waitErr != nil:
		// *gossh.ExitMissingError, or the connection went away.
		res.Status = -1
	}
	return res, nil
}

// closeSession closes a session, logging a failure other than "already
// closed".
func closeSession(session *gossh.Session) {
	if err := session.Close(); err != nil && !errors.Is(err, io.EOF) {
		log.Debug().Err(err).Msg("Closing session")
	}
}

// cappedBuffer keeps the first max bytes written to it and drops the rest.
type cappedBuffer struct {
	max int
	buf bytes.Buffer
}

// Write keeps what fits under max and reports all of p as written, so the
// writer never sees an error.
func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// String returns the bytes kept.
func (c *cappedBuffer) String() string {
	return c.buf.String()
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
