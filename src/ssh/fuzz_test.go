// Fuzz targets for the ssh_config parser and resolver: any text must give
// a result or an error, never a panic or a hang.

package ssh

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzSplitConfigLine(f *testing.F) {
	for _, s := range []string{"Host web", "HostName = 10.0.0.1", `IdentityFile "/a b" # c`, "Port\t22", "x", `"`, "=", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		_, _, _ = splitConfigLine(line)
	})
}

func FuzzResolve(f *testing.F) {
	f.Add("Host web\n  HostName 10.0.0.1\n  Port 2222\n  User admin\n  IdentityFile ~/.ssh/%r@%h:%p\n", "web")
	f.Add("Match host web exec true\n  ProxyJump b\nHost !x *\n  Include x\n", "web")
	f.Add("Host *\n  IdentityFile ${HOME}/%d/%%\n  UserKnownHostsFile none\n", "a.b")
	f.Fuzz(func(t *testing.T, config, name string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config")
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _ = Resolve([]ConfigFile{{Path: path, IncludeBase: dir}}, name, nil)
	})
}

func FuzzWildcardMatch(f *testing.F) {
	f.Add("*a?b*", "xxaybzz")
	f.Fuzz(func(t *testing.T, pattern, s string) {
		_ = wildcardMatch(pattern, s)
	})
}
