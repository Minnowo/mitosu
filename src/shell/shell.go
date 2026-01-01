package shell

type Shell interface {

	// Sh returns the platforms 'sh' command
	Sh() string

	// Echo returns the platforms 'echo' command echoing the given string
	Echo(s string) string

	// OrTrue appends a '|| true' to the command, making it never fail
	OrTrue(s string) string
}
