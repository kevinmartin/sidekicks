#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
failed=0
for id in novnc neko; do
 if test -f "pocs/$id/stop.sh"; then bash "pocs/$id/stop.sh" || failed=1; fi
done
bash scripts/workbench.sh stop || failed=1
bash scripts/keep-awake.sh stop || failed=1
exit "$failed"
