#!/usr/bin/env bash
# Runs every native Go fuzz target (func Fuzz* in *_test.go) for SECONDS
# each, one after another at low priority. Minimizing is off: on large
# inputs it stalls a run at 0 execs/s for minutes (docs/brief.md, M2).
# A failure leaves the input under the package's testdata/fuzz/ and makes
# this script fail; rerun it with: go test -run '<Target>/<file>' <package>
#   scripts/fuzz.sh [SECONDS]   default 60

set -euo pipefail

cd "$(dirname "$0")/.."

if [ -x "$HOME/.local/go/bin/go" ]; then
    PATH="$HOME/.local/go/bin:$PATH"
fi

SECONDS_EACH="${1:-60}"
case "$SECONDS_EACH" in
    '' | *[!0-9]*) echo "fuzz: SECONDS must be a whole number" >&2; exit 2 ;;
esac

# The Go build embeds the web UI.
scripts/web.sh >/dev/null

log="$(mktemp)"
trap 'rm -f "$log"' EXIT
targets="$(grep -rhoE --include='*_test.go' -H '^func (Fuzz[A-Za-z0-9_]+)' src | sed -E 's/:func / /' || true)"
if [ -z "$targets" ]; then
    echo "fuzz: no fuzz targets found" >&2
    exit 1
fi

failed=""
ran=0
while read -r file target; do
    pkg="./$(dirname "$file")"
    echo "fuzz: $pkg $target (${SECONDS_EACH}s)"
    if ! nice -n 19 go test -run '^$' -fuzz "^${target}\$" -fuzztime "${SECONDS_EACH}s" -fuzzminimizetime 0 "$pkg" >"$log" 2>&1; then
        tail -20 "$log" >&2
        failed="$failed $pkg:$target"
    else
        grep -E 'elapsed.*execs' "$log" | tail -1
    fi
    ran=$((ran + 1))
done <<<"$targets"

if [ -n "$failed" ]; then
    echo "fuzz: FAILED:$failed" >&2
    exit 1
fi
echo "fuzz: all $ran targets ran ${SECONDS_EACH}s without a failure"
