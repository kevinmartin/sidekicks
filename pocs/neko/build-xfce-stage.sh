#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
IMAGE="${SIDEKICKS_NEKO_XFCE_IMAGE:-sidekicks-neko:polish-xfce-stage-v1}"
RUNTIME_BASE="${SIDEKICKS_NEKO_XFCE_BASE:-sidekicks-neko:polish-xfce-session-deps}"
case "$IMAGE" in sidekicks-neko:polish-xfce-stage-*) ;; *) exit 1 ;; esac
test "$(docker image inspect -f '{{.Id}}' sidekicks-neko:3.2.0-ux1)" = sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53
test "$(docker image inspect -f '{{.Id}}' sidekicks-neko:polish-xfce-session-deps)" = sha256:c4cfcdf56e95654b64a1841ff43797a1b6a21202f27a5740c55edee9409071c1
OUT="$HERE/test/artifacts/xfce-stage/build-$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$OUT" "$HERE/config/polish"
cp "$ROOT/shared/desktop/polish/settings.ini" "$ROOT/shared/desktop/polish/gtk.css" "$HERE/config/polish/"
cp "$ROOT/shared/desktop/polish/Xresources" "$HERE/config/Xresources"
cp -R "$ROOT/shared/desktop/polish/icons" "$HERE/config/polish/"
if [ -f "$HERE/config/polish/xfce/panel-context-guard.py" ]; then cp "$HERE/config/polish/xfce/panel-context-guard.py" "$OUT/failed-panel-context-guard.py"; fi
rm -rf "$HERE/config/polish/xfce"
cp -R "$ROOT/shared/desktop/polish/xfce" "$HERE/config/polish/"
cp "$ROOT/shared/desktop/home.html" "$HERE/public/home.html"
python3 - "$HERE" "$OUT" <<'PY'
import hashlib,json,pathlib,sys
root,out=map(pathlib.Path,sys.argv[1:])
files=[root/'Dockerfile.xfce-runtime',root/'server.mjs',root/'desktop-apps.mjs',root/'public/home.html',*sorted((root/'config').rglob('*'))]
hashes={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files if p.is_file()}
(out/'source.json').write_text(json.dumps({'files':hashes,'sharedXfceOwner':'noVNC worker; copied read-only'},indent=2)+'\n')
PY
docker image inspect "$RUNTIME_BASE" > "$OUT/runtime-base.json"
docker build --platform linux/arm64 --progress plain --build-arg "XFCE_RUNTIME_BASE=$RUNTIME_BASE" -f "$HERE/Dockerfile.xfce-runtime" -t "$IMAGE" "$HERE" > "$OUT/build.log" 2>&1
docker image inspect "$IMAGE" > "$OUT/image.json"
test "$(docker image inspect -f '{{.Id}}' sidekicks-neko:3.2.0-ux1)" = sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53
printf 'Built separate Xfce stage image: %s\nEvidence: %s\n' "$IMAGE" "$OUT"
