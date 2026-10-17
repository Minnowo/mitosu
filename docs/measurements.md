# mitosu measurements

Measured numbers, with the setup they come from. Estimates are marked.

## M3 store size and pull speed (2026-10-17, dev machine)

Source: kanshi's test server (127.0.0.1:2222), kanshi 1.0.0 sampling
every 2 s with Docker on, as its samples report it: 8 cores, 1
filesystem, 1 disk, 1 interface, 1 running container (of 3). 6,885
samples between the first and last 13.9 h apart (the test server's
collector did not run the whole time: about 3.8 h of sampling).

`mitosu pull` into an empty database: 4.0 s for 6,885 samples in 4
requests of 2,000; a second pull 22 s later took 0.1 s. By hand, not in a
test: a pull stopped with SIGINT after 1.5 s had stored one page (2,000
samples, cursor 2,000); the next pull stored the other 4,909, and the
database then held 6,909 samples with 6,909 distinct sequence numbers.

| Table   | Rows    | Size     | Per sample |
|---------|---------|----------|------------|
| points  | 337,263 | 6.4 MiB  | 978 B      |
| rollups | 11,613  | 440 KiB  | 65 B       |
| samples | 6,885   | 120 KiB  | 18 B       |

49 values per sample (17 host, 8 cores, 5 for the filesystem, 5 for the
disk, 4 for the interface, 10 for the container), about 20 bytes per raw
value and 40 bytes per rollup bucket (n, sum, min, max).

Estimate (not measured) for OVH_4gb (4 cores, 2 fs, 1 disk, 8 interfaces, 7 running
containers, 10 s): about 138 values per sample, so per day about 24 MB
raw, 8 MB of 1-minute buckets and 0.13 MB of 1-hour buckets.

| Tiers                                           | Steady-state size (estimate) |
|-------------------------------------------------|------------------------------|
| defaults: raw 30 d, 1 min 1 y, 1 h forever      | 0.7 + 2.9 GB, +0.05 GB/year  |
| raw 7 d, 1 min 90 d, 1 h forever                | 0.17 + 0.7 GB, +0.05 GB/year |
| raw 180 d, 1 min 2 y, 1 h forever               | 4.3 + 5.8 GB, +0.05 GB/year  |

Measured on OVH_4gb (2026-10-17, by the user on their other machine):
`mitosu pull --alias OVH_4gb` stored 6,128 samples (all kanshi held since
the 1.0 store wipe, about 17 h at 10 s) in 4 requests, 19.3 s; the
database file is 19 MB: about 3.1 KB per sample in all (raw points,
rollups, samples, events), so about 27 MB per day while every tier is
still filling. The estimate above (24 MB raw + 8 MB of 1-minute buckets,
32 MB per day) was slightly high; the steady-state sizes above are
therefore upper bounds.

## M7 fuzzing (2026-10-17, dev machine)

scripts/fuzz.sh, 5 minutes per target at nice 19 with
`-fuzzminimizetime 0` (minimizing large inputs otherwise stalls a run).

| Target                          | Executions | Result                         |
|---------------------------------|------------|--------------------------------|
| kanshi FuzzReadExport           | 17.1 M     | no failure                     |
| ssh FuzzSplitConfigLine         | 19.3 M     | no failure                     |
| ssh FuzzResolve                 | 7.2 M      | no failure                     |
| ssh FuzzWildcardMatch           | 20.7 M     | no failure                     |
| knownhosts FuzzOpen             | 10.7 M     | hang found, fixed, then clean  |
| config FuzzParse                | 8.7 M      | no failure                     |
| live FuzzJournal                | 18.4 M     | no failure                     |
| live FuzzLogs                   | 19.9 M     | no failure                     |
| live FuzzLineParsers            | 19.4 M     | no failure                     |
| live FuzzSystemAndInspect       | 19.6 M     | no failure                     |

The FuzzOpen hang: x/crypto's known_hosts pattern matcher recursed once
per '*', so a 10 KB host pattern with about a thousand '*'s ran for
minutes (exponential). The vendored copy now backtracks to the last '*'
only (O(pattern x name)); the input is kept in
src/knownhosts/testdata/fuzz/FuzzOpen/ and runs with every `go test`.
The count above is the run after the fix.
