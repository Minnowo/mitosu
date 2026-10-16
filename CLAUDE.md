# CLAUDE.md

Instructions for AI agents (and humans) working in this repository. The rules in
"Core rules" apply everywhere. "Project specifics" at the bottom is filled in
per project. Subdirectories may carry their own CLAUDE.md / AGENTS.md with rules
for that module; the closer file wins on conflicts.

## Core rules

### 1. ASCII only

- Write code, comments, docs, commit messages and logs in plain ASCII.
- No em/en dashes, smart quotes, ellipsis characters, arrows, bullets, emoji or
  box-drawing characters. Use `-`, `--`, `"`, `'`, `...`, `->`, `*`.
- Exceptions: string literals that must hold non-ASCII text, translation files,
  test fixtures for unicode handling, and data that is non-ASCII by nature.
  If there is no concrete reason for a non-ASCII character, do not use it.
- Before finishing, check your changes: `grep -nP '[^\x00-\x7F]' <files>`.

### 2. Keep it simple

Write every language as if it were Go: limited, explicit control flow that a
reader can follow top to bottom.

- Prefer a plain `for` loop, `if` and early `return` over chained iterators,
  comprehensions, clever combinators, ternary chains or metaprogramming when
  the plain version is clearer.
- One way of doing a thing. No abstraction until there is a second real user
  of it. No speculative options, flags or extension points.
- Handle errors explicitly at the point they occur. Do not swallow them, and do
  not use panicking shortcuts (`unwrap`, `!`, bare `except`, etc.) outside tests
  unless failure is provably impossible.
- Treat every number that comes from input (files, network, users, agents) as
  hostile: check bounds before indexing, guard divisions, cap allocations,
  bound recursion.
- No side effects inside asserts, debug-only code or log statements. They may
  be compiled out or skipped, and the side effect goes with them.

### 3. Minimal dependencies

- Never add a dependency without asking first. This includes dev and build
  dependencies.
- If a library would only give us one or two functions, vendor that code in
  instead (with its license header and a note of where it came from), or write it.
- Prefer the standard library. Ensure license compatability, if the project license
  does not work with the library license, do not use it.


### 4. Segmentation

- Keep features in their own modules so they can be added or removed by
  touching as few places as possible: plug in, plug out.
- One concept per file. For example, one API endpoint, one command, or one
  handler per file, named after what it does. Avoid catch-all files like
  `utils`, `helpers`, `misc`, `*_commands`.
- Shared types and transport code live in their own modules, not inside a
  feature module.
- Dependencies between modules point one way. Lower layers never import higher
  ones.
- Removing a feature should mean deleting its directory and its registration
  line, not hunting through the codebase.

### 5. Fix bugs test-first

For logic bugs:

1. Write a test that reproduces the bug and fails.
2. Run it and confirm it fails for the reason you expect.
3. Fix the bug.
4. Run it and confirm it passes, along with the rest of the suite.

Skip this only where a test does not make sense (UI styling, layout, copy
changes). New features come with at least one test that would fail without them.
Never weaken, skip or delete a test or a check to make something pass.

- Prefer tests at the external interface (CLI, API, files written, protocol
  messages). They survive refactors and even rewrites; tests of private
  internals break with them.
- Check that tests actually ran. A skipped, filtered or empty test run is not
  a pass.

### 6. Read the spec

When implementing or talking to a service, file format, protocol or API, find
the authoritative reference (spec, RFC, official docs, schema) and read it
before writing code. Download it into `docs/reference/` (or note the URL and
version in the module header) so the facts are on hand. Do not guess field
names, limits, encodings or error semantics from memory or from examples.
If no spec exists, say so and record what behaviour was observed.

### 7. Comments

Code should explain itself by being explicit and simple. Comments are for what
the code cannot say.

- Every file starts with a short header comment: what this file is for.
- Public functions and types get a doc comment (javadoc / godoc / rustdoc
  style): what it does, how to call it, what it returns, and any hidden
  semantics (ownership, blocking, thread safety, units, side effects, errors).
- Inside function bodies, comment only when something is done in a
  non-obvious way or for a non-obvious reason. Say why, not what.
- No comments that restate the code, narrate changes ("now uses X", "fixed
  bug"), or mark sections for decoration.
- If a workaround needs a paragraph of comment to justify why it is OK, the
  code is wrong. Fix the code.

## File layout

Organize each file to read top to bottom, important things first:

1. Header comment, then imports.
2. Constants.
3. The primary type or function the file exists for.
4. Supporting types below it.
5. Methods: constructors, then public methods, then private helpers.
   Callers above callees.

Tests: constants and fixtures first, then test cases, then test helpers last.

## Working style

- Read before you write. Look at the surrounding code and match its naming,
  structure and idiom.
- Do what was asked. Do not refactor, rename or reformat unrelated code in the
  same change.
- Keep the build green at every step. Commit small and often; each commit is a
  working state. Commits can be squashed at the end as needed.
- Report honestly. A stub, partial implementation or untested path is reported
  as such, never as done. Say whether a number was measured or estimated.
- No stubs or placeholders to get something compiling or passing: no empty
  function bodies, hard-coded stand-in values, `todo!()`-style panics or
  disabled checks. If something cannot be finished, stop and say so.
- Prefer rules enforced by tools (types, compiler, linter, tests, check
  scripts) over rules enforced by prose. When a rule here can be checked
  automatically, add the check.
- Fix the process, not just the instance. When the same kind of mistake
  happens twice, add a rule to this file (or the module's CLAUDE.md) or a check
  that catches it.
- For large jobs (many errors, many failing tests, many files), write the work
  list to a file, try the approach on a small sample first, then work through
  the list in batches.
- For UI changes, run the app and look at the result (screenshot) before
  calling it done.
- Use logging, not print statements, outside of tests and CLI output.
- Markdown tables are space-padded so columns line up in plain text.

## Independent review

The author of a change is biased toward accepting it. For any non-trivial
change, get a review from a separate context (a subagent) before calling it
done:

- The reviewer gets the diff and the task description, not the author's
  reasoning.
- The reviewer is told to assume the code is wrong and find out why: bugs,
  edge cases, behaviour changes, rule violations from this file.
- The reviewer does not edit code. The author fixes each finding or explains
  why it does not apply.

## Ask a human first

Do not decide these on your own:

- adding or upgrading a dependency;
- anything that costs money or uses paid services;
- pushing, publishing, releasing or deploying;
- destructive git operations (force push, reset, deleting branches);
- deleting files that are untracked or not yours;
- changes to public APIs, wire formats, or stored data formats;
- handling personal data or credentials.

Never put personal details (names, emails) into outgoing requests, headers or
user agents.

## Parallel agents

- One git worktree per agent. Edit only the files your task owns.
- Use separate build/output directories and ports per agent.
- If someone else's in-progress change breaks the build, wait and retry; do not
  fix their files.
- Never run git commands that touch other people's work: no `git stash`,
  `git reset`, `git checkout -- .`, `git clean`, `git add -A` or `git add .`.
  Stage and commit only the specific files you changed.
- Run heavy or slow commands (full builds, stress tests) with resource limits,
  and not from many agents at once.
- Clean up your worktree, build directory and temp files when done.

## Before you finish

- [ ] Build passes, formatter and linter are clean.
- [ ] Full test suite passes, and the tests actually ran. Bug fixes have a
      regression test.
- [ ] No stubs, placeholders or disabled checks left behind.
- [ ] Non-trivial changes had an independent review, findings addressed.
- [ ] New/changed text is ASCII only (see rule 1).
- [ ] No new dependencies without approval.
- [ ] Doc comments on new public functions; file header on new files.

## Project specifics

### Overview

mitosu reads servers over SSH. The quick CLI (`mitosu stat`) runs shell
commands on any Linux host and prints what it finds. Server mode
(`mitosu serve`, from milestone 4) pulls the metric history that kanshi
keeps on the server (`kanshi export`, contract in
`../kanshi/docs/protocol.md`, format 1.0), stores it in SQLite with
configurable downsampling, and serves a web UI. Logs, journal, logins,
sudo lines and other live views are read over the admin login when
opened and never stored. Full scope, decisions and milestones:
`docs/brief.md`.

### Layout

```
main.go            entry point: CLI definition, dispatches to src/cmd
src/<package>/     one directory per package; allowed imports between
                   them are listed in scripts/layers.txt
docs/brief.md      approved brief (source of truth)
docs/reference/    downloaded specs
scripts/check.sh   the one check; approved-deps.txt, layers.txt feed it
web/               the web UI (from milestone 5; npm runs in Docker only)
```

### Commands

```
check:  scripts/check.sh   (gofmt, go vet, tests with -race, static build, rules)
build:  CGO_ENABLED=0 go build -trimpath .
test:   go test ./...
e2e:    scripts/e2e.sh     (TestE2E* over real SSH against kanshi's test server)
```

Run scripts/e2e.sh before committing changes to src/ssh, src/cmd or
anything that talks to a server; it needs Docker, so check.sh does not
run it.

Commit only after reading check.sh's own exit status: never chain a
commit after `scripts/check.sh | tail` (the pipe reports tail's status;
this let a failing commit through once).

Go is at ~/.local/go/bin on the dev machine; check.sh adds it to PATH.
End-to-end tests (`e2e_test.go`, `TestE2E*`) use kanshi's test server
(`../kanshi/testserver/run.sh`, 127.0.0.1:2222); they skip without the
variables scripts/e2e.sh sets, and that script fails on any skip.

### Language notes

- Go (version in go.mod), CGO_ENABLED=0: the binary must stay static and
  cross-compile for every target in .goreleaser.yaml.
- Approved direct dependencies only (scripts/approved-deps.txt).
- Errors: return `error`, wrap with `fmt.Errorf("...: %w", err)`; no
  `panic` and no ignored errors outside tests.
- Logging: zerolog to stderr. stdout is for CLI output only.
- Everything from a server (command output, kanshi lines) and from the
  browser is hostile: bound-check before indexing, cap sizes, validate
  before it reaches a shell. Remote commands are fixed templates; the
  only values put into them are validated and single-quoted.
- Never write secrets (passwords, logs, journal lines, `docker inspect`
  environment) to the database or to logs.
