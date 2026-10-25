# mitosu threat model (M7, 2026-10-17)

mitosu runs on the user's own machine. `mitosu stat` and `mitosu kanshi`
are one-shot CLI commands; `mitosu serve` pulls kanshi's history from
servers over SSH, keeps it in SQLite and serves a web UI. This document
lists what it trusts, what it does not, and what is left. Decisions
behind it are in docs/brief.md; the server side is
../kanshi/docs/threat-model.md.

## Assets

| Asset                           | Why it matters                                                |
|---------------------------------|---------------------------------------------------------------|
| The SSH login to each server    | usually an admin with sudo: whoever drives it owns the server |
| The user's SSH keys and agent   | the same, for every server they open                          |
| The history database            | names servers, containers, images, mounts; years of load data |
| Live view output (never stored) | logs, journal and sudo lines can hold secrets                 |
| The web UI password             | opens all of the above to whoever has it                      |
| The user's machine              | runs mitosu with the user's rights                            |

## Actors and trust

| Actor                          | Trust                                                   | Can                                                                         |
|--------------------------------|---------------------------------------------------------|-----------------------------------------------------------------------------|
| The user                       | full                                                    | writes the config and ~/.ssh/config, runs mitosu, logs in to the UI         |
| A monitored server             | authenticated by its host key, output parsed as hostile | answers commands: kanshi exports, docker, journalctl, apt, systemctl output |
| Containers on a server         | none                                                    | choose their names' log lines, labels, images, and what they print          |
| SSH clients of the server      | none                                                    | choose user names that appear in sshd log lines                             |
| The network between            | none                                                    | see, change or redirect traffic                                             |
| Browsers that reach the UI     | none until logged in                                    | send requests to `listen`; a logged-in browser reads everything             |
| Other web pages in the browser | none                                                    | make the browser send requests to mitosu (cross-site, DNS rebinding)        |
| Other local users              | none                                                    | read world-readable files, plant files in shared directories                |

## Entry points and mitigations

| Entry point                       | Threat                                               | Mitigation                                                                                                                                                                                                                                                                             |
|-----------------------------------|------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| The SSH connection                | a man in the middle or a swapped server              | host keys checked against known_hosts (x/crypto knownhosts, vendored with bad lines skipped); unknown or changed keys refused, never added; algorithms limited to the known key types; certificates through @cert-authority                                                            |
| ~/.ssh/config, the user's files   | settings mitosu would follow differently from ssh    | OpenSSH rules (first value wins, Host patterns, Include); a Match that cannot be evaluated is refused if it would change the route; ProxyJump/ProxyCommand refused rather than connecting directly; tokens limited; Include depth 16; fuzzed                                           |
| Key files and the agent           | prompting at the wrong time, offering keys not meant | identity files first, IdentitiesOnly honoured; locked keys offered only when they can be unlocked; serve never prompts                                                                                                                                                                 |
| kanshi export responses           | oversized, malformed or hostile data; shell noise    | strict reader: first line must be kanshi's, end line required, 1 MiB lines, 64 MiB of lines per page (asked again with fewer when over), --limit enforced, UTF-8 without control characters, lower-case unrepeated keys, no null, required fields, protocol caps; int64 bounds; fuzzed |
| Live view output                  | the same, plus secrets and terminal escapes          | output capped (4 MiB, 2000 lines, 20 s); parsers bounded and fuzzed; docker inspect decodes listed fields only (never Env, Cmd, Args, other labels); output never stored or logged; error text only from the failing command's own messages                                            |
| Values in remote commands         | shell or option injection                            | fixed templates; only container names (validated, single-quoted after `--`), unit names and journal cursors (validated, no leading `-`, single-quoted), docker timestamps (parsed and re-formatted) and numbers; kanshi requests carry numbers only                                    |
| Server-chosen strings shown       | XSS in the UI, escapes in the terminal               | the UI renders text nodes only (Preact, uPlot textContent); `pull` and `kanshi` print through Printable, `stat` replaces control characters and invalid UTF-8 in all server output before parsing; strict CSP (default-src 'self', no inline scripts)                                  |
| The web server                    | anyone who reaches `listen`                          | 127.0.0.1 by default; login on unless --no-login; argon2id password; sessions in memory, HttpOnly, SameSite=Strict, Secure under TLS; per-address login limit (not a global one), one check in flight each, 8 queued; opt-in TLS 1.2+; timeouts and 16 KiB bodies                      |
| Cross-site requests and rebinding | another page using the user's browser                | Host header must be an IP, localhost, the listen name or allowed_host; every /api/ call needs X-Mitosu: 1 (forces a CORS preflight, never answered); frame-ancestors 'none'                                                                                                            |
| The database and password files   | other local users reading or planting them           | new directory 0700, new files 0600 (WAL too; an existing database others can read is warned about); the password file must be a regular file owned by the user with no access for others; SQLite path percent-encoded; foreign SQLite files refused                                    |
| mitosu.conf                       | a bad edit or a missing mount                        | strict parser with line numbers, check-config, bounded durations; a `--config` file that does not exist is an error                                                                                                                                                                    |
| The web UI build                  | a malicious npm package                              | npm only inside a pinned Node image, install scripts off; scripts/web.sh builds without network and checks the approved package list and registry-only lock; only preact and uplot ship    |

## Residual risks (accepted)

- **The admin login is root.** mitosu holds a key (or agent) that can
  log in as an admin, and with live_sudo runs sudo -n. A bug in mitosu's
  command building would run as that admin. Mitigated by fixed templates
  and validation, not removed.
- **A logged-in browser sees everything,** including logs and sudo lines
  the live views fetch. Login off (`--no-login`) on a reachable address
  hands that to anyone who can reach it; mitosu allows it because the
  user asked for the choice.
- **Plain HTTP by default.** Off loopback without TLS, the password and
  session cookie cross the network in clear. TLS, a WireGuard tunnel or
  a reverse proxy is the user's call (docs/running.md).
- **The login limit is per address.** Behind a reverse proxy every
  client shares the proxy's; an attacker with many addresses (an IPv6
  /64) can guess at the rate of one argon2 check at a time per address
  and can keep the queue full, so the real user is told "busy". A long
  random password is the defence.
- **The session cookie is sent to every port on the same host name.**
  Cookies are not separated by port (RFC 6265 section 8.5): a service of
  another local user on 127.0.0.1:8001, or a local dev server that logs
  headers, receives the mitosu session if the browser visits it. Use TLS
  with its own host name, or a host name used for nothing else, when
  other users share the machine.
- **Live views need a POSIX login shell.** Their commands use `{ ...; }`
  and `$?`; under fish or tcsh they fail (safely).
- **The history is not encrypted at rest.** File modes keep other users
  out; anyone with the user's account or the disk reads it.
- **A compromised server can lie** in its kanshi data and command output.
  mitosu bounds and validates what it reads, but cannot tell a lying
  server from an honest one.
- **Shutdown can wait** up to 15 s for a pull's connection check.

## Out of scope

The servers themselves and kanshi (its own threat model), OpenSSH on the
servers, the user's account and machine, the browser.
