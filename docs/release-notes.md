# Release notes

## Unreleased (branch kanshi-reader)

mitosu becomes the reading half of a two-tool monitor with kanshi, its
server-side companion: kanshi keeps a few days of samples on each
server, mitosu pulls them into a long-term history and shows them.
`mitosu stat` keeps working against any server, with or without kanshi.

### New

- `mitosu serve`: pulls every server in mitosu.conf on a timer over one
  kept SSH connection each, stores the history in SQLite with
  configurable downsampling (default: every sample for 30 days, 1-minute
  buckets for a year, 1-hour buckets forever), and serves a web UI on
  127.0.0.1:7878. Runs in the foreground anywhere: shell, systemd
  (deploy/mitosu.service), Docker (deploy/Dockerfile).
- Web UI: an overview of every server with what needs attention, charts
  from 1 hour to 1 year (gaps and stale data shown), containers, events,
  and live views (container logs and configuration, journal, SSH logins,
  sudo uses, system facts, upgradable packages, reboot-required, failed
  units) read over the same login when opened and never stored.
- Problem indicators: disk, inodes, memory, swap, CPU and load high;
  containers dead, unhealthy, restarting, failed or OOM-killed; no new
  samples; pulls failing; problems kanshi reports.
- Login with one password (`mitosu passwd`), or `--no-login`; opt-in TLS;
  `listen` and `allowed_host` for use beyond this machine.
- `mitosu pull`, `mitosu kanshi version|export`, `mitosu check-config`.
- `--config` and `--db` put mitosu's own files (config, database,
  password) where you want them; a `--config` file that does not exist
  is an error.

### Changed (behaviour of `mitosu stat`)

- Host keys are checked against known_hosts; an unknown or changed key is
  refused (it used to accept any key). Connect once with `ssh` to record
  a new server's key.
- `--alias` follows OpenSSH's ssh_config rules (first value wins, Host
  patterns, Include, Match where it can be evaluated); ProxyJump and
  ProxyCommand are refused instead of connecting directly.
- `--user` defaults to the config's User, else the local user (it was
  root); OpenSSH's default key files are tried when no key is given.
- Keys are offered like OpenSSH (identity files, through the agent when
  it holds them, then other agent keys); passphrase-protected keys are
  only unlocked when needed; connecting times out.
- `--no-prompt` no longer asks for a sudo password without
  `--with-root`.
- Control characters and invalid UTF-8 in server output are shown as
  `?`, so a server cannot send terminal escape sequences.

### Fixed

- Crashes on odd server output (short container IDs, empty filesystem
  names, cut-off df lines, missing command output).
- CPU percentages after a reboot (were huge or NaN) and the steal share
  (was always 0).
- An ssh_config alias without Port connected to port 0.

### Requirements

- Building needs Go 1.26 or newer and Docker (the web UI is built with
  npm inside a pinned Node image): `make build`.
- Servers need kanshi 1.0.x (protocol 1.0) for history; kanshi 1.0.1
  exits cleanly when a reader goes away.

See docs/brief.md for every decision and docs/threat-model.md for what
mitosu protects and how.
