// End-to-end tests of the CLI over real SSH against kanshi's test server.
// They are skipped unless scripts/e2e.sh sets the MITOSU_E2E_* variables,
// and that script fails if any is skipped.

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type e2eServer struct {
	host, port, user, key, knownHosts string
}

func TestE2EStatJSON(t *testing.T) {
	srv := testServer(t)
	out, stderr, err := runMitosu(t, "stat", "all", "--json", "--no-prompt",
		"--host", srv.host, "--port", srv.port, "--user", srv.user,
		"--key", srv.key, "--known-hosts", srv.knownHosts)
	if err != nil {
		t.Fatalf("stat: %v\n%s", err, stderr)
	}
	var stats []map[string]any
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(stats) != 4 || stats[0]["Hostname"] != "kanshi-test" {
		t.Fatalf("unexpected stats: %.300s", out)
	}
}

func TestE2EAliasFromSSHConfig(t *testing.T) {
	srv := testServer(t)
	config := filepath.Join(t.TempDir(), "config")
	content := "Host testbox\n  HostName " + srv.host + "\n  Port " + srv.port + "\n  User " + srv.user +
		"\n  IdentityFile " + srv.key + "\n  UserKnownHostsFile " + srv.knownHosts + "\n"
	if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runMitosu(t, "stat", "fs", "--json", "--no-prompt", "--alias", "testbox", "--config", config)
	if err != nil {
		t.Fatalf("stat --alias: %v\n%s", err, stderr)
	}
}

func TestE2EUnknownHostIsRefused(t *testing.T) {
	srv := testServer(t)
	empty := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runMitosu(t, "stat", "fs", "--no-prompt",
		"--host", srv.host, "--port", srv.port, "--user", srv.user,
		"--key", srv.key, "--known-hosts", empty)
	if err == nil || !strings.Contains(stderr, "not in known_hosts") {
		t.Fatalf("unknown host not refused: err %v\n%s", err, stderr)
	}
}

func TestE2EChangedHostKeyIsRefused(t *testing.T) {
	srv := testServer(t)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(srv.host + ":" + srv.port)}, other)
	if err := os.WriteFile(file, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runMitosu(t, "stat", "fs", "--no-prompt",
		"--host", srv.host, "--port", srv.port, "--user", srv.user,
		"--key", srv.key, "--known-hosts", file)
	if err == nil || !strings.Contains(stderr, "does not match known_hosts") {
		t.Fatalf("changed key not refused: err %v\n%s", err, stderr)
	}
}

// The server has several host keys; only its RSA key is known here, so
// mitosu must ask for an RSA host key instead of the server's first choice.
func TestE2EOnlyRSAKeyKnown(t *testing.T) {
	srv := testServer(t)
	rsaKnown := os.Getenv("MITOSU_E2E_KNOWN_HOSTS_RSA")
	if rsaKnown == "" {
		t.Skip("MITOSU_E2E_KNOWN_HOSTS_RSA not set; run scripts/e2e.sh")
	}
	_, stderr, err := runMitosu(t, "stat", "fs", "--no-prompt",
		"--host", srv.host, "--port", srv.port, "--user", srv.user,
		"--key", srv.key, "--known-hosts", rsaKnown)
	if err != nil {
		t.Fatalf("stat with only the RSA key known: %v\n%s", err, stderr)
	}
}

func testServer(t *testing.T) e2eServer {
	t.Helper()
	srv := e2eServer{
		host:       os.Getenv("MITOSU_E2E_HOST"),
		port:       os.Getenv("MITOSU_E2E_PORT"),
		user:       os.Getenv("MITOSU_E2E_USER"),
		key:        os.Getenv("MITOSU_E2E_KEY"),
		knownHosts: os.Getenv("MITOSU_E2E_KNOWN_HOSTS"),
	}
	if srv.host == "" || srv.port == "" || srv.user == "" || srv.key == "" || srv.knownHosts == "" {
		t.Skip("MITOSU_E2E_* not set; run scripts/e2e.sh")
	}
	return srv
}

// runMitosu runs the built binary without the caller's ssh agent and
// returns stdout, stderr and the exit error.
func runMitosu(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
