// Tests for running remote commands with Exec against the in-process
// server.

package ssh

import (
	"io"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

func TestExecStatusOutputAndStderr(t *testing.T) {
	c := execClient(t, func(cmd string, stdout, stderr io.Writer) (uint32, string) {
		io.WriteString(stdout, "ran "+cmd+"\n")
		io.WriteString(stderr, strings.Repeat("e", maxStderr+100))
		return 3, ""
	})
	var out string
	res, err := c.Exec("kanshi version", 10*time.Second, func(r io.Reader) error {
		b, err := io.ReadAll(r)
		out = string(b)
		return err
	})
	if err != nil || res.ReadErr != nil {
		t.Fatalf("exec: %v, read: %v", err, res.ReadErr)
	}
	if out != "ran kanshi version\n" || res.Status != 3 || len(res.Stderr) != maxStderr {
		t.Fatalf("out %q status %d stderr %d bytes", out, res.Status, len(res.Stderr))
	}
}

func TestExecStopsACommandWhenReadGivesUp(t *testing.T) {
	c := execClient(t, func(cmd string, stdout, stderr io.Writer) (uint32, string) {
		chunk := []byte(strings.Repeat("x", 4096))
		for {
			if _, err := stdout.Write(chunk); err != nil {
				return 0, ""
			}
		}
	})
	done := make(chan ExecResult, 1)
	go func() {
		res, _ := c.Exec("endless", 10*time.Second, func(r io.Reader) error {
			_, err := r.Read(make([]byte, 10))
			return err
		})
		done <- res
	}()
	select {
	case res := <-done:
		if res.Status != -1 {
			t.Fatalf("status %d for a cut-off command, want -1", res.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Exec did not return after read gave up")
	}
}

func TestExecTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c := execClient(t, func(cmd string, stdout, stderr io.Writer) (uint32, string) {
		io.WriteString(stdout, "first line\n")
		<-release
		return 0, ""
	})
	start := time.Now()
	res, err := c.Exec("hangs", 300*time.Millisecond, func(r io.Reader) error {
		_, err := io.ReadAll(r)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.Status != -1 {
		t.Fatalf("result %+v, want a timeout", res)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
}

func TestExecSignal(t *testing.T) {
	c := execClient(t, func(cmd string, stdout, stderr io.Writer) (uint32, string) {
		return 0, "KILL"
	})
	res, err := c.Exec("killed", 10*time.Second, func(r io.Reader) error {
		_, err := io.ReadAll(r)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != -1 || res.Signal != "KILL" {
		t.Fatalf("result %+v", res)
	}
}

func TestExecTimeoutWhileOpeningTheSession(t *testing.T) {
	host := newKey(t)
	user := newKey(t)
	srv := startServer(t, []gossh.Signer{host.signer}, []gossh.PublicKey{user.pub}, nil, true)
	c := testClient(srv, knownHostFor(t, srv, host), []string{user.file(t, "")}, SSHPasswords{})
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	res, err := c.Exec("never runs", 300*time.Millisecond, func(r io.Reader) error { return nil })
	if err == nil || !res.TimedOut {
		t.Fatalf("result %+v err %v, want a timeout", res, err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("returned after %v", elapsed)
	}
}

// execClient connects to a new in-process server running exec.
func execClient(t *testing.T, exec execHandler) *SSHClient {
	t.Helper()
	host := newKey(t)
	user := newKey(t)
	srv := startExecServer(t, []gossh.Signer{host.signer}, []gossh.PublicKey{user.pub}, exec)
	c := testClient(srv, knownHostFor(t, srv, host), []string{user.file(t, "")}, SSHPasswords{})
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
