// Tests of SSHClient.Connect through real handshakes with the in-process
// server: host certificates, key order, locked keys and timeouts.

package ssh

import (
	"bytes"
	"crypto/rand"
	"net"
	"strconv"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestConnectWithHostCertificate(t *testing.T) {
	ca := newKey(t)
	hostKey := newKey(t)
	cert := &gossh.Certificate{
		Key:             hostKey.pub,
		CertType:        gossh.HostCert,
		ValidPrincipals: []string{"127.0.0.1"},
		ValidBefore:     gossh.CertTimeInfinity,
	}
	if err := cert.SignCert(rand.Reader, ca.signer); err != nil {
		t.Fatal(err)
	}
	certSigner, err := gossh.NewCertSigner(cert, hostKey.signer)
	if err != nil {
		t.Fatal(err)
	}
	user := newKey(t)
	srv := startSSHServer(t, []gossh.Signer{hostKey.signer, certSigner}, []gossh.PublicKey{user.pub})

	line := "@cert-authority " + serverAddress(srv) + " " + string(bytes.TrimSpace(gossh.MarshalAuthorizedKey(ca.pub)))
	c := testClient(srv, knownHosts(t, line), []string{user.file(t, "")}, SSHPasswords{})
	if err := c.Connect(); err != nil {
		t.Fatalf("host trusted through a CA refused: %v", err)
	}
	c.Close()
}

func TestConnectSkipsLockedKeyWithoutPrompt(t *testing.T) {
	locked := newKey(t)
	plain := newKey(t)
	host := newKey(t)
	srv := startSSHServer(t, []gossh.Signer{host.signer}, []gossh.PublicKey{locked.pub, plain.pub})
	files := []string{locked.file(t, "secret"), plain.file(t, "")}

	for _, pwds := range []SSHPasswords{{}, {KeyPassword: "wrong"}} {
		c := testClient(srv, knownHostFor(t, srv, host), files, pwds)
		if err := c.Connect(); err != nil {
			t.Fatalf("key-pass %q: the unlocked key was not tried: %v", pwds.KeyPassword, err)
		}
		c.Close()
	}
}

func TestConnectNeverPromptsForARejectedKey(t *testing.T) {
	locked := newKey(t)
	plain := newKey(t)
	host := newKey(t)
	srv := startSSHServer(t, []gossh.Signer{host.signer}, []gossh.PublicKey{plain.pub})
	files := []string{locked.file(t, "secret"), plain.file(t, "")}
	// CanPrompt without a terminal: a prompt would fail and end key auth.
	c := testClient(srv, knownHostFor(t, srv, host), files, SSHPasswords{CanPrompt: true})
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	c.Close()
	if len(srv.offeredKeys()) != 2 {
		t.Fatalf("offered %d keys, want both", len(srv.offeredKeys()))
	}
}

func TestConnectOffersIdentityFilesBeforeAgentKeys(t *testing.T) {
	file := newKey(t)
	host := newKey(t)
	t.Setenv("SSH_AUTH_SOCK", startAgent(t, newKey(t), newKey(t), newKey(t)))

	for _, only := range []bool{false, true} {
		srv := startSSHServer(t, []gossh.Signer{host.signer}, []gossh.PublicKey{file.pub})
		c := testClient(srv, knownHostFor(t, srv, host), []string{file.file(t, "")}, SSHPasswords{})
		c.Config.IdentitiesOnly = only
		if err := c.Connect(); err != nil {
			t.Fatalf("IdentitiesOnly %v: %v", only, err)
		}
		c.Close()
		offered := srv.offeredKeys()
		if len(offered) != 1 || !bytes.Equal(offered[0].Marshal(), file.pub.Marshal()) {
			t.Fatalf("IdentitiesOnly %v: offered %d keys, want the identity file's alone", only, len(offered))
		}
	}
}

func TestConnectHandshakeTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	srv := &testSSHServer{port: l.Addr().(*net.TCPAddr).Port}
	c := testClient(srv, knownHosts(t), nil, SSHPasswords{})
	c.HandshakeTimeout = 200 * time.Millisecond
	start := time.Now()
	if err := c.Connect(); err == nil {
		t.Fatal("connected to a silent server")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("gave up after %v, want about 200ms", elapsed)
	}
}

func testClient(srv *testSSHServer, knownHostsFile string, keys []string, pwds SSHPasswords) *SSHClient {
	return &SSHClient{
		Config: Section{
			Name:            "127.0.0.1",
			Hostname:        "127.0.0.1",
			Port:            srv.port,
			User:            "tester",
			IdentityFiles:   keys,
			KnownHostsFiles: []string{knownHostsFile},
		},
		Passwords: pwds,
	}
}

func serverAddress(srv *testSSHServer) string {
	return knownhosts.Normalize(net.JoinHostPort("127.0.0.1", strconv.Itoa(srv.port)))
}

func knownHostFor(t *testing.T, srv *testSSHServer, host testKey) string {
	t.Helper()
	return knownHosts(t, knownhosts.Line([]string{serverAddress(srv)}, host.pub))
}
