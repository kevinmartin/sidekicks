#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
label=io.sidekicks.overnight-awake
case "${1:-status}" in
 start)
  mkdir -p "$root/.run"
  if launchctl list "$label" >/dev/null 2>&1; then echo 'Sidekicks temporary awake job already exists; inspect status.'; exit 0; fi
  deadline=$(($(date +%s)+28800))
  launchctl submit -l "$label" -o "$root/.run/awake.log" -e "$root/.run/awake.log" -- /bin/bash "$root/scripts/awake-job.sh" "$deadline"
  echo 'Temporary idle-sleep assertion requested for 8 hours; no system setting or persistent plist changed.';;
 stop) launchctl remove "$label" 2>/dev/null || true; echo 'Temporary Sidekicks idle-sleep assertion removed.';;
 status) launchctl list "$label";;
 *) echo 'Usage: scripts/keep-awake.sh start|stop|status' >&2; exit 2;;
esac
