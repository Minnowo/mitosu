package ssh

import (
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"
)

var (
	ErrEmptyKeyPassword = errors.New("Key requires password but got empty password.")
)

func GetKeyAuthMethod(keypath string, pwds *SSHPasswords) (gossh.AuthMethod, error) {

	keyBytes, err := os.ReadFile(keypath)

	if err != nil {
		return nil, err
	}

	signer, err := gossh.ParsePrivateKey(keyBytes)

	log.Debug().Str("key", keypath).Msg("Reading private key")

	if err == nil {
		log.Debug().Str("key", keypath).Msg("Got private key auth method")
		return gossh.PublicKeys(signer), nil
	}

	log.Debug().Str("key", keypath).Msg("Private key needs a password")

	if pwds == nil {
		return nil, ErrEmptyKeyPassword
	}

	if pwds.KeyPassword == "" {

		if !pwds.CanPrompt {
			return nil, fmt.Errorf("Key requires password, but no password was given and no prompt is enabled: %w", ErrEmptyKeyPassword)
		}

		if pass, err := PromptForKeyPassword(keypath); err != nil {
			return nil, err
		} else {
			pwds.KeyPassword = string(pass)
		}
	}

	signer, err = gossh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(pwds.KeyPassword))

	if err != nil {
		log.Debug().Err(err).Str("key", keypath).Msg("Failed to parse private key with password")
		return nil, err
	}

	log.Debug().Str("key", keypath).Msg("Got private key auth method")

	return gossh.PublicKeys(signer), nil
}
