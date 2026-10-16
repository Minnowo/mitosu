// One "publickey" authentication method offering, like OpenSSH, the
// identity files' keys first (through the ssh-agent when it holds the same
// key), then the agent's other keys unless IdentitiesOnly is set. x/crypto
// tries each method name only once, so all keys share one method.

package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// maxPassphraseTries is how often a passphrase prompt is repeated after a
// wrong passphrase, as in OpenSSH.
const maxPassphraseTries = 3

// ErrEmptyKeyPassword is returned when a key file needs a passphrase and
// none was given or could be prompted for.
var ErrEmptyKeyPassword = errors.New("key requires a passphrase, but none was given and prompting is off")

// PublicKeyAuth returns the publickey method for the identity files of s
// and a function that closes the agent connection, to call once the SSH
// handshake is over. Keys come in the order described in the file header.
// A passphrase-protected file is decrypted with pwds.KeyPassword if given;
// otherwise, if prompting is allowed and its public key is known
// (OpenSSH format, or a .pub file beside it), only when the server
// accepts that key. A protected key that cannot be unlocked is not
// offered. If the user cancels a prompt or enters a wrong passphrase three
// times, x/crypto ends public key authentication there.
func PublicKeyAuth(s Section, pwds *SSHPasswords) (gossh.AuthMethod, func()) {
	var agentConn io.Closer
	method := gossh.PublicKeysCallback(func() ([]gossh.Signer, error) {
		if agentConn != nil {
			agentConn.Close()
		}
		var signers []gossh.Signer
		signers, agentConn = keySigners(os.Getenv("SSH_AUTH_SOCK"), s, pwds)
		return signers, nil
	})
	closeAgent := func() {
		if agentConn != nil {
			agentConn.Close()
		}
	}
	return method, closeAgent
}

// keySigners returns the keys to offer for s, in order, using the agent at
// agentSock if not empty and reachable. The returned closer, if not nil,
// is the agent connection, which the agent's signers need until
// authentication ends. Files that cannot be used are logged and skipped.
func keySigners(agentSock string, s Section, pwds *SSHPasswords) ([]gossh.Signer, io.Closer) {
	var agentSigners []gossh.Signer
	var agentConn io.Closer
	if agentSock != "" {
		conn, err := net.Dial("unix", agentSock)
		if err != nil {
			log.Debug().Err(err).Msg("Could not connect to the ssh agent")
		} else {
			agentConn = conn
			agentSigners, err = agent.NewClient(conn).Signers()
			if err != nil {
				log.Debug().Err(err).Msg("Could not list ssh agent keys")
			}
		}
	}

	var signers []gossh.Signer
	for _, f := range s.IdentityFiles {
		signer, err := fileSigner(f, agentSigners, pwds)
		if errors.Is(err, os.ErrNotExist) {
			if !s.DefaultIdentities {
				log.Warn().Str("key", f).Msg("Identity file does not exist")
			}
			continue
		}
		if err != nil {
			log.Warn().Err(err).Str("key", f).Msg("Skipping identity file")
			continue
		}
		if !hasKey(signers, signer.PublicKey()) {
			signers = append(signers, signer)
		}
	}
	if !s.IdentitiesOnly {
		for _, a := range agentSigners {
			if !hasKey(signers, a.PublicKey()) {
				signers = append(signers, a)
			}
		}
	}
	return signers, agentConn
}

// fileSigner returns the signer for the private key in path: the agent's
// if it holds the same key, else the key itself, decrypted now with
// pwds.KeyPassword or, when prompting is allowed, on first use.
func fileSigner(path string, agentSigners []gossh.Signer, pwds *SSHPasswords) (gossh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := gossh.ParsePrivateKey(keyBytes)
	if err == nil {
		if a := findKey(agentSigners, signer.PublicKey()); a != nil {
			return a, nil
		}
		return signer, nil
	}
	var missing *gossh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pub := missing.PublicKey
	if pub == nil {
		pub = readPublicKey(path + ".pub")
	}
	if pub != nil {
		if a := findKey(agentSigners, pub); a != nil {
			return a, nil
		}
	}
	if pwds.KeyPassword != "" {
		signer, err := gossh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(pwds.KeyPassword))
		if err == nil {
			return signer, nil
		}
		if !pwds.CanPrompt {
			return nil, fmt.Errorf("%s: the given key passphrase does not unlock it: %w", path, err)
		}
	}
	if !pwds.CanPrompt {
		return nil, fmt.Errorf("%s: %w", path, ErrEmptyKeyPassword)
	}
	if pub == nil {
		// Legacy PEM encryption hides the public key: decrypt now.
		return promptDecrypt(path, keyBytes)
	}
	return &lazySigner{path: path, keyBytes: keyBytes, pub: pub}, nil
}

// readPublicKey returns the key in an OpenSSH .pub file, or nil.
func readPublicKey(path string) gossh.PublicKey {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey(b)
	if err != nil {
		return nil
	}
	return pub
}

// promptDecrypt asks for the passphrase of the key in path, up to
// maxPassphraseTries times, and returns the decrypted key.
func promptDecrypt(path string, keyBytes []byte) (gossh.Signer, error) {
	var lastErr error
	for try := 0; try < maxPassphraseTries; try++ {
		pass, err := PromptForKeyPassword(path)
		if err != nil {
			return nil, err
		}
		signer, err := gossh.ParsePrivateKeyWithPassphrase(keyBytes, pass)
		if err == nil {
			return signer, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%s: %w", path, lastErr)
}

// hasKey reports whether one of signers has the public key pub.
func hasKey(signers []gossh.Signer, pub gossh.PublicKey) bool {
	return findKey(signers, pub) != nil
}

// findKey returns the signer in signers with the public key pub, or nil.
func findKey(signers []gossh.Signer, pub gossh.PublicKey) gossh.Signer {
	want := pub.Marshal()
	for _, s := range signers {
		if bytes.Equal(s.PublicKey().Marshal(), want) {
			return s
		}
	}
	return nil
}

// lazySigner is a passphrase-protected key whose public key is known. It
// prompts for the passphrase on the first signature, which x/crypto only
// asks for after the server accepted the public key. Safe for concurrent
// use.
type lazySigner struct {
	path     string
	keyBytes []byte
	pub      gossh.PublicKey

	once   sync.Once
	signer gossh.Signer
	err    error
}

func (l *lazySigner) PublicKey() gossh.PublicKey {
	return l.pub
}

func (l *lazySigner) Sign(rand io.Reader, data []byte) (*gossh.Signature, error) {
	signer, err := l.load()
	if err != nil {
		return nil, err
	}
	return signer.Sign(rand, data)
}

// SignWithAlgorithm lets x/crypto pick rsa-sha2-256/512 for RSA keys;
// without it only ssh-rsa (SHA-1) would be offered, which OpenSSH refuses.
func (l *lazySigner) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*gossh.Signature, error) {
	signer, err := l.load()
	if err != nil {
		return nil, err
	}
	as, ok := signer.(gossh.AlgorithmSigner)
	if !ok {
		if algorithm != "" && algorithm != signer.PublicKey().Type() {
			return nil, fmt.Errorf("%s: key cannot sign with %s", l.path, algorithm)
		}
		return signer.Sign(rand, data)
	}
	return as.SignWithAlgorithm(rand, data, algorithm)
}

func (l *lazySigner) load() (gossh.Signer, error) {
	l.once.Do(func() {
		l.signer, l.err = promptDecrypt(l.path, l.keyBytes)
	})
	return l.signer, l.err
}
