package shell

import "errors"

var (
	ErrNoRootAccess = errors.New("This shell does not have root access")
)

type ShellType int

var (
	PosixShellType ShellType = 0
)

type Shell interface {
	GetType() ShellType

	// Sh returns the platforms 'sh' command
	Sh() string

	// RootSh returns the platforms 'sudo su' command,
	// If canPromptPassword is false, the commmand will be the platforms non-interactive `sudo -n sh` command,
	// If canPromptPassword is true, the command will be the platforms interactive `sudo -P sh` command, exppecting the root password on stdin.
	RootSh(canPromptPassword bool) string

	// Echo returns the platforms 'echo' command echoing the given string
	Echo(s string) string

	// OrTrue appends a '|| true' to the command, making it never fail
	OrTrue(s string) string
}
