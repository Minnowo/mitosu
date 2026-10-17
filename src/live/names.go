// Checks on the few values that go into live commands: container names,
// systemd unit names, line counts and times. Anything else is refused
// before a command is built.

package live

import (
	"fmt"
	"regexp"
)

var (
	// containerPattern is Docker's container name rule (kanshi's protocol
	// sends names in this form).
	containerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	// unitPattern is a systemd unit name: letters, digits and :-_.\@
	// (no shell characters, no leading '-').
	unitPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9:_.@\\-]{0,255}$`)
)

// checkContainer accepts a container name.
func checkContainer(name string) error {
	if !containerPattern.MatchString(name) {
		return fmt.Errorf("%w: container name %q", ErrBadInput, name)
	}
	return nil
}

// checkUnit accepts a systemd unit name.
func checkUnit(name string) error {
	if !unitPattern.MatchString(name) {
		return fmt.Errorf("%w: unit name %q", ErrBadInput, name)
	}
	return nil
}

// checkLines accepts a line count of 1 to MaxLines.
func checkLines(n int) error {
	if n < 1 || n > MaxLines {
		return fmt.Errorf("%w: lines must be 1 to %d", ErrBadInput, MaxLines)
	}
	return nil
}

// quote single-quotes a value that passed a check above (they hold no
// quote, so this only keeps the shell from splitting or globbing).
func quote(s string) string {
	return "'" + s + "'"
}
