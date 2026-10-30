#!/usr/bin/env bash
# Current UX rebuild follows the verified Xfce chain, never the old Openbox recipe.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
exec bash "$HERE/build-current.sh" "$@"
