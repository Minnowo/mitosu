// CLI tests: build the mitosu binary once and run it as a user would.

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mitosu-test-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "mitosu")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestHelpListsStat(t *testing.T) {
	out, err := exec.Command(binary, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("mitosu --help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "stat") {
		t.Fatalf("help does not list the stat command:\n%s", out)
	}
}

func TestCheckConfig(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.conf")
	bad := filepath.Join(dir, "bad.conf")
	if err := os.WriteFile(good, []byte("host = web\ntier = raw 7d\ntier = 1h forever\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("host = web\nlogin = maybe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "check-config", "--config", good).Output()
	if err != nil || !strings.Contains(string(out), "host = web\n") || !strings.Contains(string(out), "tier = raw 7d\ntier = 1h forever\n") {
		t.Fatalf("good config: %v\n%s", err, out)
	}
	out, err = exec.Command(binary, "check-config", "--config", bad).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "line 2") {
		t.Fatalf("bad config: %v\n%s", err, out)
	}
}

func TestDBFlagOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "m.conf")
	if err := os.WriteFile(conf, []byte("host = web\ndb = /somewhere/else.db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "mine.db")
	out, err := exec.Command(binary, "check-config", "--config", conf, "--db", db).Output()
	if err != nil || !strings.Contains(string(out), "db = "+db+"\n") {
		t.Fatalf("check-config --db: %v\n%s", err, out)
	}
}

func TestServeFailsFastWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	dir := t.TempDir()
	conf := filepath.Join(dir, "m.conf")
	// 10.255.255.1 never answers: a pull would hang in connect.
	text := "host = 10.255.255.1\nlisten = " + taken.Addr().String() + "\nlogin = off\n"
	if err := os.WriteFile(conf, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "serve", "--config", conf, "--db", filepath.Join(dir, "h.db"))
	cmd.Env = append(os.Environ(), "HOME="+dir)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "address already in use") {
		t.Fatalf("serve on a taken port: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "Serving") {
		t.Fatalf("serve announced itself before its port was open:\n%s", out)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v to fail", elapsed)
	}
}

func TestServeStopsWhileConnecting(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "m.conf")
	// 10.255.255.1 never answers: the first pull waits in connect.
	text := "host = 10.255.255.1\nlisten = 127.0.0.1:" + freePort(t) + "\nlogin = off\n"
	if err := os.WriteFile(conf, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "serve", "--config", conf, "--db", filepath.Join(dir, "h.db"))
	cmd.Env = append(os.Environ(), "HOME="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("serve did not stop within 5 s of SIGTERM while connecting")
	}
}
