package ssh

	import gossh "golang.org/x/crypto/ssh"

func GetPasswordAuthMethod(user, host string) (gossh.AuthMethod) {

	return gossh.PasswordCallback( func() (string, error) {

		passwordBytes, err := PromptForPassword(user, host)

		return string(passwordBytes), err
	})
}
