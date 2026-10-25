
# mitosu

See into servers remotely over SSH.

`mitosu` is a remote system monitor inspired by [rtop](https://github.com/rapidloop/rtop).
It connects over SSH to a remote system and displays information.
No special software is needed on the remote system, other than an SSH server and working credentials.

For long-term data, a serverside daemon is required.

## Features

mitosu can see the following:
- Hostname
- Uptime
- Load Avg (only when polling / with a daemon)
- Number of running processes
- Memory (total, free, buffers, cached, swap)
- CPU (only when polling / with a daemon)
- Docker container stats (only with root / docker permissions on the user)
- File system stats (path, used, free)
- Network interfaces (ipv4, ipv6, volume of traffic)
- SSH agent integration
- Historical metrics when paired with kanshi daemon

## Usage

The `mitosu stat` command provides quick one-time stats of the server:
```sh
mitosu stat all -a <ssh-alias>
```

Use the `--poll <seconds>` option to get a continuous update of stats (required for proper CPU and load stats to be collected):
```sh
mitosu stat all -a <ssh-alias> --poll 5
```

Use `docker` or `fs` instead of `all` to only see Docker containers or file systems:
```sh
mitosu stat docker -a <ssh-alias>
mitosu stat fs -a <ssh-alias>
```

Without an SSH config, pass the connection details directly (`-p` and `-i` are optional):
```sh
mitosu stat all -H <host> -p 22 -u <user> -i ~/.ssh/id_ed25519
```

Use `--with-root` to run the commands with sudo (needed for Docker stats if the user is not in the `docker` group).
Add `--no-pass-sudo` if the user doesn't need a password for sudo:
```sh
mitosu stat all -a <ssh-alias> --with-root --no-pass-sudo
```

Use `--json` to get output for other tools, and `--no-prompt` to never ask for passwords
(they can be given with `MITOSU_USER_PASSWORD` and `MITOSU_KEY_PASSWORD` instead):
```sh
mitosu stat all -a <ssh-alias> --json --no-prompt
```

>**NOTE**: The server's host key must already be in `known_hosts` (connect once with `ssh` first). Unknown or changed keys are refused.

### History with kanshi

If the server runs kanshi, mitosu can read the history it keeps.

Check that kanshi is reachable:
```sh
mitosu kanshi version -a <ssh-alias>
```

Copy new samples into the local database (`~/.local/share/mitosu/mitosu.db`, change it with `--db`):
```sh
mitosu pull -a <ssh-alias>
```

### Web UI

`mitosu serve` pulls every configured server on a timer and serves a web UI with charts, containers, events and live views (logs, journal, logins).

Copy the example config and add your servers by their SSH alias (`host = <ssh-alias>`, one line each):
```sh
mkdir -p ~/.config/mitosu
cp docs/mitosu.conf ~/.config/mitosu/mitosu.conf
mitosu check-config
```

Set a password for the web UI, then start it:
```sh
mitosu passwd
mitosu serve
```

The web UI is at http://127.0.0.1:7878 by default. See [docs/running.md](docs/running.md) for running it on another machine, TLS and the permissions live views need.

## Building

The web UI is embedded in the binary; build it first (npm runs inside
Docker, see src/ui/README.md):

```
make build      # scripts/web.sh, then go build
```

![Example output](./docs/pic/stats-all.png)

