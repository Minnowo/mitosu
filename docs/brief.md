# Project brief: mitosu (reader for kanshi)

Status: approved (2026-10-17) after two review rounds the same day
(listen address, tiers, runs anywhere; login, TLS, npm in Docker)

The single source of truth for what this round of mitosu work is and why.
Decisions are recorded with their reasons so they can be revisited.
The server side is kanshi (`../kanshi`, its own brief in
`../kanshi/docs/brief.md`); the contract between the two is
`../kanshi/docs/protocol.md` (format 1.0, frozen).

Workspace: `~/Documents/kanshi/mitosu` is a clone of
github.com/Minnowo/mitosu (Go, AGPL-3.0). Work happens on the local
branch `kanshi-reader`; nothing is pushed.

## 1. Idea

- **Pitch:** mitosu is the reading half of a two-tool monitor. It keeps
  its quick agentless CLI (`mitosu stat`) and gains a server mode
  (`mitosu serve`): a long-running process that pulls `kanshi export`
  over SSH, keeps the history in a local SQLite database (downsampled as
  it ages, on a schedule you choose), and serves a web UI (127.0.0.1 by
  default, any address by config). Things kanshi
  deliberately does not collect (logs, journal, logins, sudo, system
  facts, container config, pending updates, failed units) are read live
  over the same admin login when you open them, and never stored.
- **User:** just the owner, one home machine (Debian 13, amd64), one
  server today (OVH_4gb).
- **Problem:** mitosu shows only the present. kanshi now keeps 7 days on
  the server, but nothing keeps it longer or draws it.
- **v1 is done when:**
  - `mitosu serve` runs in the foreground anywhere (shell, systemd,
    Docker, tmux), pulls every
    configured host over a verified, reused SSH connection, and survives
    the server or the home box being offline (kanshi's buffer fills the
    gap; real losses are shown, not hidden);
  - the web UI shows host and container charts for any range
    from the last hour to the last year, marks gaps and stale data, and
    flags problems derived from the samples (disk low, CPU high, container
    unhealthy, restarting or OOM-killed, kanshi stale);
  - the live views (section 3) work for the admin login;
  - `mitosu stat` still works against a host without kanshi.
- **Hard constraints:**
  - Go, golang.org/x/crypto/ssh (kept and hardened, user decision).
  - Host keys verified against known_hosts; unknown or changed keys are
    refused (user decision 2026-10-17).
  - Listens on 127.0.0.1 unless configured otherwise; a login is
    required unless started with `--no-login`; TLS is opt-in. Securing
    the network is the user's call, mitosu makes it easy. No cloud
    services.
  - No dependency on systemd or any init system: works wherever a static
    Go binary, ~/.ssh and a writable data directory are available.
  - Secrets in logs, journal and `docker inspect` (environment) never
    reach the database.
  - License AGPL-3.0.

## 2. Current state of mitosu (read 2026-10-17)

About 2,500 lines of Go, 16 commits, no tests. Dependencies: zerolog,
urfave/cli/v3, x/crypto, x/term.

- `mitosu stat [all|docker|fs]`: opens one SSH session, starts `sh` (or
  `sudo sh`), writes commands (`cat /proc/...`, `df`, `ip -o addr`,
  `docker stats --no-stream`) separated by random markers, parses the
  text. `--poll N` repeats in a small terminal navigator; `--json`.
- Problems found while reading (each gets a test before its fix, M1):
  - `HostKeyCallback` accepts every key.
  - ssh_config parser: no `Include`, wildcards or `Match`; an alias
    without `Port` gets port 0; an alias that is not found silently ends
    with "Could not get ssh connection".
  - Possible panics on hostile output: `ct.ID[0:12]` on a short ID,
    `fs.Filesystem[0]` on an empty field.
  - `--user` defaults to `root`.
  - No dial timeout, no keepalive.

## 3. Architecture decisions

| Dimension          | Decision                                                                                                                                                                       | Options considered                                     | Reason                                                                     | By        |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------|----------------------------------------------------------------------------|-----------|
| Modes              | One binary: `mitosu stat` (quick CLI, unchanged in spirit), `mitosu pull <host>` (one-shot ingest), `mitosu serve` (pull loop + web UI), `mitosu check-config`               | Separate daemon binary                                 | One tool, the CLI stays quick                                              | user      |
| Server mode        | Always-on foreground process: logs to stderr, stops cleanly on SIGINT/SIGTERM, all paths from flags or config (XDG defaults); no sd_notify or other init hooks. Example setups: systemd --user unit, Docker (keys and known_hosts mounted read-only), plain shell | systemd-only service; only while the UI is open       | Runs anywhere; history stays complete while it runs                       | user      |
| Pull               | Every 5 min per host (configurable), `export --since <next> --limit 5000 --gzip`, looping while `more`; one SSH connection per host, reused, reconnect with backoff            | Every 10 s (live); hourly                              | kanshi buffers 7 days; 5 min keeps the UI fresh enough at little cost     | default   |
| Live data          | The UI's "now" view uses the last pulled sample plus a "pull now" button                                                                                                       | Streaming kanshi                                       | No new kanshi request needed                                               | default   |
| Store              | SQLite via modernc.org/sqlite (pure Go, no cgo), one file under `$XDG_DATA_HOME/mitosu` (or `data_dir`)                                                                                       | Own files; mattn (cgo); bbolt                          | SQL range queries for the UI; static CGO_ENABLED=0 builds keep working    | user      |
| Store contents     | Derived values, not raw JSON: rates from counter deltas (CPU %, bytes/s, IO/s) and gauges (memory, disk, load), per host, core, filesystem, disk, interface, container        | Raw kanshi lines as blobs                              | Small and directly chartable; raw lines can be re-pulled within 7 days     | default   |
| Tiers              | Fully configurable list of tiers, each a resolution and how long it is kept, e.g. `tier = raw 180d`, `tier = 1m 2y`, `tier = 1h forever`. Every tier is filled at ingest, so a coarser tier already exists when a finer one expires; `check-config` refuses tiers that are not coarser and kept longer down the list. Defaults: raw 30 d, 1 min 1 year, 1 h forever | Fixed tiers; rolling up only on expiry                  | Storage is cheap at home; the user decides the trade (section 3a)          | user      |
| Identity           | Host by its configured name; container by compose project + service (else name); cursor per host is (store ID, next seq)                                                       | Container ID                                           | IDs change on every `compose up`                                           | default   |
| Counter resets     | New `boot_id`, container restart or a counter going down: no rate for that step                                                                                                | Guess across the reset                                 | A wrong spike is worse than a missing point                                |           |
| Gaps               | Stored explicitly: seq gaps, `lost_through`, a new store ID, and pull failures are rows the UI draws as gaps                                                                   | Interpolate                                            | Silence must not look like "fine" (same rule as kanshi)                   | default   |
| Web server         | Go net/http; JSON API + the built UI embedded with go:embed. `listen = 127.0.0.1:7878` by default, any address allowed. Host header must match the listen address, localhost or an `allowed_host` line (DNS rebinding); API calls need a custom header (blocks cross-site requests) | Loopback only; separate static server                 | Single binary; reachable where the user chooses                            | user      |
| Login              | One account. `mitosu passwd` stores a password hash (argon2id from x/crypto, existing dependency) in the data directory; login page sets a session cookie (HttpOnly, SameSite=Strict, Secure under TLS), failed logins rate-limited. `serve` refuses to start without a password unless `--no-login` (or `login = off`) is given, whatever the listen address | Password only off loopback; Basic auth                  | Easy to secure, easy to skip (user, 2026-10-17)                            | user      |
| TLS                | Opt-in: `--tls-cert` / `--tls-key` (or `tls_cert` / `tls_key`) serve HTTPS with Go's crypto/tls; plain HTTP by default                                                         | Reverse proxy only; always TLS                          | Go does it well; HTTP must keep working without setup                     | user      |
| UI stack           | TypeScript, Preact, Vite, Tailwind 4 with a ponkotsu-generated theme, uPlot for charts (vendored via npm, bundled; no CDN)                                                     | Solid; no framework; Chart.js; hand-written SVG        | See section 4                                                              | proposed  |
| Live views         | Fixed command templates run over the admin login, never stored: `docker logs --tail N` per container, `journalctl` (unit, priority, time window), SSH logins (sshd and sshd-session) and sudo / sudo-rs lines from the journal, /etc/os-release + `uname -r` + /proc/cpuinfo, `docker inspect` without `Config.Env`, `apt list --upgradable` (cache only, never `apt update`), /var/run/reboot-required, `systemctl --failed` | kanshi collecting them (rejected in kanshi's brief)    | They need the admin's privileges; kanshi stays unprivileged                | user      |
| Live view input    | Container names only from the last pulled sample, validated (`[A-Za-z0-9_.-]`), single-quoted; line counts and time windows are numbers with caps                               | Free text from the UI                                  | Nothing from the browser reaches a shell unchecked                         | default   |
| Privilege          | Live views run without sudo; on "permission denied" the UI says which group is missing (docker, adm/systemd-journal) or that `sudo -n` can be enabled per host; server mode never prompts for a password | Always sudo                                            | Least privilege; the service cannot type passwords                         | default   |
| Host keys          | knownhosts from x/crypto against ~/.ssh/known_hosts and /etc/ssh/ssh_known_hosts (hashed entries and `[host]:port` supported); unknown or changed key refused with a message to run `ssh <host>` once | Prompt in CLI; accept-new                              | Never writes known_hosts; MITM refused                                     | user      |
| Alerts             | Derived indicators in the UI only; no notifications in v1                                                                                                                      | Email, desktop notifications                           | Same "on-demand first" rule as kanshi                                      | default   |
| Config             | `$XDG_CONFIG_HOME/mitosu/mitosu.conf` (or `--config`), strict `key = value` lines like kanshi's; `host = <ssh alias>` and `tier` repeatable; pull interval, data directory, listen, allowed_host, login, tls_cert, tls_key, live_views; flags override the file; `mitosu check-config` | TOML/YAML (dependency)                                 | No parser dependency; same habits as kanshi                                | default   |
| Distribution       | Static binary (CGO_ENABLED=0) for the platforms modernc.org/sqlite supports; docs for systemd --user, Docker and a plain shell; an example Dockerfile in M7                   | Docker only; systemd only                              | Works anywhere (user, 2026-10-17)                                          | user      |
| Tests              | Go tests at the CLI and HTTP API level; integration tests against kanshi's test server (127.0.0.1:2222, run by `../kanshi/testserver/run.sh`); kanshi's golden files copied to testdata/ with a check that they match | Mocks only                                             | Real sshd, real kanshi, real known_hosts handling                          | default   |

### 3a. Storage estimate (one host like OVH_4gb; estimated, measured in M3)

About 23 rows per sample (host, 4 cores, 2 filesystems, 1 disk, 8
interfaces, 7 containers), roughly 60 bytes each in SQLite with indexes:
about 1.4 KB per sample, 12 MB per day at a 10 s interval. 1 min and 1 h
rows hold avg, min and max (about 1.5x a raw row) at a sixth and a 360th
of the row count.

| Tiers                                   | Size                                  |
|-----------------------------------------|---------------------------------------|
| raw 30 d, 1 min 1 y, 1 h forever (dflt) | 0.4 GB + 1.1 GB, then +0.02 GB a year |
| raw 180 d, 1 min 2 y, 1 h forever       | 2.2 GB + 2.2 GB, then +0.02 GB a year |
| raw forever                             | +4.4 GB a year                        |

### Non-goals (v1)

- Notifications and alert rules.
- Writing anything to the server (no restart buttons, no `apt update`).
- Storing logs, journal lines or `docker inspect` output.
- Multiple user accounts, built-in TLS (use a proxy or WireGuard).
- Windows / macOS servers (the CLI keeps building for them as today).

### Diagram

```
 server (OVH_4gb)                          home machine
+---------------------------+            +------------------------------------+
| kanshi collect -> store   |   ssh      | mitosu serve (any init / shell)    |
| kanshi export (group      | <--------- |  pull loop, 1 conn per host        |
|  kanshi, read-only)       |  admin     |  -> SQLite, tiers as configured    |
|                           |  login     |  HTTP on `listen` (127.0.0.1 dflt) |
| docker logs, journalctl,  | <--------- |   /api/... JSON  + embedded UI     |
| apt, systemctl (live,     |            |  live views: fixed templates,      |
|  when a view is opened)   |            |   never stored                     |
+---------------------------+            +------------------------------------+
                     browser -> listen address (login unless --no-login, TLS opt-in)
```

## 4. Frontend: recommendation (needs your OK)

Preact + TypeScript + Vite + Tailwind 4 + ponkotsu + uPlot.

- ponkotsu's own demo app already uses Preact, Vite and Tailwind 4, so the
  toolchain is one you know and the theme drops in as-is.
- The UI is a handful of pages refreshed every few minutes. Solid's
  fine-grained reactivity pays off with many fast updates; here it does
  not, and its rules (no destructuring props, effects instead of
  re-renders) are easier to get wrong. Preact is about 4 KB with plain
  hooks.
- uPlot is imperative either way: one small `<Chart>` component owns a
  uPlot instance, so the framework hardly touches the charts.
- No framework would also work for charts, but the log viewer, filters,
  host switching and range picker would turn into hand-written DOM
  bookkeeping.
- No router or state library: hash routes and hooks are enough.

npm packages (need approval): preact, uplot; dev: vite,
@preact/preset-vite, tailwindcss, @tailwindcss/vite, typescript.
Versions are pinned in package-lock.json. The ponkotsu theme is generated
once and committed as a CSS file (no runtime dependency on ponkotsu).

Build: `web/` holds the app; go:embed takes `web/dist`. npm runs inside a
Debian-based Node image (`node:26-trixie-slim`, pinned by digest) through
the Makefile, as the user's own projects do: `docker run --rm --user
<uid>:<gid> -e HOME=/tmp -v web:/ui -w /ui <image> npm ...`, or plain
`npm` when already inside a container (/.dockerenv). Nothing is
installed on the host. `make build` runs `npm ci` and `npm run build`
that way, then `go build`. `web/dist` is not committed; a plain `go
build` without it fails with a clear message (check.sh builds both).

## 5. Dependencies

| Dependency                 | Use                    | License        | Status                                |
|----------------------------|------------------------|----------------|---------------------------------------|
| golang.org/x/crypto        | SSH, knownhosts, agent | BSD-3          | existing                              |
| golang.org/x/term          | password prompt, TUI   | BSD-3          | existing                              |
| github.com/rs/zerolog      | logging                | MIT            | existing                              |
| github.com/urfave/cli/v3   | CLI                    | MIT            | existing                              |
| modernc.org/sqlite         | store                  | BSD-3          | chosen by user 2026-10-17             |
| npm packages (section 4)   | web UI                 | MIT            | proposed                              |

modernc.org/sqlite pulls in modernc.org/libc, mathutil, memory,
github.com/dustin/go-humanize, github.com/google/uuid,
github.com/ncruces/go-strftime, github.com/remyoudompheng/bigfft
(BSD-3 or MIT; all compatible with AGPL-3.0). An allowlist in
scripts/check.sh fails the build on anything else, as in kanshi.

Vendored: src/knownhosts is x/crypto v0.46.0's ssh/knownhosts/knownhosts.go
(BSD-3-Clause, license beside it) with the changes listed in its header
(M1 review, 2026-10-17).

## 6. Milestones

Each ends with check.sh green, an independent review, and a commit.

- **M0 Setup.** CLAUDE.md (kanshi's core rules plus Go and TypeScript
  notes), scripts/check.sh (gofmt, go vet, go test -race, build, ASCII,
  dependency allowlist), test helpers that use kanshi's test server.
- **M1 SSH hardening.** Host-key verification as decided; dial timeout and
  keepalive; one connection with many sessions; ssh_config fixes (default
  port 22, default user = local user, clear "alias not found"); test-first
  fixes for the problems in section 2. `mitosu stat` against the test
  server in an integration test.
- **M2 kanshi reader.** A Go package for protocol 1.0: first line must be
  `header`/`version`/`error` (anything printed by shell startup files is
  refused), unknown fields ignored, unknown major refused, gzip, `--limit`
  paging, `end` required, `lost_through`, store ID change. Tested against
  kanshi's golden files and against real kanshi on the test server. Go
  native fuzzing (no dependency) on the line parser.
- **M3 Store.** SQLite schema and migrations, ingest (rates, resets,
  gaps), tiers and retention, `mitosu pull <host>`. Measure the database
  size on a copy of the real server's 7 days.
- **M4 Server mode.** Config file and `check-config`, pull loop with
  backoff, HTTP JSON API with the Host and header checks, `listen`,
  login (`mitosu passwd`, `--no-login`), opt-in TLS, signal handling; run notes for a plain
  shell and systemd --user (Docker in M7).
- **M5 Web UI.** Overview per host, charts with a range picker (1 h to 1
  year, tier chosen by range), containers, gaps and stale markers, derived
  problem indicators. Checked with screenshots.
- **M6 Live views.** The fixed templates in section 3, with caps (lines,
  bytes, time) and the permission messages.
- **M7 Hardening.** Threat model, fuzzing runs, review, example
  Dockerfile, real-server run over a week, release notes.

## 7. Decisions at approval (2026-10-17)

- Frontend stack as in section 4.
- Tiers fully configurable; long raw retention allowed (many GB is fine).
- `listen` configurable; runs anywhere, not only under systemd.
- Login with one account by default, `--no-login` skips it; TLS opt-in
  with Go's crypto/tls; "not our job to secure the user's network, just
  make it easy".
- npm only inside a Docker image via the Makefile.
- Defaults taken without an explicit answer: `web/dist` not committed;
  work on the local branch `kanshi-reader`, committed locally, never
  pushed.

## 8. Changes during implementation

- 2026-10-17, M0: CLAUDE.md (kanshi's core rules plus mitosu specifics),
  scripts/check.sh (gofmt -s, go vet, go test -race with a "no tests ran"
  check, static build plus cross builds for the goreleaser targets,
  ASCII, dependency allowlist, package layering from scripts/layers.txt)
  and a first CLI test (main_test.go builds the binary and runs it). Each
  rule was checked to fail when broken. Test helpers for kanshi's test
  server move to M1, where the first test needs them. The independent
  review of M0 is folded into M1's.
- 2026-10-17, M1 (SSH): host keys checked with x/crypto knownhosts; the
  host key algorithms offered are limited to the key types known_hosts
  has for the host (without this the test server offers ECDSA first and
  a correctly recorded ed25519 key looks "changed"; shown by e2e). The
  ssh_config parser was rewritten to OpenSSH rules (first value wins,
  Host patterns with negation, Include up to 16 deep, tokens, ${VAR},
  quotes and '=', one argument for single-value keywords); Match is
  skipped with a warning; ProxyJump/ProxyCommand are refused instead of
  connecting directly. "Alias not found" is gone: like `ssh name`, an
  unmatched name is used as the host name. --host alone reads no config
  (kept from the user's commit 15ed47b). Agent and key-file keys share one
  publickey method, agent first: x/crypto tries each method name once, so
  the agent was never tried after a key-file failure. Passphrase-protected
  keys are decrypted only if the server accepts them. Dial plus handshake
  time out after 30 s (2 min when a prompt is possible). Fixed with tests
  first: the crash list in section 2, port 0 (shown failing on the old
  parser), CPU NaN / reset / missing steal, a short df line, and `stat
  --no-prompt` asking for a sudo password without --with-root.
  Keepalive and reconnect move to M4, their first user. scripts/e2e.sh
  runs TestE2E* against kanshi's test server.
- 2026-10-17, M1 review (independent subagent), 12 findings, all fixed
  with tests (each checked to fail with its fix reverted, except where
  the reviewer's own reproduction stood in):
  host certificates (@cert-authority) now work: certificate algorithms
  are offered and a CA line no longer turns an unknown plain key into a
  false "changed" warning; HostKeyAlias is looked up without the port;
  a bad line or unreadable known_hosts file is skipped with a warning
  instead of refusing every host (x/crypto's knownhosts.go is vendored in
  src/knownhosts with these changes marked; no new module); a locked key
  that cannot be unlocked is not offered (one failed signature ends all
  public key auth in x/crypto), prompts are per key and repeat 3 times;
  key order follows OpenSSH (identity files first, through the agent
  when it holds them; IdentitiesOnly honoured); Match criteria all, host,
  originalhost, user, localuser are evaluated, and a Match that cannot be
  evaluated is refused if it would set a still-unset routing keyword
  (ProxyJump, HostName, Port, User, HostKeyAlias, known_hosts files) or
  Include; Include in user configs is relative to ~/.ssh; --config reads
  only that file (like ssh -F); command-line flags are seeded as
  first values so tokens such as %h see --host; connect timeout 30 s,
  handshake 30 s only without prompting (a prompt has no deadline); README
  lists the --user and default-key changes. Tests now include an
  in-process SSH server and ssh-agent (src/ssh/server_test.go).
