# Running mitosu serve

`mitosu serve` is a plain foreground program: it logs to stderr, stops
on Ctrl-C or SIGTERM, and needs nothing from an init system. It needs
`~/.ssh` (config, known_hosts, keys or an ssh-agent) and a writable
directory for the database.

## Once

1. Make sure `ssh myserver kanshi version` works without a prompt (the
   host key is known and the key needs no passphrase, or is in an
   ssh-agent).
2. Copy `deploy/mitosu.conf` to `~/.config/mitosu/mitosu.conf` and set
   `host = myserver`. `mitosu check-config` shows what serve will use.
3. Set the web UI password: `mitosu passwd` (or `login = off` in the
   config, or `--no-login`, to go without one).

## Where files go

mitosu writes only the database and the `password` file next to it,
and reads the config, the SSH files (~/.ssh/config, keys, known_hosts)
and, with TLS, the certificate and key. Defaults:
~/.config/mitosu/mitosu.conf (missing: the defaults; a `--config` file
must exist) and ~/.local/share/mitosu/mitosu.db; `--config` and `--db`
put them anywhere. A new database directory is created 0700 and new
files 0600; an existing database others can read is logged as a warning
at start (SQLite gives its -wal and -shm files the same mode).

## Live views and permissions

The live views (container logs and configuration, journal, SSH logins,
sudo uses, system facts, pending updates, failed units) run fixed
commands over the same SSH login, only when opened, and nothing they
show is stored. What the login needs:

| View                          | Needs                                                        |
|-------------------------------|--------------------------------------------------------------|
| Container logs, configuration | group `docker` (or `live_sudo = on`)                         |
| Journal, logins, sudo         | group `adm` or `systemd-journal` (or `live_sudo = on`) [1]   |
| System                        | nothing special (Docker version needs `docker`)              |

[1] Otherwise only the login's own entries, and the page says so.

`live_sudo = on` runs `docker` and `journalctl` through `sudo -n`, so
the login needs passwordless sudo for them. Note that group `docker` is
root-equivalent on the server. `live_views = off` hides all of them.
The live views' commands need a POSIX login shell (sh, bash, dash, zsh);
with fish or tcsh as the login's shell they fail while pulls keep
working.

## From a shell

```
mitosu serve
```

The log says where the web UI listens (by default http://127.0.0.1:7878).

## With systemd (user service)

`deploy/mitosu.service` is an example unit; its header has the commands.

## On another machine than the browser

Set `listen` to an address the browser can reach and add the name the
browser uses as `allowed_host`. mitosu speaks plain HTTP unless
`tls_cert` and `tls_key` are set; over a network you do not trust, use
TLS, a WireGuard tunnel or a reverse proxy that adds TLS. Keep login on.

## Docker

`deploy/Dockerfile` builds an image (web UI, static binary, Debian slim,
running as an unprivileged user); its header has the build and run
commands. In the container:

- `~/.ssh` is mounted read-only at /home/mitosu/.ssh: its config, the
  servers' host keys in known_hosts, and keys that need no passphrase
  (or mount an ssh-agent socket). IdentityFile paths in that config must
  be the container's paths (/home/mitosu/.ssh/...).
- The config needs `listen = 0.0.0.0:7878` so the published port
  reaches it; publish it on 127.0.0.1 (`-p 127.0.0.1:7878:7878`) unless
  you mean to expose it.
- The history lives in a volume at /data (`--db /data/mitosu.db`).

Checked on 2026-10-17 against kanshi's test server: the container pulled
it and wrote its database 0600 as user mitosu.
