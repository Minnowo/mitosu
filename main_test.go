// CLI tests: build the mitosu binary once and run it as a user would.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
