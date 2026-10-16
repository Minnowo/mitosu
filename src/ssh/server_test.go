// An in-process SSH server, an in-memory ssh-agent and key helpers for
// tests: real handshakes against SSHClient.Connect, recording which
// public keys the client offers.

package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// testSSHServer accepts connections until the test ends.
type testSSHServer struct {
	port int

	mu      sync.Mutex
	offered []gossh.PublicKey
}

// startSSHServer serves with hostKeys and accepts the authorized public
// keys. Sessions are refused; a connection stays open until the client
// closes it.
func startSSHServer(t *testing.T, hostKeys []gossh.Signer, authorized []gossh.PublicKey) *testSSHServer {
	t.Helper()
	srv := &testSSHServer{}
	config := &gossh.ServerConfig{
		PublicKeyCallback: func(_ gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			srv.record(key)
			for _, a := range authorized {
				if bytes.Equal(a.Marshal(), key.Marshal()) {
					return &gossh.Permissions{}, nil
				}
			}
			return nil, errors.New("not authorized")
		},
	}
	for _, k := range hostKeys {
		config.AddHostKey(k)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	srv.port = l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				sc, chans, reqs, err := gossh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				go gossh.DiscardRequests(reqs)
				go func() {
					for ch := range chans {
						ch.Reject(gossh.Prohibited, "test server")
					}
				}()
				sc.Wait()
				sc.Close()
			}()
		}
	}()
	return srv
}

// record notes key once, in the order the client offered it.
func (s *testSSHServer) record(key gossh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.offered {
		if bytes.Equal(k.Marshal(), key.Marshal()) {
			return
		}
	}
	s.offered = append(s.offered, key)
}

// offeredKeys returns the keys offered so far, in order.
func (s *testSSHServer) offeredKeys() []gossh.PublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gossh.PublicKey(nil), s.offered...)
}

// testKey is an ed25519 key pair for tests.
type testKey struct {
	priv   ed25519.PrivateKey
	signer gossh.Signer
	pub    gossh.PublicKey
}

// newKey returns a fresh ed25519 key.
func newKey(t *testing.T) testKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, signer: signer, pub: signer.PublicKey()}
}

// file writes the private key in OpenSSH format, protected by passphrase
// if not empty, and returns its path.
func (k testKey) file(t *testing.T, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = gossh.MarshalPrivateKey(k.priv, "")
	} else {
		block, err = gossh.MarshalPrivateKeyWithPassphrase(k.priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	write(t, path, string(pem.EncodeToMemory(block)))
	return path
}

// startAgent serves an in-memory ssh-agent holding keys on a unix socket
// and returns the socket path.
func startAgent(t *testing.T, keys ...testKey) string {
	t.Helper()
	keyring := agent.NewKeyring()
	for _, k := range keys {
		if err := keyring.Add(agent.AddedKey{PrivateKey: k.priv}); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				agent.ServeAgent(keyring, conn)
				conn.Close()
			}()
		}
	}()
	return sock
}
