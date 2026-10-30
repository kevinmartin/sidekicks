#!/bin/bash
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
exec python3 "$HERE/start.py" "$@"
