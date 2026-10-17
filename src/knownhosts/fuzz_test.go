// Fuzz target for the vendored known_hosts parser: any file must open
// (bad lines skipped) and answer lookups without a panic.

package knownhosts

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzOpen(f *testing.F) {
	f.Add("web.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICBqBdc1nRg3MFJqDxKq1OwOAAAAAAAAAAAAAAAAAAAAAAAA\n")
	f.Add("@cert-authority *.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICBqBdc1nRg3MFJqDxKq1OwOAAAAAAAAAAAAAAAAAAAAAAAA\n|1|abc=|def= ssh-rsa AAAA\n@revoked x ssh-ed25519 AAAA\n")
	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		db := Open(path)
		_, _ = db.KeyTypes("web.example.com:22")
		_, _ = db.KeyTypes("[::1]:2222")
	})
}
