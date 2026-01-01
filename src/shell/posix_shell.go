package shell

import (
	"fmt"
)

type PosixShell struct{}

func (PosixShell) Sh() string {
	return "sh"
}

func (PosixShell) Echo(s string) string {
	return fmt.Sprintf("printf '%s'", escapeSingleQuotes(s))
}

func (PosixShell) OrTrue(s string) string {
	// Group the command so '|| true' applies to the whole thing
	// Works even if s contains pipes or &&.
	return fmt.Sprintf("( %s ) || true", s)
}
