package ssh

import (
	"bytes"
	"net"
	"strconv"

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
	}else {
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

