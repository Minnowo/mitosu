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

type SSHClient struct {
	*gossh.Client
	Config Section
}

func NewClient(s Section) (SSHClient, error) {

	authMethods := make([]gossh.AuthMethod, 0, 3)

	if m, err := GetKeyAuthMethod(s.IdentityFile); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get key auth method")
	}

	if m, err := GetAgentAuthMethod(s.User, s.Hostname); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get ssh agent auth method")
	}

	m := GetPasswordAuthMethod(s.User, s.Hostname)
	authMethods = append(authMethods, m)

	config := &gossh.ClientConfig{
		User: s.User,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, _ net.Addr, _ gossh.PublicKey) error {
			log.Debug().Str("host", hostname).Msg("Connecting to remote")
			return nil
		},
	}

	addr := s.Hostname + ":" + strconv.Itoa(s.Port)
	client, err := gossh.Dial("tcp", addr, config)

	if err != nil {
		client = nil
	}

	sshClient := SSHClient{
		Client: client,
		Config: s,
	}

	return sshClient, err
}

func (s *SSHClient) Close() error {
	s.Client.Close()
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

func (s *SSHClient) RunCommands(sh shell.Shell, commands []shell.ShellCmd) ([]string, error) {

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

	if rootPw, err := sh.GetRootPassword(); err == nil {

		cmd = sh.RootSh()
		log.Debug().Str("cmd", cmd).Msg("Shell has root password, running root shell")

		if err := session.Start(cmd); err != nil {
			return nil, err
		}

		// provide the root password
		fmt.Fprintln(stdin, rootPw)

	} else {

		if err != shell.ErrNoRootAccess {
			return nil, err
		}

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
