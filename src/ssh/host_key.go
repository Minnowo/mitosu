// Verifies the server's host key against known_hosts files (sshd(8),
// SSH_KNOWN_HOSTS FILE FORMAT; docs/reference/sshd.8.txt). An unknown or
// changed key is refused; mitosu never writes known_hosts.

package ssh

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/rs/zerolog/log"
	gossh "golang.org/x/crypto/ssh"

	"mitosu/src/knownhosts"
)

// ErrUnknownHost is returned (wrapped) when no known_hosts file has a key
// for the host.
var ErrUnknownHost = errors.New("host key is not in known_hosts")

// ErrHostKeyChanged is returned (wrapped) when known_hosts has keys for the
// host but none matches the one the server sent.
var ErrHostKeyChanged = errors.New("host key does not match known_hosts")

// certAlgorithms are the host certificate algorithms offered when a
// @cert-authority entry matches the host.
var certAlgorithms = []string{
	gossh.CertAlgoED25519v01, gossh.CertAlgoSKED25519v01,
	gossh.CertAlgoECDSA256v01, gossh.CertAlgoECDSA384v01, gossh.CertAlgoECDSA521v01,
	gossh.CertAlgoSKECDSA256v01, gossh.CertAlgoRSASHA512v01, gossh.CertAlgoRSASHA256v01,
}

// plainAlgorithms are offered after the certificate algorithms when only
// a CA is known, so a server without a certificate still reaches the
// callback and gets a clear "unknown host" error.
var plainAlgorithms = []string{
	gossh.KeyAlgoED25519, gossh.KeyAlgoECDSA256, gossh.KeyAlgoECDSA384,
	gossh.KeyAlgoECDSA521, gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256,
}

// HostKeyCheck returns the callback that verifies host keys for address
// (host:port as looked up in known_hosts; see Section.KnownHostsAddress)
// against files, and the host key algorithms to offer. The algorithms are
// those of the key types known for address, preceded by the certificate
// algorithms if a @cert-authority entry matches it, so that a server with
// several keys presents one that can be checked; nil if nothing is known.
// hint is the command a user can run to verify and record the key, used
// in the "unknown host" error. Missing files are skipped; unreadable files
// and unparsable lines are skipped with a logged warning.
func HostKeyCheck(files []string, address, hint string) (gossh.HostKeyCallback, []string) {
	db := knownhosts.Open(files...)
	for _, w := range db.Warnings() {
		log.Warn().Msg("known_hosts: " + w)
	}

	types, ca := db.KeyTypes(address)
	var algorithms []string
	if ca {
		algorithms = append(algorithms, certAlgorithms...)
	}
	for _, t := range types {
		algorithms = appendNew(algorithms, algorithmsForKeyType(t)...)
	}
	if ca && len(types) == 0 {
		algorithms = appendNew(algorithms, plainAlgorithms...)
	}

	check := db.Callback()
	callback := func(_ string, remote net.Addr, key gossh.PublicKey) error {
		err := check(address, remote, key)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				return unknownHostError(address, key, files, hint)
			}
			return changedKeyError(address, key, keyErr.Want)
		}
		if err != nil {
			return fmt.Errorf("host key for %s: %w", address, err)
		}
		return nil
	}
	return callback, algorithms
}

// algorithmsForKeyType maps a known_hosts key type to the host key
// algorithms that prove it: an RSA key can sign with SHA-2 or SHA-1.
func algorithmsForKeyType(keyType string) []string {
	if keyType == gossh.KeyAlgoRSA {
		return []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA}
	}
	return []string{keyType}
}

// appendNew appends each of values not already in list.
func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		found := false
		for _, l := range list {
			if l == v {
				found = true
				break
			}
		}
		if !found {
			list = append(list, v)
		}
	}
	return list
}

func unknownHostError(address string, key gossh.PublicKey, files []string, hint string) error {
	return fmt.Errorf("%w: %s (%s %s), checked %s; verify the key and record it by connecting once with: %s",
		ErrUnknownHost, address, key.Type(), gossh.FingerprintSHA256(key), strings.Join(files, ", "), hint)
}

func changedKeyError(address string, key gossh.PublicKey, want []knownhosts.KnownKey) error {
	var known []string
	for _, w := range want {
		known = append(known, fmt.Sprintf("%s %s (%s:%d)", w.Key.Type(), gossh.FingerprintSHA256(w.Key), w.Filename, w.Line))
	}
	return fmt.Errorf("%w: %s sent %s %s; known_hosts has %s. Someone may be intercepting the connection, or the server was reinstalled. Refusing to connect",
		ErrHostKeyChanged, address, key.Type(), gossh.FingerprintSHA256(key), strings.Join(known, ", "))
}
