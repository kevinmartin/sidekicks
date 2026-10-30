#!/usr/bin/env bash
# Ephemeral launch job body. A fixed deadline prevents relaunch extending the assertion.
set -u
label=io.sidekicks.overnight-awake
deadline="${1:-0}"
now=$(date +%s)
remaining=$((deadline-now))
if test "$remaining" -gt 0; then /usr/bin/caffeinate -i -t "$remaining"; fi
/bin/launchctl remove "$label" 2>/dev/null || true
