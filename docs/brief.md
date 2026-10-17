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
| Identity           | Host by its --alias, else host:port; container by name; cursor per host is (store ID, next seq)                                                                               | Container ID; Compose project + service                | IDs change on every `compose up`; Compose names survive it and keep replicas apart | user      |
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
| Config             | `$XDG_CONFIG_HOME/mitosu/mitosu.conf` (or `--config`), strict `key = value` lines like kanshi's; `host = <ssh alias>` and `tier` repeatable; pull_interval, db, listen, allowed_host, login, tls_cert, tls_key; `--db` and `--no-login` override the file; `mitosu check-config` | TOML/YAML (dependency)                                 | No parser dependency; same habits as kanshi                                | default   |
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
- 2026-10-17, M2 (kanshi reader): src/kanshi reads export and version
  responses, plain or gzip: the first line must be a kanshi line
  (anything printed earlier is refused, ErrNotKanshiOutput), lines are
  capped at 1 MiB, a response without its end line is ErrTruncated, the
  major version must be 1 (unknown fields and sections of newer minors
  are ignored), seq must rise above --since, end.next must equal the
  last seq, nothing may follow end, list lengths and strings are capped
  as in the protocol. Server text shown to users goes through Printable
  (no control characters). SSHClient.Exec streams a command's stdout to
  a reader and keeps its exit status and 16 KiB of stderr. New commands
  `mitosu kanshi version` and `mitosu kanshi export` (connection flags
  shared with stat through connectionFlags in main.go). kanshi's golden
  files are copied to testdata/kanshi-golden; check.sh compares the copy
  with ../kanshi when present. Native Go fuzz target FuzzReadExport
  (90 s, 1.66 M inputs, no failure). e2e: version, two export pages, and
  a login outside group kanshi refused, against real kanshi 1.0.0.
  For M3: an export with no samples but lost_through above --since keeps
  next at --since, so the same loss is reported again on the next pull;
  the store must record gaps idempotently.
- 2026-10-17, M2 review (independent subagent), 11 findings, all fixed:
  shell noise was reported as "kanshi did not answer (exit 101)" because
  Exec cuts kanshi off once the reader refuses the first line (now the
  noise hint wins whatever the status; e2e with a login whose shell
  prints a greeting, checked to fail with the old order); Exec has a
  deadline (version 30 s, export 10 min) and drops the connection if the
  server does not close a session within 5 s of being asked; the reader
  is told --limit and refuses more samples, and `more` unless exactly
  --limit samples came; export prints nothing unless the whole response
  passed (samples held in memory, capped at 256 MiB); every line must be
  valid UTF-8 without raw C0, DEL or C1 characters, with lower-case,
  unrepeated keys, no null and every required field (strict_json.go:
  encoding/json alone is case-insensitive, lets the last duplicate win
  and reads missing fields as zero); a sample needs at least one section;
  an error line must be the only line; nothing may follow the end line,
  nor the gzip stream; caps follow the protocol (container, project and
  service names 128 and their pattern, image 256 printable ASCII, message
  512, container id 64 hex, boot_id a UUID; cores 8192 is kanshi's own
  cap, not the protocol's); ExecResult carries Signal and TimedOut; gzip
  seeds for the fuzz target; tests for lost_through, limits, and each
  strictness rule. Found for kanshi: it exits 101 (a Rust panic) when
  its stdout is closed early; the protocol says 1. Fix pending in kanshi. Fuzzing after the fixes: two 60 s runs, about
  3.2 M inputs with plain and gzip seeds, no failure; run with
  -fuzzminimizetime 0, since minimizing large inputs otherwise stalls the
  run at 0 execs/s for minutes (seen; not a hang in the reader).
- 2026-10-17, M3 (store): modernc.org/sqlite v1.60.1 added (with
  modernc.org/libc, mathutil, memory, dustin/go-humanize, google/uuid,
  ncruces/go-strftime, remyoudompheng/bigfft; BSD-3 or MIT); it needs
  Go 1.26 in go.mod and raised indirect golang.org/x/sys to v0.48.0 and
  mattn/go-isatty to v0.0.24. Schema 1 (PRAGMA user_version; a newer one
  is refused): hosts (cursor: store ID, next seq, last sample line for
  rates across pulls), series (host, kind, entity, metric), points (raw
  tier, WITHOUT ROWID, whole numbers stored as integers), rollups (per
  resolution: n, sum, min, max per bucket, filled at ingest by upsert),
  samples (coverage), events (seq_gap, lost, store_reset, boot, errors,
  container; each recorded once). One page per transaction with the
  cursor: a page is stored whole or not at all. The cursor passes lost
  samples (max of next and lost_through), so a loss is reported once.
  Changed from section 3: containers are keyed by name, not Compose
  project and service (Compose names such as blog-web-1 survive
  recreation, and project+service would merge scaled replicas). Rates
  need the same boot_id, time going forward, at most 15 min between
  samples and no counter going down; containers also the same id and
  started_at. Container rates use their own counters, host rates the
  host's. Tiers are validated (only the first may be raw, resolutions
  nest, keeps do not shrink); `mitosu pull` uses the defaults until the
  config file (M4). Rollups of a resolution no longer configured are left
  in the database. Measured (docs/measurements.md): about 20 B per raw
  value; the brief's estimate (section 3a, 12 MB/day for OVH_4gb) was
  low by half: about 24 MB/day raw and 8 MB/day of 1-minute buckets, so
  the defaults settle near 3.6 GB. Found by a test while writing: a
  sample older than the one before wrapped the time difference and gave
  a rate (fixed).
- 2026-10-17, M3 review (independent subagent), 15 findings. Fixed, each
  with a test checked to fail with its fix undone: seq numbers explained
  by a reported loss (up to the cursor, or this page's lost_through) are
  no longer also recorded as a crash gap; a loss in the middle of a page
  gets its true range (after the last returned sample below it);
  container events compare with each container's last known state
  (kept in hosts.containers): an absent section says nothing, a list
  that may be partial (a containers problem, or 64 entries) marks nothing
  gone, and a store reset does not re-announce containers; reboots
  compare with the last known boot_id (hosts.boot_id); problem events
  fire when the set of failing sources changes (hosts.problems), not on
  every wording change; the series cache has a mutex (race found with
  -race and 4 hosts at once); retention deletes rollups and events per
  series and host so the keys are used (checked with EXPLAIN QUERY PLAN);
  the database path is made absolute and percent-encoded into the SQLite
  URI (a '%' in the name opened another, 0644 file), '?' and '#' are now
  allowed; transactions take the write lock at BEGIN and the schema is
  created once even by two first opens; an SQLite file with other tables
  is refused; seq, time_ms, next and lost_through must fit in an int64
  (checked by the kanshi reader); a sample not newer than the last stored
  values adds none, so raw points and rollups count the same samples; a
  series repeated in one sample keeps its first value; error and drop
  counters are each checked for a reset; `pull` names a host by --alias,
  else host:port; the request explanation moved to the kanshi package
  (kanshi.Explain, with pull.Outcome converting an SSH result); pull
  takes a Runner interface and has tests with a fake kanshi (paging,
  store recreated, samples deleted, failure); measurements.md corrected.
  Event times: server time, except a store reset and a loss reported
  without samples (this machine's clock), noted in the schema comment.
  Not done in M3: pull failures as gap rows (section 3 Gaps row) need
  server mode (M4). Waiting for the user: containers keyed by name
  instead of project+service, and the indirect module upgrades that
  modernc.org/sqlite requires.
- 2026-10-17, user decisions on the M3 review: containers stay keyed by
  name (Identity row updated); the indirect upgrades that
  modernc.org/sqlite v1.60.1 needs are kept (golang.org/x/sys v0.48.0,
  mattn/go-isatty v0.0.24, go 1.26 in go.mod; release builds need Go 1.26
  or newer).
- 2026-10-17, M4 (server mode), in three commits. (1) mitosu.conf with
  check-config: host, pull_interval (30s..24h, default 5m), db, tier
  (repeatable; any tier line replaces the defaults), listen,
  allowed_host, login, tls_cert/tls_key; durations are a number and one
  unit (s, m, h, d, w, y). (2) src/scheduler: one loop and one kept SSH
  connection per host, a keepalive check before each pull, retries after
  30 s doubling to the pull interval, pull_failed / pull_ok events on
  changes only, a pull-now trigger; serve stops cleanly on SIGINT or
  SIGTERM, cutting off a running pull. Hosts connect through ~/.ssh/config
  with prompting off. (3) src/web and src/auth: JSON API (/api/session,
  login, logout, hosts, series, points, events, pull), every request
  checked for its Host header (IP addresses, localhost, the listen name
  and allowed_host; others 421) and every /api/ request for X-Mitosu: 1;
  CSP and other security headers; 16 KiB bodies; login with one password
  (argon2id, 64 MiB, 3 passes, in a 0600 file next to the database, set
  by `mitosu passwd` or `--stdin`), sessions in memory (30 days, 64 at
  most, HttpOnly, SameSite=Strict, Secure under TLS), 5 failed logins in
  10 min lock logins for a minute, password checks one at a time; serve
  refuses to start with login on and no password; --no-login; opt-in TLS
  (TLS 1.2+). /api/points reads the finest tier that keeps the range and
  has at most 20 stored rows per returned point, merged into round steps
  (multiples of the tier resolution). Run notes in docs/running.md with
  an example config and systemd user unit in deploy/. No web UI yet
  (M5): / answers 404.
- 2026-10-17, M4 review (independent subagent), 19 findings, all fixed;
  each fix's test was checked to fail with the fix undone. Login limit:
  per client address (one address cannot lock out another; behind a
  reverse proxy all share the proxy's), one attempt in flight per
  address counted before the check (30 parallel wrong logins had all
  been checked), at most 8 checks queued. Every protected endpoint is
  tested without a session; the TLS test builds the server as serve
  does. allowed_host is lower-cased and loses a trailing dot. Events are
  limited in SQL (newest 10,000). net/http's error lines go to zerolog.
  serve opens its port (and loads the TLS certificate) before pulling, so
  a taken port fails at once; connecting honours stop; a second Ctrl-C
  ends the process. Exec's deadline covers opening the session. The
  failing state of pulls is read back from the events at start, so a
  restart still records pull_ok. A failed pull keeps a connection that
  still answers. Durations are bounded before multiplying (585y had
  wrapped to 21 days). A tier error names its own line. listen needs a
  port 1..65535 and a zone-free IP or a name. Points are counted from
  the range start (or the start of the first rollup bucket), never more
  than max. The password file must be a regular file of the user running
  mitosu with no access for others. Retry backoff is a tested function.
  Brief corrections: there are no --tls-cert/--tls-key flags (TLS is
  config only) and the key is db, not data_dir; added at the user's
  request: --db on serve, passwd and check-config (as on pull), which
  overrides the config's db, so `--config FILE --db FILE` keeps every
  file mitosu writes where the user says.
- 2026-10-17, M5 (web UI). npm packages, exact versions, each at least
  two weeks old (Preact 11.0.1 and Vite 8.3.4 were 8 days old and Preact
  11 a new major, so the newest Preact 10 and an older Vite): preact
  10.29.8, uplot 1.6.32 (both MIT, the only code in the page); build
  only: vite 8.3.2, @preact/preset-vite 2.10.6, tailwindcss and
  @tailwindcss/vite 4.3.3 (MIT), typescript 7.0.2 (Apache-2.0). The 102
  installed packages are MIT, ISC, BSD or Apache, plus MPL-2.0
  (lightningcss, used by Tailwind) and CC-BY-4.0 (caniuse-lite data),
  both build-time only. npm runs in node:26-trixie-slim pinned by digest
  (scripts/web.sh); the ponkotsu theme was generated once and committed
  (web/README.md). web/dist is not committed: check.sh builds it first
  and so needs Docker. The binary embeds it (web/embed.go) and serve
  answers / with it (index.html no-cache, hashed assets cached a year);
  one fallback route answers unknown /api/ paths with JSON 404 and other
  methods than GET/HEAD with 405 (registering "GET /" next to "/api/"
  crashed at start, found by running it; now tested with a UI present).
  New API: /api/latest, /api/containers; /api/hosts gives the pull
  interval and the problems of each host. Problems are derived in Go
  (src/problems, every threshold tested): disk free under 10 % (5 %
  critical), inodes over 90 % (95 %), memory available under 10 % (5 %),
  swap over 80 %, CPU busy over 90 % (95 %) averaged over 15 min, load5
  over twice the cores, containers dead or unhealthy (critical),
  restarting, exited non-zero or OOM-killed; no new sample for one pull
  interval plus 3 min; pulls failing; problems kanshi reports. Pages:
  login; overview (a card per host: status, CPU, memory, fullest
  filesystem, load, containers running, problems); host (range 1h to
  1y, charts for CPU, memory, load, each core, each filesystem, disk I/O
  and busy, network received and sent, container CPU and memory; gaps
  over 2.5 steps are drawn as gaps; container table; newest 200
  events). Follows the system's light or dark setting. Checked with
  headless Firefox screenshots (WebDriver BiDi, light, dark, 390 px
  wide, login) against kanshi's test server.
- 2026-10-17, M5 review (independent subagent), 15 findings, all fixed;
  each Go-side fix's test checked to fail with the fix undone, the UI
  ones checked in the browser. The host page re-requested in a loop for
  a host without samples (827 event requests in 13 s; now one per
  minute: requests depend on a clock that moves once a refresh). Charts
  with mixed steps lost their lines: aligning moved to Go (/api/chart,
  store.Chart: all series merged into the coarsest step, gaps of over
  2.5 steps marked, series without values left out, at most 64 series),
  so web/src/aligned.ts is gone and the logic is tested. Problem
  windows are measured from the newest sample, not from now; "no new
  samples" allows the pull interval plus 3 min or 3 of kanshi's own
  sample intervals (store.SampleInterval), whichever is longer; the CPU
  average is exact over its 15 minutes (store.Average); without a raw
  tier, latest values come from the finest rollup; kanshi's problems are
  read from the newest stored sample; exit codes 137 and 143 (docker
  stop) are not failures. Charts end now, so stale data shows as an empty
  stretch; the series list reloads each minute; every load error is
  shown; an older response can no longer replace a newer one; logout
  failures are shown; charts shrink with the window and redraw when the
  theme changes; /assets/ no longer lists the directory. Builds: make
  test/release build the UI first, goreleaser checks web/dist exists;
  web.sh installs by content hash with install scripts off and builds
  without the network; scripts/check-npm.sh enforces
  scripts/approved-npm.txt and registry-only locked packages. Not done
  by a test: the UI's request loop and ordering (no JS test runner is
  approved), checked by counting requests in headless Firefox.
- 2026-10-17, M6 (live views). src/live runs fixed commands over the
  host's kept connection (the scheduler now holds each host's connection,
  shared by pulls and live views, checked under one lock per host):
  docker logs --timestamps --tail N [--until] for a container the
  samples named; docker inspect, decoding only listed fields (Env, Cmd,
  Args and other labels than Compose's are never read; tested with a
  planted secret); journalctl -o json with --output-fields, unit and
  priority filters and --until for paging; SSH logins (sshd and
  sshd-session lines: accepted, failed, invalid user) and sudo / sudo-rs
  uses parsed from the journal; one script for OS, kernel, CPU, uptime,
  Docker version, apt list --upgradable (never apt update),
  /var/run/reboot-required and systemctl --failed. Only validated
  container and unit names (single-quoted) and numbers go into commands;
  output is capped at 4 MiB and 2000 lines; each command has 20 s (the
  connection may first have to be opened). Failures are explained:
  tool missing (also as sudo reports it, exit 1), Docker permission,
  sudo wanting a password, timeouts; error text printed on stdout is
  used too. journalctl prints no partial-access hint in JSON mode, so
  partial access is derived from the login's uid and groups (root,
  systemd-journal, adm or wheel see all; journalctl(1)). Config:
  live_views (default on), live_sudo (default off; sudo -n for docker and
  journalctl). API /api/live/{logs,inspect,journal,logins,sudo,system},
  behind the login. UI: a view row on the host page; container names
  link to their configuration and logs. e2e against the test server: the
  journal partial without sudo and complete with live_sudo, system facts,
  SSH logins, mitosu's own sudo use, and "docker is not installed" with
  and without sudo (the test server has no docker CLI, so container logs
  and inspect are tested with fixtures only).
- 2026-10-17, M6 review (independent subagent), 14 findings, all
  addressed; no injection found. docker logs applies --tail before
  --until, so older pages were always empty: the last lines before a
  line's exact timestamp are now taken by tail on the server, with
  docker's exit status on a marker line; lines without docker's
  timestamp (docker's or sudo's own messages, a stderr warning) are never
  shown as log lines and only explain failures, and container output
  never reaches an error message or mitosu's log. Failures of other
  commands are explained from stderr only, and only when they failed (a
  config-file warning on a successful run had read as "no Docker
  access"). Journal pages use journal cursors with -r (newest first, so
  a size cap drops the oldest) and --all (long fields are null
  otherwise); fields may be strings, byte arrays or arrays of strings;
  logins and sudo page by the last journal line read, not the last
  parsed one. SSH login lines take the address from the last
  " from <addr> port <n>", so a user name cannot fake it; sudo's
  "(command continued)" lines join their command; sudo-rs's refusal is
  recognised; a shell greeting before docker inspect is explained. Live
  commands stop when their request ends (ExecContext), run at most 2 at
  once per host, and skip the liveness check pulls make. The UI pages by
  the returned cursor, drops superseded answers, shows cut-off answers,
  and hides the view tabs when live_views is off (/api/session says so).
  A real-Docker test (TestE2ELocalDocker: a throwaway local container
  with 500 lines and a planted SECRET) pages through every line and
  checks inspect leaves the secret out; it fails with the old command.
  Accepted: a pull's liveness check (up to 15 s) can still delay
  shutdown under the per-host lock.
- 2026-10-17, M7 (hardening): docs/threat-model.md (assets, actors,
  entry points with their mitigations, accepted risks),
  docs/release-notes.md, deploy/Dockerfile (three stages, base images
  pinned by digest, user mitosu uid 1000, /data volume; built and pulled
  the test server, database 0600) with a Docker section in
  docs/running.md. Fuzz targets for ssh_config lines, Resolve and host
  patterns, known_hosts files, mitosu.conf, and the live-view parsers;
  scripts/fuzz.sh runs every target (results in docs/measurements.md).
  Fuzzing found that x/crypto's known_hosts pattern matcher takes
  exponential time in the number of '*'s (a 10 KB line hung it): the
  vendored copy now backtracks to the last '*' only, and a trailing '*'
  matches an empty run as in OpenSSH. kanshi 1.0.1 (exit 1, not a
  panic, when its reader goes away) is the version e2e and the golden
  files expect.
- 2026-10-17, M7 final review (independent subagent, whole system), 14
  findings. Fixed with tests first: a live view cancelled while its
  session was opening closed the host's shared connection (pulls and
  other views with it), and a connection that died under a live view
  was only replaced at the next pull; now the session is closed when it
  opens, SSHClient.Closed reports an ended connection without a round
  trip and the scheduler replaces it on the next use, and a live command
  that hit its deadline has the connection checked in the background
  (never under the per-host lock; a cut-off read is not a reason).
  A pull holds at most 64 MiB of sample lines per page and asks again
  for as many samples as fitted when a page is over (before: up to
  2000 x 1 MiB per host). `mitosu stat` replaces control characters
  and invalid UTF-8 in server output (a hostname could carry terminal
  escapes; \r, \v and \f become spaces). Chart series names split at
  the first and last '|' (a mount point may hold one). Docker logs over
  the output cap keep their end (the newest lines and docker's exit
  line; the oldest are reached with "older"), as the journal does; they
  used to fail with "ended without its exit status". A
  `--config` that does not exist is an error (a missed Docker mount
  silently gave the defaults). Without tests: the UI falls back to the
  overview on a malformed hash (checked in a browser); an existing
  database others can read is logged as a warning; scripts/fuzz.sh uses
  mktemp and fails with no targets. Documented as accepted risks: the
  session cookie reaches every port on the same host name (RFC 6265
  cookies ignore ports), the login limit is per address with no global
  bound, live views need a POSIX login shell. A second review of these
  fixes (subagent) found the first versions kept the oldest log lines
  and could hide a docker failure, checked liveness under the lock after
  any cut-off read, and halved --limit step by step; reworked as above.
  Threat model and running notes corrected where they claimed more than the code does (quoting,
  file modes, the Dockerfile's npm build, files read).
- Waiting for the user: a week of `mitosu serve` against OVH_4gb with
  kanshi 1.0.1 deployed, then the UI feedback pass.
