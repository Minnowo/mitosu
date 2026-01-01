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

	// RootSh returns the platforms 'sudo -S su' command,
	// It expects a stdin root password immediately after running the command.
	RootSh() string

	// GetRootPassword returns the root password, or a ErrNoRootAccess error
	GetRootPassword() (string, error)

	// Echo returns the platforms 'echo' command echoing the given string
	Echo(s string) string

	// OrTrue appends a '|| true' to the command, making it never fail
	OrTrue(s string) string
}
