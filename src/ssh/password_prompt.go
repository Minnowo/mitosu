package ssh

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

func PromptForPasswordF(format string, args ...any) ([]byte, error) {

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	in := int(os.Stdin.Fd())

	if state, err := term.GetState(in); err != nil {
		return nil, err
	} else {
		defer term.Restore(in, state)
	}

	passCh := make(chan []byte)
	errCh := make(chan error)
	go func() {
		fmt.Fprintf(os.Stderr, format, args...)
		password, err := term.ReadPassword(in)

		if err != nil {
			errCh <- err
			return
		}
		passCh <- password
	}()

	select {
	case password := <-passCh:
		fmt.Fprintln(os.Stderr)
		return password, nil
	case err := <-errCh:
		fmt.Fprintln(os.Stderr)
		return nil, err
	case <-sig:
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		return nil, fmt.Errorf("input cancelled")
	}
}

func PromptForPassword(user, host string) ([]byte, error) {

	return PromptForPasswordF("Enter passphrase for %s@%s: \n", user, host)
}

func PromptForKeyPassword(keypath string) ([]byte, error) {

	return PromptForPasswordF("Enter passphrase for key '%s': \n", keypath)
}
