#!/usr/bin/env bash
# Build-only: never starts, stops, attaches to or replaces a desktop/container.
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BASE=sidekicks-neko:3.2.0-ux1
EXPECTED=sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53
IMAGE="${SIDEKICKS_NEKO_POLISH_IMAGE:-sidekicks-neko:polish-thunar-eval-v2}"
case "$IMAGE" in
  sidekicks-neko:polish-*) ;;
  *) printf 'Use a separate sidekicks-neko:polish-* evaluation tag.\n' >&2; exit 1 ;;
esac
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
mkdir -p "$HERE/config/polish"
for asset in settings.ini gtk.css; do
  test -s "$ROOT/shared/desktop/polish/$asset"
  cp "$ROOT/shared/desktop/polish/$asset" "$HERE/config/polish/$asset"
done
cp "$ROOT/shared/desktop/polish/Xresources" "$HERE/config/Xresources"
cp -R "$ROOT/shared/desktop/polish/icons" "$HERE/config/polish/"
OUT="$HERE/test/artifacts/polish/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$OUT/packages"
docker image inspect "$BASE" > "$OUT/base-image.json"
python3 - "$HERE" "$OUT/source.json" <<'PY'
import hashlib, json, pathlib, sys
root=pathlib.Path(sys.argv[1])
files=[root/'Dockerfile.polish',root/'build-polish.sh',*sorted((root/'config').rglob('*'))]
hashes={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files if p.is_file()}
pathlib.Path(sys.argv[2]).write_text(json.dumps({'sourceOnly':True,'theme':'shared original Sidekicks Adwaita overlay','files':hashes},indent=2)+'\n')
PY
docker build --platform linux/arm64 --target polish --progress plain -f "$HERE/Dockerfile.polish" -t "$IMAGE" "$HERE" 2>&1 | tee "$OUT/build.log"
docker image inspect "$IMAGE" > "$OUT/candidate-image.json"
docker build --platform linux/arm64 --target evidence --output "type=local,dest=$OUT/packages" --progress plain -f "$HERE/Dockerfile.polish" "$HERE" > "$OUT/evidence-export.log" 2>&1
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
python3 "$HERE/measure-polish.py" "$OUT"
printf 'Build-only candidate complete: %s\nEvidence: %s\n' "$IMAGE" "$OUT"
