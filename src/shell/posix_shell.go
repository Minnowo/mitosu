package shell

import (
	"fmt"
)

type PosixShell struct {
	rootPassword string
}

func NewPosixShell(rootPassword string) PosixShell {
	return PosixShell{
		rootPassword: rootPassword,
	}
}

func (PosixShell) GetType() ShellType {
	return PosixShellType
}
func (PosixShell) Sh() string {
	return "sh"
}

func (PosixShell) RootSh() string {
	return "sudo -S sh"
}

func (s PosixShell) GetRootPassword() (string, error) {

	if s.rootPassword != "" {
		return s.rootPassword, nil
	}

	return "", ErrNoRootAccess
}

func (PosixShell) Echo(s string) string {
	return fmt.Sprintf("printf '%s'", escapeSingleQuotes(s))
}

func (PosixShell) OrTrue(s string) string {
	// Group the command so '|| true' applies to the whole thing
	// Works even if s contains pipes or &&.
	return fmt.Sprintf("( %s ) || true", s)
}
