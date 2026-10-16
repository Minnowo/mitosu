// Tests for the order and loading of public keys offered to a server.

package ssh

import (
	"bytes"
	"crypto/rand"
	"path/filepath"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestFilesComeFirstThroughTheAgentCopy(t *testing.T) {
	inAgent := newKey(t)
	agentOnly := newKey(t)
	other := newKey(t)
	sock := startAgent(t, inAgent, agentOnly)
	// inAgent's file is locked and no passphrase is available: it can only
	// be offered through the agent's copy.
	s := Section{IdentityFiles: []string{other.file(t, ""), inAgent.file(t, "secret")}}

	signers, closer := keySigners(sock, s, &SSHPasswords{})
	if closer == nil {
		t.Fatal("no agent connection")
	}
	defer closer.Close()
	checkOrder(t, signers, other, inAgent, agentOnly)

	s.IdentitiesOnly = true
	signers, closer = keySigners(sock, s, &SSHPasswords{})
	defer closer.Close()
	checkOrder(t, signers, other, inAgent)
}

func TestMissingDefaultFilesAreSkipped(t *testing.T) {
	k := newKey(t)
	missing := filepath.Join(t.TempDir(), "id_rsa")
	s := Section{IdentityFiles: []string{missing, k.file(t, "")}, DefaultIdentities: true}
	signers, _ := keySigners("", s, &SSHPasswords{})
	checkOrder(t, signers, k)
}

func TestLockedKeyIsNotOfferedWithoutAWayToUnlockIt(t *testing.T) {
	k := newKey(t)
	s := Section{IdentityFiles: []string{k.file(t, "secret")}}
	for _, pwds := range []*SSHPasswords{{}, {KeyPassword: "wrong"}} {
		signers, _ := keySigners("", s, pwds)
		if len(signers) != 0 {
			t.Fatalf("key-pass %q: a key that cannot be unlocked was offered", pwds.KeyPassword)
		}
	}
}

func TestLockedKeyUnlocksWithKeyPass(t *testing.T) {
	k := newKey(t)
	s := Section{IdentityFiles: []string{k.file(t, "secret")}}
	signers, _ := keySigners("", s, &SSHPasswords{KeyPassword: "secret"})
	checkOrder(t, signers, k)
	sig, err := signers[0].(gossh.AlgorithmSigner).SignWithAlgorithm(rand.Reader, []byte("data"), gossh.KeyAlgoED25519)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.pub.Verify([]byte("data"), sig); err != nil {
		t.Fatal(err)
	}
}

func TestLockedKeyWithPromptIsOfferedLazily(t *testing.T) {
	k := newKey(t)
	s := Section{IdentityFiles: []string{k.file(t, "secret")}}
	signers, _ := keySigners("", s, &SSHPasswords{CanPrompt: true})
	checkOrder(t, signers, k)
	if _, ok := signers[0].(*lazySigner); !ok {
		t.Fatalf("signer is %T, want one that prompts on use", signers[0])
	}
}

func checkOrder(t *testing.T, signers []gossh.Signer, want ...testKey) {
	t.Helper()
	if len(signers) != len(want) {
		t.Fatalf("got %d signers, want %d", len(signers), len(want))
	}
	for i := range want {
		if !bytes.Equal(signers[i].PublicKey().Marshal(), want[i].pub.Marshal()) {
			t.Fatalf("signer %d is not the expected key", i)
		}
	}
}
