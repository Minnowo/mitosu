#!/usr/bin/env bash
# End-to-end tests over real SSH against kanshi's local test server
# (../kanshi/testserver, 127.0.0.1:2222, user dev): runs the TestE2E* tests
# with the server's address, key and host key, and fails if any of them
# was skipped. Start the server first with ../kanshi/testserver/run.sh.
# Usage: scripts/e2e.sh   (KANSHI_DIR overrides ../kanshi)

set -euo pipefail

cd "$(dirname "$0")/.."

if [ -x "$HOME/.local/go/bin/go" ]; then
    PATH="$HOME/.local/go/bin:$PATH"
fi

KANSHI_DIR="${KANSHI_DIR:-../kanshi}"
NAME="kanshi-testserver"

fail() {
    echo "e2e: FAIL: $*" >&2
    exit 1
}

docker inspect -f '{{.State.Running}}' "$NAME" 2>/dev/null | grep -qx true \
    || fail "$NAME is not running; start it with $KANSHI_DIR/testserver/run.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

host_key="$(docker exec "$NAME" cat /etc/ssh/ssh_host_ed25519_key.pub | awk '{print $1, $2}')"
[ -n "$host_key" ] || fail "could not read the test server's host key"
echo "[127.0.0.1]:2222 $host_key" >"$work/known_hosts"
rsa_key="$(docker exec "$NAME" cat /etc/ssh/ssh_host_rsa_key.pub | awk '{print $1, $2}')"
[ -n "$rsa_key" ] || fail "could not read the test server's RSA host key"
echo "[127.0.0.1]:2222 $rsa_key" >"$work/known_hosts_rsa"

export MITOSU_E2E_HOST=127.0.0.1
export MITOSU_E2E_PORT=2222
export MITOSU_E2E_USER=dev
export MITOSU_E2E_KEY
MITOSU_E2E_KEY="$(cd "$KANSHI_DIR/testserver/keys" && pwd)/dev"
export MITOSU_E2E_KNOWN_HOSTS="$work/known_hosts"
export MITOSU_E2E_KNOWN_HOSTS_RSA="$work/known_hosts_rsa"

log="$work/test.log"
go test -count=1 -run '^TestE2E' -v . 2>&1 | tee "$log" || fail "tests failed"
if grep -q '^--- SKIP' "$log"; then
    fail "an e2e test was skipped"
fi
passed="$(grep -c '^--- PASS' "$log" || true)"
[ "$passed" -gt 0 ] || fail "no e2e tests ran"
echo "e2e: ok ($passed passed)"
