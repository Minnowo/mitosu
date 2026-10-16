#!/usr/bin/env bash
# Checks web/package.json and web/package-lock.json: the direct packages
# are exactly scripts/approved-npm.txt (name@version), and every locked
# package comes from registry.npmjs.org with an integrity hash.

set -euo pipefail

cd "$(dirname "$0")/.."

python3 -I - <<'PY'
import json, sys
approved = set()
for line in open("scripts/approved-npm.txt"):
    line = line.strip()
    if line and not line.startswith("#"):
        approved.add(line)
pkg = json.load(open("web/package.json"))
direct = set()
for section in ("dependencies", "devDependencies"):
    for name, version in pkg.get(section, {}).items():
        direct.add(f"{name}@{version}")
for extra in ("peerDependencies", "optionalDependencies", "bundleDependencies"):
    if pkg.get(extra):
        sys.exit(f"check-npm: web/package.json has {extra}")
if direct != approved:
    sys.exit(f"check-npm: web/package.json differs from scripts/approved-npm.txt: "
             f"not approved {sorted(direct - approved)}, missing {sorted(approved - direct)}")
lock = json.load(open("web/package-lock.json"))
for path, entry in lock.get("packages", {}).items():
    if path == "":
        continue
    resolved = entry.get("resolved", "")
    if not resolved.startswith("https://registry.npmjs.org/") or not entry.get("integrity", "").startswith("sha512-"):
        sys.exit(f"check-npm: {path} is not from registry.npmjs.org with an sha512 integrity hash")
PY
