// Tests for host key verification against known_hosts files.

package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var serverAddr = &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 2222}

func TestKnownKeyIsAccepted(t *testing.T) {
	key := edKey(t)
	file := knownHosts(t, knownhosts.Line([]string{"[web.example.com]:2222"}, key))
	check, _ := HostKeyCheck([]string{file}, "web.example.com:2222", "ssh web")
	if err := check("ignored", serverAddr, key); err != nil {
		t.Fatalf("known key refused: %v", err)
	}
}

func TestHashedEntryOnPort22IsAccepted(t *testing.T) {
	key := edKey(t)
	file := knownHosts(t, knownhosts.Line([]string{knownhosts.HashHostname("web.example.com")}, key))
	check, _ := HostKeyCheck([]string{file}, "web.example.com:22", "ssh -p 22 admin@web.example.com")
	if err := check("ignored", serverAddr, key); err != nil {
		t.Fatalf("known key refused: %v", err)
	}
}

func TestUnknownHostIsRefused(t *testing.T) {
	file := knownHosts(t, knownhosts.Line([]string{"other.example.com"}, edKey(t)))
	check, algorithms := HostKeyCheck([]string{file}, "web.example.com:22", "ssh -p 22 admin@web.example.com")
	if algorithms != nil {
		t.Errorf("algorithms = %v for an unknown host", algorithms)
	}
	err := check("ignored", serverAddr, edKey(t))
	if !errors.Is(err, ErrUnknownHost) || !strings.Contains(err.Error(), "ssh -p 22 admin@web.example.com") {
		t.Fatalf("err = %v", err)
	}
}

func TestSamePortOnlyMatches(t *testing.T) {
	key := edKey(t)
	file := knownHosts(t, knownhosts.Line([]string{"web.example.com"}, key))
	check, _ := HostKeyCheck([]string{file}, "web.example.com:2222", "ssh web")
	if err := check("ignored", serverAddr, key); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("a port-22 entry was used for port 2222: %v", err)
	}
}

func TestChangedKeyIsRefused(t *testing.T) {
	file := knownHosts(t, knownhosts.Line([]string{"web.example.com"}, edKey(t)))
	check, _ := HostKeyCheck([]string{file}, "web.example.com:22", "ssh -p 22 admin@web.example.com")
	err := check("ignored", serverAddr, edKey(t))
	if !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("err = %v", err)
	}
}

func TestNoKnownHostsFilesRefuses(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	check, _ := HostKeyCheck([]string{missing}, "web.example.com:22", "ssh web")
	if err := check("ignored", serverAddr, edKey(t)); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("err = %v", err)
	}
}

func TestAlgorithmsFollowKnownKeyTypes(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := gossh.NewPublicKey(&ec.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := gossh.NewPublicKey(&rk.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	file := knownHosts(t,
		knownhosts.Line([]string{"web.example.com"}, ecKey),
		knownhosts.Line([]string{"web.example.com"}, rsaKey),
		knownhosts.Line([]string{"other.example.com"}, edKey(t)))
	_, algorithms := HostKeyCheck([]string{file}, "web.example.com:22", "ssh -p 22 admin@web.example.com")
	want := "ecdsa-sha2-nistp256 rsa-sha2-512 rsa-sha2-256 ssh-rsa"
	if strings.Join(algorithms, " ") != want {
		t.Fatalf("algorithms = %v, want %s", algorithms, want)
	}
}

func TestUnparsableLinesAndUnreadableFilesAreSkipped(t *testing.T) {
	key := edKey(t)
	file := knownHosts(t,
		"web.example.com 1024 35 1234567890",
		"web.example.com ssh-unknown-type AAAA",
		knownhosts.Line([]string{"web.example.com"}, key))
	unreadable := knownHosts(t, knownhosts.Line([]string{"web.example.com"}, edKey(t)))
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(unreadable); err == nil {
		f.Close()
		t.Skip("running as root: chmod 0 does not stop reading")
	}
	check, _ := HostKeyCheck([]string{unreadable, file}, "web.example.com:22", "ssh web")
	if err := check("ignored", serverAddr, key); err != nil {
		t.Fatalf("a valid line after bad ones was not used: %v", err)
	}
}

func TestCertificateAuthorityOnly(t *testing.T) {
	ca := edKey(t)
	file := knownHosts(t, "@cert-authority *.example.com "+authorizedLine(ca))
	check, algorithms := HostKeyCheck([]string{file}, "web.example.com:22", "ssh web")
	if len(algorithms) == 0 || algorithms[0] != gossh.CertAlgoED25519v01 {
		t.Fatalf("algorithms = %v, want certificate algorithms first", algorithms)
	}
	if !slices.Contains(algorithms, gossh.KeyAlgoED25519) {
		t.Fatalf("algorithms = %v, want plain ones too so a server without a certificate gets a clear error", algorithms)
	}
	// A plain key from a host trusted only through a CA is unknown, not
	// changed (no intercept warning).
	if err := check("ignored", serverAddr, edKey(t)); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("err = %v", err)
	}
}

func TestCertificateAuthorityAndPlainKey(t *testing.T) {
	plain := edKey(t)
	file := knownHosts(t,
		"@cert-authority *.example.com "+authorizedLine(edKey(t)),
		knownhosts.Line([]string{"web.example.com"}, plain))
	_, algorithms := HostKeyCheck([]string{file}, "web.example.com:22", "ssh web")
	want := append(append([]string{}, certAlgorithms...), gossh.KeyAlgoED25519)
	if strings.Join(algorithms, " ") != strings.Join(want, " ") {
		t.Fatalf("algorithms = %v, want %v", algorithms, want)
	}
}

func authorizedLine(key gossh.PublicKey) string {
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key)))
}

func edKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func knownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	write(t, path, strings.Join(lines, "\n")+"\n")
	return path
}
