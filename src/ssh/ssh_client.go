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
}

func NewClient(s Section) (SSHClient, error) {

	authMethods := make([]gossh.AuthMethod, 0, 3)

	if m, err := GetAgentAuthMethod(s.User, s.Hostname); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get ssh agent auth method")
	}

	if m, err := GetKeyAuthMethod(s.IdentityFile); err == nil {
		authMethods = append(authMethods, m)
	} else {
		log.Debug().Err(err).Msg("Could not get key auth method")
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
	}

	return sshClient, err
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

func (s *SSHClient) RunCommands(sh shell.Shell, commands []string) ([]string, error) {

	log.Debug().Strs("command", commands).Msg("Running command")

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
	session.Stdout = &buf

	if err := session.Start(sh.Sh()); err != nil {
		return nil, err
	}

	for _, cmd := range commands {

		var run string

		run = sh.OrTrue(cmd)
		log.Debug().Str("cmd", run).Msg("Running")
		fmt.Fprintln(stdin, run)

		run = sh.Echo(sep)
		log.Debug().Str("cmd", run).Msg("Running")
		fmt.Fprintln(stdin, run)
	}

	stdin.Close()

	if err := session.Wait(); err != nil {
		return nil, err
	}

	stdout := buf.String()

	results := strings.Split(stdout, sep)

	if len(results) == len(commands)+1 {
		results = results[0:len(commands)]
	}

	log.Debug().Int("len", len(results)).Strs("stdout", results).Msg("got result of commands")

	return results, nil
}
