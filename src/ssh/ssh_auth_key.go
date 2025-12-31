package ssh

import (
	"os"

	gossh "golang.org/x/crypto/ssh"
)

func GetKeyAuthMethod(keypath string) (gossh.AuthMethod, error) {

	keyBytes, err := os.ReadFile(keypath)

	if err != nil {
		return nil, err
	}

	signer, err := gossh.ParsePrivateKey(keyBytes)

	if err == nil {
		return gossh.PublicKeys(signer), nil
	}

	pass, err := PromptForKeyPassword(keypath)

	if err != nil {
		return nil, err
	}

	signer, err = gossh.ParsePrivateKeyWithPassphrase(keyBytes, pass)

	if err != nil {
		return nil, err
	}

	return gossh.PublicKeys(signer), nil
}
