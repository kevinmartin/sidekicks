#!/usr/bin/env bash
set -uo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
bash scripts/workbench.sh status
for id in novnc neko; do
 echo "$id adapter:"
 port=7791; test "$id" = neko && port=7792
 curl -fsS --max-time 3 "http://127.0.0.1:$port/health" || true
 echo
 if test -f "pocs/$id/status.sh"; then bash "pocs/$id/status.sh"; fi
done
