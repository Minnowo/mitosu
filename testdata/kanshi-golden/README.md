# Golden responses (protocol 1.0)

Exact stdout of kanshi requests against the fixtures in testdata/, for
readers such as mitosu to test their parser against. The kanshi test suite
(tests/golden.rs, tests/collect_export.rs) checks every file byte for
byte, so they always match the code.

Values that differ between runs are replaced before comparing:

| Field                                                                   | Shown as                          |
|-------------------------------------------------------------------------|-----------------------------------|
| `time_ms`                                                               | `0`                               |
| `store` (header)                                                        | `STORE`                           |
| `fs[].size_bytes`, `free_bytes`, `avail_bytes`, `inodes`, `inodes_free` | `0` (statvfs of the test machine) |
| store directory in an error message                                     | `/var/lib/kanshi`                 |

A real response has real values there; everything else, including field
order, is as kanshi writes it. Error `message` text is for humans and may
change in any version; do not parse it.

| File                       | Request                              | Shows                                                                                   |
|----------------------------|--------------------------------------|-----------------------------------------------------------------------------------------|
| `version.jsonl`            | `kanshi version`                     | the version line                                                                        |
| `export-host-basic.jsonl`  | `kanshi export --since 0`            | two samples with Docker on: every section, containers in several states, one `fs` error |
| `export-host-only.jsonl`   | `kanshi export --since 0`            | one sample with Docker off (no `containers`)                                            |
| `export-limit.jsonl`       | `kanshi export --since 0 --limit 1`  | `more: true`                                                                            |
| `export-nothing-new.jsonl` | `kanshi export --since 1`            | header and end only, `next` unchanged                                                   |
| `error-no-store.jsonl`     | `kanshi export --since 0` (no store) | `no_store` error, exit 1                                                                |
| `error-bad-request.jsonl`  | `kanshi export --since 01`           | `bad_request` error, exit 2                                                             |

Not covered by a file: gzip responses (`--gzip`, the same lines
compressed), `lost_through` above 0, and a response cut off without its
`end` line; docs/protocol.md specifies them.
