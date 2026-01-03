package display

import (
	"os"
	"runtime"

	"golang.org/x/term"
)

func SupportsANSI() bool {

	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	if runtime.GOOS == "windows" {
		// modern Windows 10+ supports ANSI
		return true
	}

	termEnv := os.Getenv("TERM")

	if termEnv == "" || termEnv == "dumb" {
		return false
	}

	// probably
	return true
}
