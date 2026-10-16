#!/usr/bin/env bash
# The single project check: format, vet, tests, static build and the
# repository rules from CLAUDE.md that tools can enforce. Must pass before
# every commit. Run from anywhere: scripts/check.sh

set -euo pipefail

cd "$(dirname "$0")/.."

if [ -x "$HOME/.local/go/bin/go" ]; then
    PATH="$HOME/.local/go/bin:$PATH"
fi

fail() {
    echo "check: FAIL: $*" >&2
    exit 1
}

step() {
    echo "check: $*"
}

step "format"
unformatted="$(gofmt -l -s .)"
[ -z "$unformatted" ] || fail "run 'gofmt -w -s .' on: $unformatted"

step "vet"
go vet ./...

step "tests"
test_log="$(mktemp)"
trap 'rm -f "$test_log"' EXIT
go test -race -count=1 -v ./... 2>&1 | tee "$test_log" >/dev/null || {
    grep -E '^(--- FAIL|FAIL|panic)' "$test_log" >&2 || true
    fail "tests failed (full output: go test -v ./...)"
}
passed="$(grep -c '^--- PASS' "$test_log" || true)"
[ "$passed" -gt 0 ] || fail "no tests ran"
step "tests: $passed passed, $(grep -c '^--- SKIP' "$test_log" || true) skipped"

step "static build"
out="$(mktemp -d)"
trap 'rm -f "$test_log"; rm -rf "$out"' EXIT
CGO_ENABLED=0 go build -trimpath -o "$out/mitosu" .
for target in linux/386 linux/arm linux/arm64 windows/amd64; do
    CGO_ENABLED=0 GOOS="${target%/*}" GOARCH="${target#*/}" go build -trimpath -o "$out/cross" . \
        || fail "cross build for $target"
done

step "ascii only"
bad=""
for f in $(git ls-files); do
    if grep -qIP '[^\x00-\x7F]' "$f" 2>/dev/null; then
        bad="$bad $f"
    fi
done
[ -z "$bad" ] || fail "non-ASCII characters in:$bad"

step "dependencies"
approved="$(grep -v '^#' scripts/approved-deps.txt | grep -v '^$')"
for dep in $(go list -m -f '{{if not .Indirect}}{{if not .Main}}{{.Path}}{{end}}{{end}}' all); do
    printf '%s\n' "$approved" | grep -qxF "$dep" \
        || fail "dependency '$dep' is not in scripts/approved-deps.txt"
done

step "layering"
module="$(go list -m)"
while read -r pkg imports; do
    pkg="${pkg%:}"
    rel="${pkg#"$module"}"
    rel="${rel#/}"
    [ -n "$rel" ] || rel="."
    line="$(grep -E "^$(printf '%s' "$rel" | sed 's/[.]/[.]/g'):" scripts/layers.txt || true)"
    [ -n "$line" ] || fail "package '$rel' is not listed in scripts/layers.txt"
    allowed="${line#*:}"
    for imp in $imports; do
        case "$imp" in
            "$module"/*) ;;
            *) continue ;;
        esac
        used="${imp#"$module"/}"
        case " $allowed " in
            *" $used "*) ;;
            *) fail "package '$rel' imports '$used'; allowed: [$allowed ]" ;;
        esac
    done
done < <(go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./...)

step "ok"
