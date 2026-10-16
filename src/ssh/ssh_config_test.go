// Tests for resolving one host's settings from ssh_config files, checked
// against the rules in ssh_config(5).

package ssh

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAliasWithoutPortGetsPort22(t *testing.T) {
	s := resolve(t, "Host web\n  HostName 10.0.0.1\n  User admin\n", "web")
	if s.Port != 22 || s.Hostname != "10.0.0.1" || s.User != "admin" {
		t.Fatalf("got %+v", s)
	}
}

func TestUnknownNameUsesDefaults(t *testing.T) {
	local, err := LocalUser()
	if err != nil {
		t.Fatal(err)
	}
	s := resolve(t, "Host web\n  HostName 10.0.0.1\n", "db.example.com")
	if s.Hostname != "db.example.com" || s.Port != 22 || s.User != local {
		t.Fatalf("got %+v", s)
	}
	if !s.DefaultIdentities || len(s.IdentityFiles) != 5 {
		t.Fatalf("identity files = %v (defaults %v)", s.IdentityFiles, s.DefaultIdentities)
	}
}

func TestFirstValueWins(t *testing.T) {
	config := "Host web\n  User first\nHost *\n  User second\n  Port 2200\nHost web\n  Port 2300\n"
	s := resolve(t, config, "web")
	if s.User != "first" || s.Port != 2200 {
		t.Fatalf("got user %q port %d, want first 2200", s.User, s.Port)
	}
}

func TestGlobalDefaultsBeforeHostWin(t *testing.T) {
	s := resolve(t, "User everyone\nHost web\n  User admin\n", "web")
	if s.User != "everyone" {
		t.Fatalf("user = %q, want the value obtained first", s.User)
	}
}

func TestHostPatterns(t *testing.T) {
	config := "Host !web2 web?\n  Port 1001\nHost *.example.com\n  Port 1002\nHost *\n  Port 1003\n"
	cases := map[string]int{"web1": 1001, "WEB3": 1001, "web2": 1003, "web10": 1003, "a.example.com": 1002, "other": 1003}
	for name, want := range cases {
		if s := resolve(t, config, name); s.Port != want {
			t.Errorf("%s: port %d, want %d", name, s.Port, want)
		}
	}
}

func TestEqualsQuotesAndComments(t *testing.T) {
	s := resolve(t, "Host=web\n  HostName = 10.0.0.9 # the box\n  IdentityFile \"/keys/my key\"\n", "web")
	if s.Hostname != "10.0.0.9" {
		t.Fatalf("hostname = %q", s.Hostname)
	}
	if len(s.IdentityFiles) != 1 || s.IdentityFiles[0] != "/keys/my key" {
		t.Fatalf("identity files = %q", s.IdentityFiles)
	}
}

func TestIdentityFilesAccumulateWithTokens(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	t.Setenv("KEYDIR", "/srv/keys")
	config := "Host web\n  HostName 10.0.0.1\n  User admin\n  Port 2222\n  IdentityFile ~/.ssh/%r@%h:%p\nHost *\n  IdentityFile \"${KEYDIR}/%n %%\"\n"
	s := resolve(t, config, "web")
	want := []string{"/home/tester/.ssh/admin@10.0.0.1:2222", "/srv/keys/web %"}
	if strings.Join(s.IdentityFiles, "|") != strings.Join(want, "|") {
		t.Fatalf("identity files = %q, want %q", s.IdentityFiles, want)
	}
}

func TestUnsupportedTokenIsAnError(t *testing.T) {
	_, _, err := resolveErr(t, "IdentityFile ~/.ssh/%C\n", "web")
	if err == nil || !strings.Contains(err.Error(), "%C") {
		t.Fatalf("err = %v, want one naming %%C", err)
	}
}

func TestUnsetEnvironmentVariableIsAnError(t *testing.T) {
	os.Unsetenv("MITOSU_TEST_UNSET")
	_, _, err := resolveErr(t, "IdentityFile ${MITOSU_TEST_UNSET}/id\n", "web")
	if err == nil {
		t.Fatal("no error for an unset variable")
	}
}

func TestProxyIsRefused(t *testing.T) {
	for _, line := range []string{"ProxyJump bastion", "ProxyCommand nc %h %p"} {
		_, _, err := resolveErr(t, "Host web\n  "+line+"\n", "web")
		if !errors.Is(err, ErrProxyNotSupported) {
			t.Errorf("%s: err = %v", line, err)
		}
	}
	if _, _, err := resolveErr(t, "ProxyJump none\n", "web"); err != nil {
		t.Errorf("ProxyJump none: %v", err)
	}
}

func TestIncludeIsRelativeToItsBaseGlobbedAndConditional(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "conf.d", "b.conf"), "Host web\n  User from-b\n  Port 2001\n")
	write(t, filepath.Join(base, "conf.d", "a.conf"), "Host web\n  User from-a\n")
	// Read only if the Include in the non-matching block were followed;
	// its own Host * line would then make it apply.
	write(t, filepath.Join(base, "other.conf"), "Host *\n  Port 3000\n")
	configDir := t.TempDir()
	write(t, filepath.Join(configDir, "conf.d", "a.conf"), "Host web\n  User wrong-base\n")
	config := filepath.Join(configDir, "config")
	write(t, config, "Host nomatch\n  Include other.conf\nHost *\n  Include conf.d/*.conf\n")

	s, _, err := Resolve([]ConfigFile{{Path: config, IncludeBase: base}}, "web", nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.User != "from-a" || s.Port != 2001 {
		t.Fatalf("got user %q port %d, want from-a 2001", s.User, s.Port)
	}
}

func TestOverridesComeFirst(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	write(t, config, "Host web\n  HostName 10.0.0.1\n  Port 2222\n  User admin\n  IdentityFile /keys/%h\n  IdentityFile /keys/second\n")
	overrides := map[string]string{"hostname": "10.9.9.9", "user": "me", "identityfile": "/mine/%h"}
	s, _, err := Resolve([]ConfigFile{{Path: config, IncludeBase: dir}}, "web", overrides)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hostname != "10.9.9.9" || s.User != "me" || s.Port != 2222 {
		t.Fatalf("got %+v", s)
	}
	if strings.Join(s.IdentityFiles, "|") != "/mine/10.9.9.9" {
		t.Fatalf("identity files = %q, want only the override, expanded with the overriding host", s.IdentityFiles)
	}
}

func TestMatchEvaluatedCriteria(t *testing.T) {
	cases := []struct {
		config string
		port   int
	}{
		{"Match host 10.0.0.*\n  Port 1001\nHost web\n  HostName 10.0.0.1\n", 22},
		{"Host web\n  HostName 10.0.0.1\nMatch host 10.0.0.*\n  Port 1001\n", 1001},
		{"Match originalhost web,db\n  Port 1002\n", 1002},
		{"Match originalhost !web,*\n  Port 1003\n", 22},
		{"Match all\n  Port 1004\n", 1004},
		{"Host web\n  User admin\nMatch user admin host web\n  Port 1005\n", 1005},
		{"Match !originalhost db\n  Port 1006\n", 1006},
	}
	for _, c := range cases {
		if s := resolve(t, c.config, "web"); s.Port != c.port {
			t.Errorf("%q: port %d, want %d", c.config, s.Port, c.port)
		}
	}
}

func TestMatchThatCannotBeEvaluated(t *testing.T) {
	// It cannot route the connection: skipped with a warning.
	s, warnings, err := resolveErr(t, "Match exec \"true\"\n  ForwardAgent yes\n  IdentityFile /x\nHost web\n  Port 7\n", "web")
	if err != nil || s.Port != 7 || len(warnings) != 1 {
		t.Fatalf("port %d warnings %q err %v", s.Port, warnings, err)
	}
	// A setting that is already decided is not affected by it.
	if _, _, err := resolveErr(t, "Host web\n  User admin\nMatch exec \"true\"\n  User other\n", "web"); err != nil {
		t.Fatalf("decided setting: %v", err)
	}
	// It could route the connection: refused.
	for _, line := range []string{"ProxyJump bastion", "HostName 10.1.1.1", "Port 2200", "UserKnownHostsFile /x", "Include other.conf"} {
		if _, _, err := resolveErr(t, "Match host web exec \"true\"\n  "+line+"\n", "web"); err == nil {
			t.Errorf("%s inside an unknown Match block was not refused", line)
		}
	}
	// A failing evaluable criterion decides the block even next to exec.
	if _, _, err := resolveErr(t, "Match host db exec \"true\"\n  ProxyJump bastion\n", "web"); err != nil {
		t.Fatalf("non-matching block: %v", err)
	}
	if _, _, err := resolveErr(t, "Match bogus x\n", "web"); err == nil {
		t.Fatal("unknown criterion accepted")
	}
}

func TestIdentitiesOnly(t *testing.T) {
	if s := resolve(t, "IdentitiesOnly yes\n", "web"); !s.IdentitiesOnly {
		t.Fatal("IdentitiesOnly yes not read")
	}
	if _, _, err := resolveErr(t, "IdentitiesOnly maybe\n", "web"); err == nil {
		t.Fatal("bad IdentitiesOnly value accepted")
	}
}

func TestKnownHostsAddressAndHint(t *testing.T) {
	s := resolve(t, "Host web\n  HostName 10.0.0.1\n  Port 2222\n  User admin\n", "web")
	if s.KnownHostsAddress() != "10.0.0.1:2222" || s.VerifyHint() != "ssh web" {
		t.Fatalf("address %q hint %q", s.KnownHostsAddress(), s.VerifyHint())
	}
	s = resolve(t, "Host web\n  Port 2222\n  HostKeyAlias box\n", "web")
	if s.KnownHostsAddress() != "box:22" {
		t.Fatalf("alias address %q, want the alias without the port", s.KnownHostsAddress())
	}
	s = resolve(t, "User admin\n", "10.0.0.5")
	if s.VerifyHint() != "ssh -p 22 admin@10.0.0.5" {
		t.Fatalf("hint %q", s.VerifyHint())
	}
}

func TestIncludeLoopIsAnError(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	write(t, config, "Include config\n")
	if _, _, err := Resolve([]ConfigFile{{Path: config, IncludeBase: dir}}, "web", nil); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Fatalf("err = %v", err)
	}
}

func TestKnownHostsFiles(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	s := resolve(t, "", "web")
	want := "/home/tester/.ssh/known_hosts|/home/tester/.ssh/known_hosts2|/etc/ssh/ssh_known_hosts|/etc/ssh/ssh_known_hosts2"
	if strings.Join(s.KnownHostsFiles, "|") != want {
		t.Fatalf("defaults = %q", s.KnownHostsFiles)
	}
	s = resolve(t, "UserKnownHostsFile ~/a ~/b\nGlobalKnownHostsFile none\n", "web")
	if strings.Join(s.KnownHostsFiles, "|") != "/home/tester/a|/home/tester/b" {
		t.Fatalf("configured = %q", s.KnownHostsFiles)
	}
}

func TestMalformedLines(t *testing.T) {
	for _, config := range []string{"Port\n", "Host web\n  Port 0\n", "Host web\n  Port 70000\n", "IdentityFile \"/x\n", "Host web\n  User a b\n", strings.Repeat("x", maxConfigLine+1) + "\n"} {
		if _, _, err := resolveErr(t, config, "web"); err == nil {
			t.Errorf("no error for %.40q", config)
		}
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	s, _, err := Resolve([]ConfigFile{{Path: filepath.Join(t.TempDir(), "absent")}}, "web", nil)
	if err != nil || s.Hostname != "web" || s.Port != 22 {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestWildcardMatchIsNotExponential(t *testing.T) {
	pattern := strings.Repeat("*a", 30) + "b"
	if wildcardMatch(pattern, strings.Repeat("a", 5000)) {
		t.Fatal("matched")
	}
}

func resolve(t *testing.T, config, name string) Section {
	t.Helper()
	s, _, err := resolveErr(t, config, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func resolveErr(t *testing.T, config, name string) (Section, []string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	write(t, path, config)
	return Resolve([]ConfigFile{{Path: path, IncludeBase: dir}}, name, nil)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
