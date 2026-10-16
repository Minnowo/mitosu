// `mitosu passwd`: set the web UI's login password (stored hashed next to
// the database, see auth.PasswordPath). serve reads it at start, so a
// running serve keeps the old password until restarted.

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"mitosu/src/auth"
	"mitosu/src/ssh"
)

// CmdPasswd asks for the new password twice on the terminal, or reads one
// line from stdin with --stdin, and writes its hash.
func CmdPasswd(ctx context.Context, c *cli.Command) error {
	_, cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	var password string
	if c.Bool("stdin") {
		line, err := bufio.NewReader(io.LimitReader(os.Stdin, auth.MaxPassword+2)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		password = strings.TrimRight(line, "\r\n")
	} else {
		first, err := ssh.PromptForPasswordF("New web UI password: ")
		if err != nil {
			return err
		}
		again, err := ssh.PromptForPasswordF("Again: ")
		if err != nil {
			return err
		}
		if string(first) != string(again) {
			return errors.New("the passwords differ; nothing changed")
		}
		password = string(first)
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	path := auth.PasswordPath(cfg.DB)
	if err := auth.WriteFile(path, hash); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "password set in %s; restart serve to use it\n", path)
	return nil
}
