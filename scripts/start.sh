#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
target="${1:-all}"
case "$target" in
 workbench) exec bash scripts/workbench.sh start;;
 novnc|neko) exec bash "pocs/$target/start.sh";;
 all)
  bash scripts/workbench.sh start
  for id in novnc neko; do
   if test -f "pocs/$id/start.sh"; then bash "pocs/$id/start.sh"; else echo "$id start.sh not ready" >&2; fi
  done;;
 *) echo 'Usage: scripts/start.sh [all|workbench|novnc|neko]' >&2; exit 2;;
esac
