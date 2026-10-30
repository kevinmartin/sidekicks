#!/usr/bin/env bash
# Controlled one-time replacement of only the three POC-owned VM desktops.
# Run after prepare.sh and final Docker checks, outside every QA input window.
set -euo pipefail
export LC_ALL=C LANG=C
test "${1:-}" = --apply || { echo 'Usage: bash pocs/microvm/ux/apply.sh --apply' >&2; exit 2; }
here="$(cd "$(dirname "$0")/.." && pwd)"
build="$here/.build"
ux="$build/ux1"
backup="$build/pre-cloud-ux"
cli="$here/microvm.py"
test ! -e "$backup" || { echo 'Existing pre-cloud-ux backup: inspect before retry.' >&2; exit 1; }
for item in "$ux/novnc/base.ext4" "$ux/neko/base.ext4" "$ux/base-nat.ext4" "$ux/runner"; do test -s "$item"; done
for session in a neko nat; do
 test -f "$here/.state/$session/session.json"
 test ! -e "$here/.state/$session-pre-ux"
done
mkdir "$backup"
mkdir -p "$here/artifacts/ux"
# Preserve template and exact runner bytes while the old guests still run.
for item in base.ext4 base-neko.ext4 base-nat.ext4 runner runner-neko runner-nat; do
 cp -c "$build/$item" "$backup/$item"
done
for session in a neko nat; do
 python3 "$cli" status "$session" > "$here/artifacts/ux/$session-before.json"
 python3 "$cli" stop "$session" > "$here/artifacts/ux/$session-stop.json"
 # Old disks remain available, flushed and stopped; no reset/destroy.
 mv "$here/.state/$session" "$here/.state/$session-pre-ux"
done
cp -c "$ux/novnc/base.ext4" "$build/base.ext4.next"
cp -c "$ux/neko/base.ext4" "$build/base-neko.ext4.next"
cp -c "$ux/base-nat.ext4" "$build/base-nat.ext4.next"
for item in base.ext4 base-neko.ext4 base-nat.ext4; do mv "$build/$item.next" "$build/$item"; done
for item in runner runner-neko runner-nat; do cp -c "$ux/runner" "$build/$item.next"; mv "$build/$item.next" "$build/$item"; done
python3 "$cli" create a 7903 7793 novnc > "$here/artifacts/ux/a-create.json"
python3 "$cli" start a > "$here/artifacts/ux/a-start.json"
python3 "$cli" create neko 7906 7796 neko > "$here/artifacts/ux/neko-create.json"
python3 "$cli" start neko > "$here/artifacts/ux/neko-start.json"
python3 "$cli" create nat 7907 7797 neko-nat > "$here/artifacts/ux/nat-create.json"
python3 "$cli" start nat > "$here/artifacts/ux/nat-start.json"
printf '%s\n' 'UX guests ready; prior guests are stopped under .state/*-pre-ux. Independent UX QA is still required.'
