// Tests for the live views with a fake runner: the exact commands built,
// refused input, parsing of real-format output, secrets left out, and the
// explanations of common failures.

package live

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"mitosu/src/ssh"
)

// fake answers every command with the same output and records them.
type fake struct {
	stdout   string
	stderr   string
	status   int
	timedOut bool
	commands []string
}

func (f *fake) Exec(_ context.Context, host, command string, _ time.Duration, read func(io.Reader) error) (ssh.ExecResult, error) {
	f.commands = append(f.commands, command)
	err := read(strings.NewReader(f.stdout))
	return ssh.ExecResult{Status: f.status, Stderr: f.stderr, ReadErr: err, TimedOut: f.timedOut}, nil
}

func TestBadInputIsRefusedBeforeRunning(t *testing.T) {
	ctx := context.Background()
	f := &fake{}
	bad := []error{
		func() error { _, err := Logs(ctx, f, Options{}, "web", "web; rm -rf /", 10, ""); return err }(),
		func() error { _, err := Logs(ctx, f, Options{}, "web", "-v", 10, ""); return err }(),
		func() error { _, err := Logs(ctx, f, Options{}, "web", "web", 0, ""); return err }(),
		func() error { _, err := Logs(ctx, f, Options{}, "web", "web", MaxLines+1, ""); return err }(),
		func() error { _, err := Inspect(ctx, f, Options{}, "web", "a'b"); return err }(),
		func() error {
			_, err := Journal(ctx, f, Options{}, "web", JournalQuery{Unit: "x $(id)", Priority: -1, Lines: 1})
			return err
		}(),
		func() error {
			_, err := Journal(ctx, f, Options{}, "web", JournalQuery{Unit: "-x", Priority: -1, Lines: 1})
			return err
		}(),
		func() error {
			_, err := Journal(ctx, f, Options{}, "web", JournalQuery{Priority: 8, Lines: 1})
			return err
		}(),
	}
	for i, err := range bad {
		if !errors.Is(err, ErrBadInput) {
			t.Errorf("case %d: %v", i, err)
		}
	}
	if len(f.commands) != 0 {
		t.Fatalf("commands ran: %q", f.commands)
	}
}

func TestWhoSeesAll(t *testing.T) {
	for who, want := range map[string]bool{"0 root": true, "1000 dev adm": true, "1000 dev systemd-journal": true,
		"1000 dev wheel": true, "1000 dev kanshi": false, "": false} {
		if seesAll(who) != want {
			t.Errorf("%q: %v", who, !want)
		}
	}
}

func TestLoginsParsing(t *testing.T) {
	msgs := map[string]Login{
		"Accepted publickey for dev from 172.17.0.1 port 49268 ssh2: ED25519 SHA256:d1bZ": {Result: "accepted", Method: "publickey", User: "dev", From: "172.17.0.1"},
		"Failed password for root from 203.0.113.9 port 4000 ssh2":                        {Result: "failed", Method: "password", User: "root", From: "203.0.113.9"},
		"Failed password for invalid user admin from 203.0.113.9 port 4001 ssh2":          {Result: "failed", Method: "password", User: "admin", From: "203.0.113.9"},
		"Invalid user admin from 203.0.113.9 port 4001":                                   {Result: "invalid_user", User: "admin", From: "203.0.113.9"},
	}
	for msg, want := range msgs {
		if got, ok := parseLogin(msg); !ok || got != want {
			t.Errorf("%q: %+v %v", msg, got, ok)
		}
	}
	// A client-chosen user name cannot fake the address.
	spoof := map[string]Login{
		"Invalid user admin from 6.6.6.6 port 1 from 203.0.113.9 port 51234":                        {Result: "invalid_user", User: "admin from 6.6.6.6 port 1", From: "203.0.113.9"},
		"Failed password for invalid user a b from 6.6.6.6 port 1 from 203.0.113.9 port 51234 ssh2": {Result: "failed", Method: "password", User: "a b from 6.6.6.6 port 1", From: "203.0.113.9"},
		"Invalid user  from 203.0.113.9 port 51234":                                                 {Result: "invalid_user", User: "", From: "203.0.113.9"},
	}
	for msg, want := range spoof {
		if got, ok := parseLogin(msg); !ok || got != want {
			t.Errorf("%q: %+v %v", msg, got, ok)
		}
	}
	for _, msg := range []string{"pam_unix(sshd:session): session opened for user dev(uid=1000) by dev(uid=0)", "Received disconnect from 172.17.0.1 port 49268:11: disconnected by user"} {
		if _, ok := parseLogin(msg); ok {
			t.Errorf("%q parsed as a login", msg)
		}
	}
}

func TestSudoParsing(t *testing.T) {
	got, ok := parseSudo("     dev : PWD=/home/dev ; USER=root ; COMMAND=/usr/bin/true")
	if !ok || got != (SudoEntry{User: "dev", RunAs: "root", Dir: "/home/dev", Command: "/usr/bin/true"}) {
		t.Fatalf("%+v %v", got, ok)
	}
	got, ok = parseSudo("dev : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/bin/sh -c echo a ; echo b")
	if !ok || got.TTY != "pts/0" || got.Command != "/bin/sh -c echo a ; echo b" {
		t.Fatalf("%+v %v", got, ok)
	}
	got, ok = parseSudo("eve : 3 incorrect password attempts ; TTY=pts/1 ; PWD=/home/eve ; USER=root ; COMMAND=/bin/bash")
	if !ok || got.Problem != "3 incorrect password attempts" || got.User != "eve" {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, ok := parseSudo("pam_unix(sudo:session): session opened for user root(uid=0) by dev(uid=1000)"); ok {
		t.Fatal("a PAM line parsed as a sudo use")
	}
}

func TestInspectKeepsOnlyListedFields(t *testing.T) {
	info, err := parseInspect([]byte(inspectFixture))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "s3cret", "POSTGRES_PASSWORD"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("%q reached the result: %s", secret, b)
		}
	}
	if info.Name != "blog-db-1" || info.Image != "postgres:17" || info.Health != "healthy" || info.RestartPolicy != "on-failure:5" ||
		info.Project != "blog" || info.Service != "db" || info.RestartCount != 2 {
		t.Fatalf("info %+v", info)
	}
	if len(info.Ports) != 2 || info.Ports[0].Container != "5432/tcp" || info.Ports[0].HostPort != "5432" ||
		len(info.Networks) != 2 || info.Networks[0].Name != "a_first" || len(info.Mounts) != 1 {
		t.Fatalf("lists %+v %+v %+v", info.Ports, info.Networks, info.Mounts)
	}
}

func TestSystemParsing(t *testing.T) {
	out := marker + "os\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nNAME=\"Debian GNU/Linux\"\n" +
		marker + "kernel\nLinux 6.12.111+deb13-amd64 x86_64\n" +
		marker + "cpus\n4\n" +
		marker + "cpu\nmodel name\t: Intel Core Processor (Haswell)\n" +
		marker + "uptime\n2437380.15 9726157.84\n" +
		marker + "docker\n29.8.2\n" +
		marker + "updates\nListing...\nlibc6/stable-security 2.41-12+deb13u1 amd64 [upgradable from: 2.41-12]\nopenssl/stable 3.5.7-1 amd64 [upgradable from: 3.5.6-1]\n" +
		marker + "reboot\nyes\nlinux-image-6.12.111+deb13-amd64\n" +
		marker + "failed\nfoo.service loaded failed failed Foo\n"
	s := parseSystem(out)
	if s.OS != "Debian GNU/Linux 13 (trixie)" || s.CPUs != 4 || s.CPUModel != "Intel Core Processor (Haswell)" ||
		s.UptimeSeconds != 2437380 || s.DockerVersion != "29.8.2" || !s.RebootRequired || len(s.RebootPackages) != 1 {
		t.Fatalf("%+v", s)
	}
	if len(s.Updates) != 2 || s.Updates[0] != (Upgrade{Package: "libc6", Version: "2.41-12+deb13u1", From: "2.41-12"}) {
		t.Fatalf("updates %+v", s.Updates)
	}
	if len(s.FailedUnits) != 1 || s.FailedUnits[0] != "foo.service" {
		t.Fatalf("failed %+v", s.FailedUnits)
	}
	if script := systemScript(Options{Sudo: true}); !strings.Contains(script, "sudo -n docker version") || !strings.HasSuffix(script, "; true") ||
		strings.Contains(script, "apt update") || strings.Contains(script, "apt-get update") {
		t.Fatalf("script %q", script)
	}
}

const inspectFixture = `[{
 "Id": "94b60a627e8f93112cd35926f2451be1a75bc9057b7b33bd9a21e75ac595d04e",
 "Created": "2026-10-14T19:22:05.5Z", "Path": "/sbin/init", "Args": ["--secret-flag=s3cret"],
 "State": {"Status": "running", "Running": true, "OOMKilled": false, "ExitCode": 0,
  "StartedAt": "2026-10-14T19:22:05.925Z", "FinishedAt": "0001-01-01T00:00:00Z", "Health": {"Status": "healthy"}},
 "Image": "sha256:abc", "Name": "/blog-db-1", "RestartCount": 2,
 "HostConfig": {"RestartPolicy": {"Name": "on-failure", "MaximumRetryCount": 5}},
 "Mounts": [{"Type": "volume", "Source": "/var/lib/docker/volumes/db/_data", "Destination": "/var/lib/postgresql", "RW": true}],
 "Config": {"Image": "postgres:17", "Env": ["POSTGRES_PASSWORD=hunter2", "PATH=/usr/bin"], "Cmd": ["postgres", "--password=hunter2"],
  "Labels": {"com.docker.compose.project": "blog", "com.docker.compose.service": "db", "secret.label": "hunter2"}},
 "NetworkSettings": {"Ports": {"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "5432"}], "9000/tcp": null},
  "Networks": {"blog_default": {"IPAddress": "172.18.0.2"}, "a_first": {"IPAddress": "172.19.0.2"}}}
}]`

const journalHead = `echo "@@mitosu-who:$(id -u) $(id -nG)"; `

func TestCommands(t *testing.T) {
	ctx := context.Background()
	const fields = " --output-fields=MESSAGE,PRIORITY,_SYSTEMD_UNIT,SYSLOG_IDENTIFIER"
	cursor := "s=0638039781c04e1c8b987d885e82ff27;i=2a75;b=63269f57;m=2c3a248c9b;t=65dfe8f7bf1ae;x=34c9f7f761176321"
	cases := []struct {
		name   string
		stdout string
		call   func(r Runner) error
		want   string
	}{
		{"journal", "", func(r Runner) error {
			_, err := Journal(ctx, r, Options{}, "web", JournalQuery{Unit: "ssh.service", Priority: 3, Lines: 100, Before: cursor})
			return err
		}, journalHead + "journalctl --no-pager -o json --all -r -n 100" + fields + " -u 'ssh.service' -p 3 --after-cursor='" + cursor + "'"},
		{"journal sudo", "", func(r Runner) error {
			_, err := Journal(ctx, r, Options{Sudo: true}, "web", JournalQuery{Priority: -1, Lines: 5})
			return err
		}, journalHead + "sudo -n journalctl --no-pager -o json --all -r -n 5" + fields},
		{"logins", "", func(r Runner) error { _, err := Logins(ctx, r, Options{}, "web", 50, ""); return err },
			journalHead + "journalctl --no-pager -o json --all -r -n 50" + fields + " '_COMM=sshd' '_COMM=sshd-session'"},
		{"sudo", "", func(r Runner) error { _, err := Sudo(ctx, r, Options{}, "web", 50, ""); return err },
			journalHead + "journalctl --no-pager -o json --all -r -n 50" + fields + " '_COMM=sudo' '_COMM=sudo-rs'"},
		{"logs newest", exitMarker + "0\n", func(r Runner) error {
			_, err := Logs(ctx, r, Options{Sudo: true}, "web", "blog-web-1", 200, "")
			return err
		}, `{ sudo -n docker logs --timestamps --tail 200 -- 'blog-web-1' 2>&1; echo "@@mitosu-exit:$?"; } | tail -n 201`},
		{"logs older", exitMarker + "0\n", func(r Runner) error {
			_, err := Logs(ctx, r, Options{}, "web", "blog-web-1", 200, "2026-10-16T23:50:23.123456789Z")
			return err
		}, `{ docker logs --timestamps --until 2026-10-16T23:50:23.123456788Z -- 'blog-web-1' 2>&1; echo "@@mitosu-exit:$?"; } | tail -n 201`},
		{"inspect", inspectFixture, func(r Runner) error { _, err := Inspect(ctx, r, Options{}, "web", "db"); return err },
			"docker inspect --type container -- 'db'"},
	}
	for _, c := range cases {
		f := &fake{stdout: c.stdout}
		if err := c.call(f); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if len(f.commands) != 1 || f.commands[0] != c.want {
			t.Errorf("%s: ran %q\nwant %q", c.name, f.commands, c.want)
		}
	}
	if _, err := Logs(ctx, &fake{}, Options{}, "web", "web", 10, "not a time"); !errors.Is(err, ErrBadInput) {
		t.Errorf("bad log cursor: %v", err)
	}
	if _, err := Journal(ctx, &fake{}, Options{}, "web", JournalQuery{Priority: -1, Lines: 1, Before: "a'b"}); !errors.Is(err, ErrBadInput) {
		t.Errorf("bad journal cursor: %v", err)
	}
}

func TestJournalParsing(t *testing.T) {
	// journalctl -r: newest first.
	lines := []string{
		`{"__CURSOR":"c5","__REALTIME_TIMESTAMP":"1792196390000000","MESSAGE":"` + strings.Repeat("x", maxMessage+10) + `"}`,
		`{"__CURSOR":"c4","__REALTIME_TIMESTAMP":"-5","MESSAGE":"bad time"}`,
		`not json`,
		`{"__CURSOR":"c3","__REALTIME_TIMESTAMP":"1792196389000000","PRIORITY":"6","MESSAGE":[104,105,255,10],"SYSLOG_IDENTIFIER":[97,98]}`,
		`{"__CURSOR":"c2","__REALTIME_TIMESTAMP":"1792196388500000","MESSAGE":["one","two"],"_SYSTEMD_UNIT":null}`,
		`{"__CURSOR":"c1","__REALTIME_TIMESTAMP":"1792196388356155","PRIORITY":"3","_SYSTEMD_UNIT":"ssh.service","SYSLOG_IDENTIFIER":"sshd","MESSAGE":"error: kex failed"}`,
	}
	f := &fake{stdout: "@@mitosu-who:1000 dev kanshi\n" + strings.Join(lines, "\n")}
	res, err := Journal(context.Background(), f, Options{}, "web", JournalQuery{Priority: -1, Lines: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Partial || len(res.Entries) != 4 || res.Older != "c1" {
		t.Fatalf("partial %v older %q entries %d", res.Partial, res.Older, len(res.Entries))
	}
	e := res.Entries[0]
	if e.TimeMs != 1792196388356 || e.Priority != 3 || e.Unit != "ssh.service" || e.Ident != "sshd" || e.Message != "error: kex failed" {
		t.Fatalf("oldest entry %+v", e)
	}
	if res.Entries[1].Message != "one\ntwo" || res.Entries[2].Message != "hi?\n" || res.Entries[2].Ident != "ab" || len(res.Entries[3].Message) != maxMessage {
		t.Fatalf("entries %+v", res.Entries[1:3])
	}
	// A page shorter than asked for is the last.
	res, err = Journal(context.Background(), f, Options{}, "web", JournalQuery{Priority: -1, Lines: 50})
	if err != nil || res.Older != "" {
		t.Fatalf("short page older %q %v", res.Older, err)
	}
}

func TestLogsParsing(t *testing.T) {
	out := "WARNING: Error loading config file: open /home/dev/.docker/config.json: permission denied\n" +
		"2026-10-16T23:50:23.123456789Z listening on :8080\n" +
		"2026-10-16T23:50:24.000000000Z GET /\n" +
		exitMarker + "0\n"
	f := &fake{stdout: out}
	res, err := Logs(context.Background(), f, Options{}, "web", "web", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 2 || res.Lines[0].Text != "listening on :8080" || res.Lines[0].TS != "2026-10-16T23:50:23.123456789Z" ||
		res.Older != "2026-10-16T23:50:23.123456789Z" {
		t.Fatalf("result %+v", res)
	}
	res, err = Logs(context.Background(), f, Options{}, "web", "web", 10, "")
	if err != nil || res.Older != "" {
		t.Fatalf("short page older %q %v", res.Older, err)
	}
}

func TestFailuresAreExplained(t *testing.T) {
	ctx := context.Background()
	logs := []struct {
		name string
		f    *fake
		want string
	}{
		{"no docker", &fake{stdout: "sh: 1: docker: not found\n" + exitMarker + "127\n"}, "docker is not installed"},
		{"no docker under sudo", &fake{stdout: "sudo: docker: command not found\n" + exitMarker + "1\n"}, "docker is not installed"},
		{"docker denied", &fake{stdout: "permission denied while trying to connect to the Docker daemon socket\n" + exitMarker + "1\n"}, "group docker"},
		{"sudo password", &fake{stdout: "sudo: a password is required\n" + exitMarker + "1\n"}, "live_sudo is on"},
		{"sudo-rs password", &fake{stdout: "sudo-rs: interactive authentication is required\n" + exitMarker + "1\n"}, "live_sudo is on"},
		{"timeout", &fake{status: -1, timedOut: true}, "did not finish"},
		{"no exit line", &fake{stdout: "2026-10-16T23:50:23Z a\n"}, "without its exit status"},
		{"failed part way", &fake{stdout: "2026-10-16T23:50:23Z secret line\nError grabbing logs: corrupt\x1b[2J\n" + exitMarker + "1\n"}, "Error grabbing logs: corrupt?[2J"},
	}
	for _, c := range logs {
		_, err := Logs(ctx, c.f, Options{}, "web", "web", 10, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "secret line") {
			t.Errorf("%s: container output in the error: %v", c.name, err)
		}
	}
	// Other commands: explained from stderr, only when they failed.
	if _, err := Inspect(ctx, &fake{stdout: inspectFixture, stderr: "WARNING: Error loading config file: permission denied"}, Options{}, "web", "db"); err != nil {
		t.Errorf("a warning on a successful run: %v", err)
	}
	if _, err := Inspect(ctx, &fake{status: 1, stderr: "sudo-rs: interactive authentication is required"}, Options{}, "web", "db"); err == nil || !strings.Contains(err.Error(), "live_sudo is on") {
		t.Errorf("inspect sudo refusal: %v", err)
	}
	if _, err := Inspect(ctx, &fake{stdout: "Welcome to the server\n" + inspectFixture}, Options{}, "web", "db"); err == nil || !strings.Contains(err.Error(), "shell startup files") {
		t.Errorf("greeting before inspect: %v", err)
	}
	if _, err := Inspect(ctx, &fake{status: 1, stderr: "Error: No such container: db"}, Options{}, "web", "db"); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("inspect failure: %v", err)
	}
}
