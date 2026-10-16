// `mitosu pull`: store a host's new kanshi samples in the local history
// once, the way server mode will on a timer.

package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"

	"mitosu/src/kanshi"
	"mitosu/src/pull"
	"mitosu/src/store"
)

// CmdPull pulls a host into the database at --db (default
// store.DefaultPath) with the default tiers, and prints a summary to
// stderr. The host is stored under its --alias, or under host:port when
// only --host is given, so two servers on one address stay apart.
func CmdPull(ctx context.Context, c *cli.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if db := c.String("db"); db != "" {
		if path, err = absPath(db); err != nil {
			return fmt.Errorf("--db: %w", err)
		}
	}

	client, err := connect(c, false)
	if err != nil {
		return err
	}
	defer client.Close()

	host := c.String("alias")
	if host == "" {
		host = net.JoinHostPort(client.Config.Hostname, strconv.Itoa(client.Config.Port))
	}

	db, err := store.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	start := time.Now()
	res, err := pull.Pull(client, db, host, store.DefaultTiers, time.Now)
	if err != nil {
		return fmt.Errorf("pull %s (%d samples stored before the error): %w", kanshi.Printable(host), res.Samples, err)
	}
	reset := ""
	if res.StoreReset {
		reset = ", server store was recreated"
	}
	fmt.Fprintf(os.Stderr, "%s: %d new samples in %d requests (%.1f s), next pull after %d%s; %s\n",
		kanshi.Printable(host), res.Samples, res.Pages, time.Since(start).Seconds(), res.Cursor.Next, reset, path)
	return nil
}
