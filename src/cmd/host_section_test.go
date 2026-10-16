// Tests for turning connection flags into settings, with a fake home
// directory holding ~/.ssh/config.

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const homeConfig = "Host *\n  Port 2200\n  User cfguser\nHost box\n  HostName 10.0.0.1\n  Include extra.conf\n"

func TestHostAloneReadsNoConfig(t *testing.T) {
	fakeHome(t)
	s, _, err := resolveHost(hostFlags{host: "box"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Hostname != "box" || s.Port != 22 || s.User == "cfguser" {
		t.Fatalf("--host used the config: %+v", s)
	}
}

func TestAliasReadsHomeConfigAndIncludesFromDotSSH(t *testing.T) {
	home := fakeHome(t)
	s, _, err := resolveHost(hostFlags{alias: "box"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Hostname != "10.0.0.1" || s.Port != 2200 || s.User != "cfguser" {
		t.Fatalf("got %+v", s)
	}
	if len(s.IdentityFiles) != 1 || s.IdentityFiles[0] != filepath.Join(home, "from-extra") {
		t.Fatalf("identity files %q: Include not read from ~/.ssh", s.IdentityFiles)
	}
}

func TestExplicitConfigIsTheOnlyFileAndIncludesStayInDotSSH(t *testing.T) {
	home := fakeHome(t)
	dir := t.TempDir()
	config := filepath.Join(dir, "my_config")
	writeFile(t, config, "Host box\n  Include extra.conf\n")
	writeFile(t, filepath.Join(dir, "extra.conf"), "IdentityFile ~/wrong-base\n")
	s, _, err := resolveHost(hostFlags{alias: "box", config: config, configSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 22 {
		t.Fatalf("port %d: ~/.ssh/config was read besides --config", s.Port)
	}
	if len(s.IdentityFiles) != 1 || s.IdentityFiles[0] != filepath.Join(home, "from-extra") {
		t.Fatalf("identity files %q", s.IdentityFiles)
	}
}

func TestFlagsOverrideConfig(t *testing.T) {
	fakeHome(t)
	f := hostFlags{alias: "box", host: "10.9.9.9", port: 2300, portSet: true, user: "me", key: "/k/id", knownHosts: "/k/known"}
	s, _, err := resolveHost(f)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hostname != "10.9.9.9" || s.Port != 2300 || s.User != "me" {
		t.Fatalf("got %+v", s)
	}
	if strings.Join(s.IdentityFiles, "|") != "/k/id" || strings.Join(s.KnownHostsFiles, "|") != "/k/known" {
		t.Fatalf("keys %q known_hosts %q", s.IdentityFiles, s.KnownHostsFiles)
	}
}

func TestBadPortAndNoHost(t *testing.T) {
	fakeHome(t)
	if _, _, err := resolveHost(hostFlags{host: "box", port: 0, portSet: true}); err == nil {
		t.Fatal("port 0 accepted")
	}
	if _, _, err := resolveHost(hostFlags{}); err == nil {
		t.Fatal("no host accepted")
	}
}

// fakeHome points HOME at a new directory with .ssh/config (homeConfig)
// and .ssh/extra.conf, and returns it.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".ssh", "config"), homeConfig)
	writeFile(t, filepath.Join(home, ".ssh", "extra.conf"), "IdentityFile ~/from-extra\n")
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
