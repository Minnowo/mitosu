package ssh

import (
	"os"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"
)

func GetKeyAuthMethod(keypath string) (gossh.AuthMethod, error) {

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

	pass, err := PromptForKeyPassword(keypath)

	if err != nil {
		return nil, err
	}

	signer, err = gossh.ParsePrivateKeyWithPassphrase(keyBytes, pass)

	if err != nil {
		log.Debug().Err(err).Str("key", keypath).Msg("Failed to parse private key with password")
		return nil, err
	}

	log.Debug().Str("key", keypath).Msg("Got private key auth method")

	return gossh.PublicKeys(signer), nil
}
