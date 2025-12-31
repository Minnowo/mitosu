package ssh

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func PromptForPassword(user, host string) ([]byte, error) {

	fmt.Printf("Enter passphrase for %s@%s: \n", user)

	return term.ReadPassword(int(os.Stdin.Fd()))
}

func PromptForKeyPassword(keypath string) ([]byte, error) {

	fmt.Printf("Enter passphrase for key '%s': \n", keypath)

	return term.ReadPassword(int(os.Stdin.Fd()))
}
