// Tests for hashing, parsing, checking and storing the password.

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("hash %q", h)
	}
	v, err := ParseHash(h)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Check("correct horse") || v.Check("correct horsE") || v.Check("") {
		t.Fatal("check gave the wrong answer")
	}
	if h2, _ := Hash("correct horse"); h2 == h {
		t.Fatal("two hashes of one password are equal (salt not random)")
	}
}

func TestPasswordLength(t *testing.T) {
	for _, p := range []string{"short", strings.Repeat("x", MaxPassword+1)} {
		if _, err := Hash(p); err == nil {
			t.Errorf("password of %d bytes accepted", len(p))
		}
	}
}

func TestBadHashes(t *testing.T) {
	good, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA",
		strings.Replace(good, "m=65536", "m=99999999", 1),
		strings.Replace(good, "t=3", "t=0", 1),
		strings.Replace(good, "t=3", "x=3", 1),
		strings.Replace(good, "v=19", "v=16", 1),
		good + "$extra",
	}
	for _, s := range bad {
		if _, err := ParseHash(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
}

func TestFile(t *testing.T) {
	dir := t.TempDir()
	path := PasswordPath(filepath.Join(dir, "mitosu.db"))
	if _, err := ReadFile(path); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("missing file: %v", err)
	}
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, h); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode().Perm(), err)
	}
	v, err := ReadFile(path)
	if err != nil || !v.Check("correct horse") {
		t.Fatalf("read back: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %d entries", len(entries))
	}
}

func TestFileMustBePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, h); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0o640, 0o604, 0o660} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFile(path); err == nil {
			t.Errorf("mode %v accepted", mode)
		}
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err != nil {
		t.Errorf("mode 0400: %v", err)
	}
	link := filepath.Join(t.TempDir(), "password")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(link); err == nil {
		t.Error("a symbolic link was followed")
	}
}
