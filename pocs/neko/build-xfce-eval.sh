#!/usr/bin/env bash
# Source/build-only evaluation; no container, desktop, viewer or host UI calls.
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
BASE=sidekicks-neko:3.2.0-ux1
EXPECTED=sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53
CORE=sidekicks-neko:polish-xfce-core-deps
SESSION=sidekicks-neko:polish-xfce-session-deps
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
OUT="$HERE/test/artifacts/xfce-eval/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$OUT/packages"
docker image inspect "$BASE" > "$OUT/base-image.json"
cp "$HERE/Dockerfile.xfce-eval" "$HERE/build-xfce-eval.sh" "$OUT/"
docker build --platform linux/arm64 --target xfce-core --progress plain -f "$HERE/Dockerfile.xfce-eval" -t "$CORE" "$HERE" > "$OUT/core-build.log" 2>&1
docker image inspect "$CORE" > "$OUT/core-image.json"
docker build --platform linux/arm64 --target xfce-session --progress plain -f "$HERE/Dockerfile.xfce-eval" -t "$SESSION" "$HERE" > "$OUT/session-build.log" 2>&1
docker image inspect "$SESSION" > "$OUT/session-image.json"
docker build --platform linux/arm64 --target evidence --progress plain --output "type=local,dest=$OUT/packages" -f "$HERE/Dockerfile.xfce-eval" "$HERE" > "$OUT/export-build.log" 2>&1
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
python3 "$HERE/measure-xfce-eval.py" "$OUT"
printf 'Dependency evaluation only; no Xfce session started. Evidence: %s\n' "$OUT"
