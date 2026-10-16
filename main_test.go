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
