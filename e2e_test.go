// End-to-end tests of the CLI over real SSH against kanshi's test server.
// They are skipped unless scripts/e2e.sh sets the MITOSU_E2E_* variables,
// and that script fails if any is skipped.

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"mitosu/src/live"
	"mitosu/src/ssh"
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

func TestE2EKanshiVersion(t *testing.T) {
	srv := testServer(t)
	out, stderr, err := runMitosu(t, append([]string{"kanshi", "version"}, srv.flags()...)...)
	if err != nil {
		t.Fatalf("kanshi version: %v\n%s", err, stderr)
	}
	if out != "kanshi 1.0.1, protocol 1.0\n" {
		t.Fatalf("output %q", out)
	}
}

func TestE2EKanshiExportPages(t *testing.T) {
	srv := testServer(t)
	next := uint64(0)
	var lastSeq uint64
	for page := 0; page < 2; page++ {
		args := append([]string{"kanshi", "export", "--since", strconv.FormatUint(next, 10), "--limit", "3"}, srv.flags()...)
		out, stderr, err := runMitosu(t, args...)
		if err != nil {
			t.Fatalf("kanshi export: %v\n%s", err, stderr)
		}
		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("page %d: %d lines, want 3", page, len(lines))
		}
		for _, line := range lines {
			var s struct {
				Type string `json:"type"`
				Seq  uint64 `json:"seq"`
			}
			if err := json.Unmarshal([]byte(line), &s); err != nil || s.Type != "sample" || s.Seq <= lastSeq {
				t.Fatalf("page %d: bad line (err %v): %.120s", page, err, line)
			}
			lastSeq = s.Seq
		}
		want := fmt.Sprintf("3 samples after %d, next %d, more true", next, lastSeq)
		if !strings.Contains(stderr, want) {
			t.Fatalf("summary %q does not contain %q", stderr, want)
		}
		next = lastSeq
	}
}

func TestE2EKanshiOutsiderIsRefused(t *testing.T) {
	srv := testServer(t)
	outsider := os.Getenv("MITOSU_E2E_OUTSIDER_KEY")
	if outsider == "" {
		t.Skip("MITOSU_E2E_OUTSIDER_KEY not set; run scripts/e2e.sh")
	}
	_, stderr, err := runMitosu(t, "kanshi", "export", "--no-prompt",
		"--host", srv.host, "--port", srv.port, "--user", "outsider",
		"--key", outsider, "--known-hosts", srv.knownHosts)
	if err == nil || !strings.Contains(stderr, "kanshi error") {
		t.Fatalf("a login outside group kanshi read the store: err %v\n%s", err, stderr)
	}
}

func TestE2EKanshiShellNoiseIsExplained(t *testing.T) {
	srv := testServer(t)
	srv.user = "noisy"
	for _, sub := range []string{"export", "version"} {
		out, stderr, err := runMitosu(t, append([]string{"kanshi", sub}, srv.flags()...)...)
		if err == nil || !strings.Contains(stderr, "shell startup files") {
			t.Fatalf("%s: err %v\n%s", sub, err, stderr)
		}
		if out != "" {
			t.Fatalf("%s: printed %q for a refused response", sub, out)
		}
	}
}

func TestE2EPullTwice(t *testing.T) {
	srv := testServer(t)
	db := filepath.Join(t.TempDir(), "history", "mitosu.db")
	args := append([]string{"pull", "--db", db}, srv.flags()...)
	_, stderr, err := runMitosu(t, args...)
	if err != nil || !strings.Contains(stderr, "new samples") {
		t.Fatalf("first pull: %v\n%s", err, stderr)
	}
	var first, second int
	if _, err := fmt.Sscanf(stderr[strings.Index(stderr, ": ")+2:], "%d new samples", &first); err != nil || first == 0 {
		t.Fatalf("first pull stored %d samples (%v): %s", first, err, stderr)
	}
	_, stderr, err = runMitosu(t, args...)
	if err != nil {
		t.Fatalf("second pull: %v\n%s", err, stderr)
	}
	if _, err := fmt.Sscanf(stderr[strings.Index(stderr, ": ")+2:], "%d new samples", &second); err != nil || second >= first {
		t.Fatalf("second pull stored %d samples after %d (%v): %s", second, first, err, stderr)
	}
	info, err := os.Stat(db)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database file: %v %v", info, err)
	}
}

func TestE2EServeWithLogin(t *testing.T) {
	srv := testServer(t)
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sshConfig := "Host testbox\n  HostName " + srv.host + "\n  Port " + srv.port + "\n  User " + srv.user +
		"\n  IdentityFile " + srv.key + "\n  UserKnownHostsFile " + srv.knownHosts + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	db := filepath.Join(home, "data", "history.db")
	conf := filepath.Join(home, "mitosu.conf")
	confText := "host = testbox\npull_interval = 30s\ndb = " + db + "\nlisten = 127.0.0.1:" + port + "\n"
	if err := os.WriteFile(conf, []byte(confText), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+home, "SSH_AUTH_SOCK=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME=")

	// Login is on by default: no password, no start.
	refuse := exec.Command(binary, "serve", "--config", conf)
	refuse.Env = env
	if out, err := refuse.CombinedOutput(); err == nil || !strings.Contains(string(out), "mitosu passwd") {
		t.Fatalf("serve without a password: %v\n%s", err, out)
	}
	passwd := exec.Command(binary, "passwd", "--config", conf, "--stdin")
	passwd.Env = env
	passwd.Stdin = strings.NewReader("correct horse\n")
	if out, err := passwd.CombinedOutput(); err != nil {
		t.Fatalf("passwd: %v\n%s", err, out)
	}

	cmd := exec.Command(binary, "serve", "--config", conf)
	cmd.Env = env
	var logs syncBuffer
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	waitLog(t, &logs, "Pulled")

	base := "http://127.0.0.1:" + port
	call := func(method, path, body string, cookie *http.Cookie) *http.Response {
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Mitosu", "1")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	page, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>mitosu</title>") {
		t.Fatalf("web UI: %d %.200s", page.StatusCode, body)
	}
	if resp := call("GET", "/api/hosts", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("hosts without login: %d", resp.StatusCode)
	}
	resp := call("POST", "/api/login", `{"password":"correct horse"}`, nil)
	if resp.StatusCode != http.StatusNoContent || len(resp.Cookies()) != 1 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	resp = call("GET", "/api/hosts", "", resp.Cookies()[0])
	var hosts []struct {
		Name         string `json:"name"`
		LastSampleMs int64  `json:"last_sample_ms"`
		LastError    string `json:"last_error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(hosts) != 1 || hosts[0].Name != "testbox" || hosts[0].LastSampleMs == 0 || hosts[0].LastError != "" {
		t.Fatalf("hosts %+v", hosts)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve did not stop cleanly: %v\n%s", err, logs.String())
	}
	if !strings.Contains(logs.String(), "Stopped") {
		t.Fatalf("no clean stop logged:\n%s", logs.String())
	}
}

func TestE2ELiveViews(t *testing.T) {
	srv := testServer(t)
	get := func(base, path string) (int, map[string]any) {
		req, err := http.NewRequest("GET", base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Mitosu", "1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return resp.StatusCode, body
	}

	// Without sudo: dev is not in adm or systemd-journal.
	base := startServe(t, srv, "live_sudo = off\n")
	code, journal := get(base, "/api/live/journal?host=testbox&lines=20")
	if code != http.StatusOK || journal["partial"] != true {
		t.Fatalf("journal without sudo: %d %v", code, journal)
	}
	code, system := get(base, "/api/live/system?host=testbox")
	if code != http.StatusOK || !strings.Contains(fmt.Sprint(system["os"]), "Debian") || system["cpus"].(float64) < 1 {
		t.Fatalf("system: %d %v", code, system)
	}
	code, logs := get(base, "/api/live/logs?host=testbox&container=kanshi-testserver")
	if code != http.StatusBadGateway || !strings.Contains(fmt.Sprint(logs["error"]), "docker is not installed") {
		t.Fatalf("logs without a docker CLI: %d %v", code, logs)
	}

	// With sudo (dev may sudo without a password on the test server).
	base = startServe(t, srv, "live_sudo = on\n")
	code, journal = get(base, "/api/live/journal?host=testbox&lines=20")
	if code != http.StatusOK || journal["partial"] != false || len(journal["entries"].([]any)) == 0 {
		t.Fatalf("journal with sudo: %d %v", code, journal)
	}
	code, logins := get(base, "/api/live/logins?host=testbox&lines=200")
	if code != http.StatusOK || !strings.Contains(fmt.Sprint(logins["logins"]), "result:accepted") {
		t.Fatalf("logins: %d %v", code, logins)
	}
	code, logs = get(base, "/api/live/logs?host=testbox&container=kanshi-testserver")
	if code != http.StatusBadGateway || !strings.Contains(fmt.Sprint(logs["error"]), "docker is not installed") {
		t.Fatalf("logs through sudo without a docker CLI: %d %v", code, logs)
	}
	code, sudo := get(base, "/api/live/sudo?host=testbox&lines=200")
	if code != http.StatusOK || !strings.Contains(fmt.Sprint(sudo["entries"]), "journalctl") {
		t.Fatalf("sudo (mitosu's own sudo journalctl should be there): %d %.300v", code, sudo)
	}
}

// TestE2ELocalDocker runs the live container views against this
// machine's Docker (the test server has no docker CLI): a throwaway
// container prints 500 numbered lines, the logs view pages back through
// all of them, and inspect leaves out a planted secret.
func TestE2ELocalDocker(t *testing.T) {
	testServer(t)
	const name = "mitosu-e2e-logs"
	const image = "node@sha256:0d1e33586362bb9e608c4e751e7a9e7edb55c9ac11461ed5666af696cc461dd4"
	exec.Command("docker", "rm", "-f", name).Run()
	out, err := exec.Command("docker", "run", "--name", name, "-e", "SECRET=hunter2", "--network", "none", image,
		"sh", "-c", "for i in $(seq 1 500); do echo line $i; done").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })

	ctx := context.Background()
	r := localRunner{}
	var got []string
	before := ""
	for page := 0; page < 10; page++ {
		res, err := live.Logs(ctx, r, live.Options{}, "local", name, 200, before)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		var texts []string
		for _, l := range res.Lines {
			texts = append(texts, l.Text)
		}
		got = append(texts, got...)
		if res.Older == "" {
			break
		}
		before = res.Older
	}
	if len(got) != 500 {
		t.Fatalf("paged %d lines, want 500", len(got))
	}
	for i, text := range got {
		if text != fmt.Sprintf("line %d", i+1) {
			t.Fatalf("line %d is %q", i+1, text)
		}
	}

	info, err := live.Inspect(ctx, r, live.Options{}, "local", name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != name || info.State != "exited" || strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "SECRET") {
		t.Fatalf("inspect %s", b)
	}
}

// localRunner runs live commands with sh on this machine.
type localRunner struct{}

func (localRunner) Exec(ctx context.Context, host, command string, timeout time.Duration, read func(io.Reader) error) (ssh.ExecResult, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ssh.ExecResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return ssh.ExecResult{}, err
	}
	readErr := read(stdout)
	io.Copy(io.Discard, stdout)
	status := 0
	if err := cmd.Wait(); err != nil {
		status = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			status = exitErr.ExitCode()
		}
	}
	return ssh.ExecResult{Status: status, Stderr: stderr.String(), ReadErr: readErr}, nil
}

// startServe starts mitosu serve for the test server (as host testbox,
// login off, with extra config lines) until the test ends, and returns its
// base URL once it has pulled.
func startServe(t *testing.T, srv e2eServer, extra string) string {
	t.Helper()
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sshConfig := "Host testbox\n  HostName " + srv.host + "\n  Port " + srv.port + "\n  User " + srv.user +
		"\n  IdentityFile " + srv.key + "\n  UserKnownHostsFile " + srv.knownHosts + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(sshConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	conf := filepath.Join(home, "mitosu.conf")
	text := "host = testbox\npull_interval = 30s\nlisten = 127.0.0.1:" + port + "\nlogin = off\n" + extra
	if err := os.WriteFile(conf, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "serve", "--config", conf, "--db", filepath.Join(home, "h.db"))
	cmd.Env = append(os.Environ(), "HOME="+home, "SSH_AUTH_SOCK=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME=")
	var logs syncBuffer
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
	})
	waitLog(t, &logs, "Pulled")
	return "http://127.0.0.1:" + port
}

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// waitLog waits up to 60 s for text in logs.
func waitLog(t *testing.T, logs *syncBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !strings.Contains(logs.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("%q not logged within 60 s:\n%s", text, logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer safe for a writing process and a reading
// test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// flags returns the connection flags for the test server as user dev.
func (s e2eServer) flags() []string {
	return []string{"--no-prompt", "--host", s.host, "--port", s.port, "--user", s.user, "--key", s.key, "--known-hosts", s.knownHosts}
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
