
# mitosu

See into servers remotely over SSH.

`mitosu` is a remote system monitor inspired by [rtop](https://github.com/rapidloop/rtop).
It connects over SSH to a remote system and displays information (CPU, disk, memory, network, Docker containers).
No special software is needed on the remote system, other than an SSH server and working credentials.

Currently only Linux systems can be monitored, other system support is planned.

## Reading kanshi

[kanshi](../kanshi) keeps a few days of samples on the server. With the
same connection flags as `stat` (the login must be in group `kanshi`):

```
mitosu kanshi version --alias myserver
mitosu kanshi export --alias myserver --since 0 --limit 100 > samples.jsonl
```

`export` checks the whole response against the kanshi protocol (1.x)
and only then prints the sample lines; a response that fails any check
prints nothing. A summary with the `next` value for `--since` goes to
stderr. Output from the login's shell startup files before kanshi's
first line is refused, so startup files must print to stderr or
nothing. `version` gives up after 30 s and `export` after 10 minutes.
Every failure exits with status 1.

## Keeping history

```
mitosu pull --alias myserver
```

stores every sample kanshi has after the last pull (under the name
given to `--alias`, or `host:port` with `--host`) in a local SQLite
database (`$XDG_DATA_HOME/mitosu/mitosu.db`, else
`~/.local/share/mitosu/mitosu.db`, or `--db FILE`; created 0600). Values
are kept raw for 30 days, as 1-minute buckets for a year and as 1-hour
buckets forever. Rates (CPU, network, disk, container CPU) come from two
consecutive samples and are left out across a reboot, a restarted
container or a gap over 15 minutes. Gaps, lost samples, reboots and
container changes are recorded as events. A failed or interrupted pull
keeps what it stored and the next one continues from there.

## Server mode

`mitosu serve` pulls every server listed in `~/.config/mitosu/mitosu.conf`
on a timer and serves a JSON API (the web UI follows) on 127.0.0.1:7878,
behind a login (`mitosu passwd`). See [docs/running.md](docs/running.md)
and the commented [deploy/mitosu.conf](deploy/mitosu.conf);
`mitosu check-config` shows the settings in effect.

mitosu writes only the database and the password file next to it, and
reads only the config file. To keep them anywhere you like:

```
mitosu serve --config /srv/mitosu/mitosu.conf --db /srv/mitosu/history.db
```

`--db` works on `pull`, `serve`, `passwd` and `check-config` and wins
over the config's `db` line.

## Connecting

`--alias <name>` reads `~/.ssh/config` and `/etc/ssh/ssh_config` (or only
`--config FILE`, like `ssh -F`) the way `ssh <name>` does: `Host` blocks,
the first value wins, `Include` (relative to `~/.ssh`), tokens such as
`%h` and `${VAR}`. Used: `HostName`, `Port`, `User`, `IdentityFile`,
`IdentitiesOnly`, `UserKnownHostsFile`, `GlobalKnownHostsFile`,
`HostKeyAlias`. `Match` blocks with `all`, `host`, `originalhost`, `user`
and `localuser` are evaluated; other criteria (such as `exec`) are skipped
with a warning, and refused if the block would change where mitosu
connects. `ProxyJump` / `ProxyCommand` are refused. `--host` alone reads
no config. Flags given explicitly win over the config.

Without `--user` or a `User` line, mitosu logs in as your local user (it
used to default to `root`). Without `--key` or `IdentityFile`, it tries
OpenSSH's default keys (`~/.ssh/id_ed25519` and the others).

The server's host key must already be in `~/.ssh/known_hosts` (or the
files the config names, or `--known-hosts`). An unknown or changed key is
refused; mitosu never adds keys. To trust a new server, check its key and
connect once with `ssh`. Host certificates signed by a `@cert-authority`
entry are accepted.

Like OpenSSH, the identity files are offered first (through the ssh-agent
if it holds them), then the agent's other keys unless `IdentitiesOnly
yes`. A passphrase-protected key is unlocked with `--key-pass`, or with a
prompt only when the server accepts that key (for OpenSSH-format keys or
keys with a `.pub` file beside them); with `--no-prompt` a key that cannot
be unlocked is skipped.

Connecting gives up after 30 s. With `--no-prompt` the SSH handshake also
gives up after 30 s; otherwise it waits, since you may be typing a
password.


![Example output](./pic/stats-all.png)



