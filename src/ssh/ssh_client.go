package ssh

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"mitosu/src/shell"
	"net"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"
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
}

func (s *SSHClient) Connect() error {

	authMethods := make([]gossh.AuthMethod, 0, 3)

	if m, err := GetKeyAuthMethod(s.Config.IdentityFile, &s.Passwords); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get key auth method")
	}

	if m, err := GetAgentAuthMethod(s.Config.User, s.Config.Hostname); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get ssh agent auth method")
	}

	m := GetPasswordAuthMethod(s.Config.User, s.Config.Hostname, &s.Passwords)
	authMethods = append(authMethods, m)

	config := &gossh.ClientConfig{
		User: s.Config.User,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, _ net.Addr, _ gossh.PublicKey) error {
			log.Debug().Str("host", hostname).Msg("Connecting to remote")
			return nil
		},
	}

	addr := s.Config.Hostname + ":" + strconv.Itoa(s.Config.Port)
	client, err := gossh.Dial("tcp", addr, config)

	if err != nil {
		return err
	}

	if s.Client != nil {
		s.Client.Close()
	}

	s.Client = client

	return nil
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
