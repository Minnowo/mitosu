package ssh

import (
	"errors"
	"net"
	"os"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)
var(
	ErrAgentNotAvailable = errors.New("SSH agent not available: SSH_AUTH_SOCK is not set")
	ErrAgentConnect      = errors.New("failed to connect to SSH agent socket")
)


func GetAgentAuthMethod(user, addr string) (gossh.AuthMethod, error) {

	sock := os.Getenv("SSH_AUTH_SOCK")

	if len(sock) <= 0 {
		return nil, ErrAgentNotAvailable
	}

	agconn, err := net.Dial("unix", sock)

	if  err != nil {
		return nil,ErrAgentConnect
	}

	ag := agent.NewClient(agconn)
	authMethod := gossh.PublicKeysCallback(ag.Signers)

	return authMethod, nil
}

