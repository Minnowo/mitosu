#!/usr/bin/env bash
# Builds the web UI (src/ui/ -> src/ui/dist) with npm inside a pinned Debian
# Node image, as the user's projects do: nothing is installed on the host.
# Inside a container (/.dockerenv) it runs npm directly.
#   scripts/web.sh          npm ci when needed, then type check and build
#   scripts/web.sh npm ...  run any npm command in src/ui/

set -euo pipefail

cd "$(dirname "$0")/../src/ui"

# node:26-trixie-slim, pinned 2026-10-17.
NODE_IMAGE="node@sha256:0d1e33586362bb9e608c4e751e7a9e7edb55c9ac11461ed5666af696cc461dd4"

# npm_run NETWORK ARGS...: npm ARGS in src/ui/, with the network (host) or
# without it (none).
npm_run() {
    local network="$1"
    shift
    if [ -f /.dockerenv ]; then
        npm "$@"
    else
        docker run --rm --network "$network" --user "$(id -u):$(id -g)" -e HOME=/tmp \
            -e npm_config_update_notifier=false -v "$PWD:/ui" -w /ui "$NODE_IMAGE" npm "$@"
    fi
}

if [ "${1:-}" = "npm" ]; then
    shift
    npm_run host "$@"
    exit
fi

# Install (with the network) when package.json or the lock file differ
# from what node_modules was installed from; build without the network.
stamp=node_modules/.mitosu-installed
want="$(cat package.json package-lock.json | sha256sum | cut -d' ' -f1)"
if [ ! -f "$stamp" ] || [ "$(cat "$stamp")" != "$want" ]; then
    npm_run host ci --no-audit --no-fund --ignore-scripts
    echo "$want" >"$stamp"
fi
npm_run none run build
