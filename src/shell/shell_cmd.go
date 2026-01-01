package shell

type ShellCmd struct {

	// The command to run
	Cmd string

	// Stdin to be passed into the cmd
	Stdin []string
}
