// The web UI's password: hashed with argon2id (RFC 9106) and kept in one
// file next to the database, in the usual PHC string form
// $argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt>$<hash> (base64
// without padding).

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// Parameters for new hashes: 64 MiB, 3 passes, 2 lanes (RFC 9106's
	// second recommended option uses 64 MiB and 3 passes).
	memoryKiB = 64 * 1024
	passes    = 3
	lanes     = 2
	saltBytes = 16
	hashBytes = 32

	// Bounds on parameters read from a file, so an edited file cannot
	// make every login use gigabytes.
	maxMemoryKiB = 1024 * 1024
	maxPasses    = 16

	// MinPassword and MaxPassword bound a new password's length in bytes.
	MinPassword = 8
	MaxPassword = 1024
)

// ErrNoPassword means the password file does not exist.
var ErrNoPassword = errors.New("no password is set")

// PasswordPath returns the password file for the database at db.
func PasswordPath(db string) string {
	return filepath.Join(filepath.Dir(db), "password")
}

// Hash returns the PHC string of password with a new random salt.
func Hash(password string) (string, error) {
	if len(password) < MinPassword || len(password) > MaxPassword {
		return "", fmt.Errorf("the password must be %d to %d bytes long", MinPassword, MaxPassword)
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, passes, memoryKiB, lanes, hashBytes)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memoryKiB, passes, lanes,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verifier checks passwords against one stored hash.
type Verifier struct {
	memory, passes uint32
	lanes          uint8
	salt, key      []byte
}

// ParseHash parses a PHC string written by Hash.
func ParseHash(s string) (*Verifier, error) {
	parts := strings.Split(strings.TrimSpace(s), "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return nil, errors.New("not an argon2id hash")
	}
	var v Verifier
	var m, t, p uint64
	for _, kv := range strings.Split(parts[3], ",") {
		k, val, ok := strings.Cut(kv, "=")
		n, err := strconv.ParseUint(val, 10, 32)
		if !ok || err != nil {
			return nil, errors.New("bad argon2id parameters")
		}
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			p = n
		default:
			return nil, errors.New("bad argon2id parameters")
		}
	}
	if m < 8 || m > maxMemoryKiB || t < 1 || t > maxPasses || p < 1 || p > 255 {
		return nil, errors.New("argon2id parameters out of range")
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return nil, errors.New("bad argon2id salt")
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return nil, errors.New("bad argon2id hash")
	}
	v.memory, v.passes, v.lanes, v.salt, v.key = uint32(m), uint32(t), uint8(p), salt, key
	return &v, nil
}

// Check reports whether password matches, in time that does not depend
// on where it differs. It is slow (about 0.1 s, 64 MiB) by design.
func (v *Verifier) Check(password string) bool {
	if len(password) > MaxPassword {
		return false
	}
	key := argon2.IDKey([]byte(password), v.salt, v.passes, v.memory, v.lanes, uint32(len(v.key)))
	return subtle.ConstantTimeCompare(key, v.key) == 1
}

// ReadFile reads the password file at path; ErrNoPassword if it does not
// exist. The file must belong to the user running mitosu and be readable
// and writable by nobody else (mode 0600 or narrower): a password file
// someone else could plant or edit would let them log in.
func ReadFile(path string) (*Verifier, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoPassword
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s has mode %v; others must not read or write it (chmod 600)", path, info.Mode().Perm())
	}
	if err := checkOwner(path, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v, err := ParseHash(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// WriteFile stores hash at path with mode 0600, replacing any old file
// in one step (a crash leaves the old or the new password, never half).
func WriteFile(path, hash string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".password-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(hash + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
