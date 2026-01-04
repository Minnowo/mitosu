package ssh

import (
	"errors"

	gossh "golang.org/x/crypto/ssh"
)

var (
	ErrUserEmptyPassword = errors.New("No user password was given, and could not prompt for user password.")
)

func GetPasswordAuthMethod(user, host string, pwds *SSHPasswords) gossh.AuthMethod {

	return gossh.PasswordCallback(func() (string, error) {

		if pwds.UserPassword == "" {

			if !pwds.CanPrompt {
				return "", ErrUserEmptyPassword
			}

			if passwordBytes, err := PromptForPassword(user, host); err != nil {
				return "", err
			} else {
				pwds.UserPassword = string(passwordBytes)
			}
		}

		return pwds.UserPassword, nil
	})
}
