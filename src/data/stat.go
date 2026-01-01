package data

import (
	"mitosu/src/shell"
)

type SystemStat interface {

	// CmdCount returns the number of commands for this shell type
	CmdCount(sh shell.ShellType) int

	// GetCmd gets the command for the given shell type
	GetCmds(sh shell.ShellType) []shell.ShellCmd

	// ParseCmdOutput parses the result of running the commands from GetCmds(sh)
	ParseCmdOutput(sh shell.ShellType, out []string)
}
