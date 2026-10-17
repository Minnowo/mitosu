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

Only `--config` (read), the database and the `password` file next to it
(written) are used. Defaults: ~/.config/mitosu/mitosu.conf and
~/.local/share/mitosu/mitosu.db; `--config` and `--db` put them
anywhere. The database's directory is created 0700, the files 0600.

## Live views and permissions

The live views (container logs and configuration, journal, SSH logins,
sudo uses, system facts, pending updates, failed units) run fixed
commands over the same SSH login, only when opened, and nothing they
show is stored. What the login needs:

| View                         | Needs                                               |
|------------------------------|-----------------------------------------------------|
| Container logs, configuration | group `docker` (or `live_sudo = on`)               |
| Journal, logins, sudo         | group `adm` or `systemd-journal` (or `live_sudo = on`); otherwise only the login's own entries, and the page says so |
| System                       | nothing special (Docker version needs `docker`)     |

`live_sudo = on` runs `docker` and `journalctl` through `sudo -n`, so
the login needs passwordless sudo for them. Note that group `docker` is
root-equivalent on the server. `live_views = off` hides all of them.

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

An example Dockerfile comes with milestone 7.
