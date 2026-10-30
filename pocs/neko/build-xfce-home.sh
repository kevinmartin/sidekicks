#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
BASE=sidekicks-neko:polish-xfce-stage-v6
EXPECTED=sha256:821d14fbc88b1ffb0700a1c332de0eeffdeb0c9b7553b087801328be895316e4
IMAGE=sidekicks-neko:polish-xfce-stage-v7
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
if docker image inspect "$IMAGE" >/dev/null 2>&1; then printf 'v7 already exists; preserving its image rather than overwriting.\n' >&2; exit 1; fi
OUT="$HERE/test/artifacts/xfce-stage/build-home-$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$OUT"
python3 - "$HERE" "$OUT" <<'PY'
from pathlib import Path
import copy,hashlib,json,sys
root,out=map(Path,sys.argv[1:]);p=root/'config/firefox-policies.json'
old=json.loads(p.read_text());new=copy.deepcopy(old)
assert old['policies']['Homepage']['StartPage']=='home'
new['policies']['Homepage']['StartPage']='homepage'
(out/'policy-before.json').write_bytes(p.read_bytes())
p.write_text(json.dumps(new,indent=2)+'\n')
(out/'policy-after.json').write_bytes(p.read_bytes())
(out/'source.json').write_text(json.dumps({'productChange':{'policies.Homepage.StartPage':{'before':'home','after':'homepage'}},'otherPropertiesUnchanged':True,'policySHA256':hashlib.sha256(p.read_bytes()).hexdigest(),'installedPaths':['/etc/sidekicks/firefox-policies.json','/usr/lib/firefox-esr/distribution/policies.json'],'officialSchema':'https://firefox-admin-docs.mozilla.org/reference/policies/homepage/'},indent=2)+'\n')
PY
docker build --platform linux/arm64 --network none --progress plain -f "$HERE/Dockerfile.xfce-home" -t "$IMAGE" "$HERE" > "$OUT/build.log" 2>&1
docker image inspect "$IMAGE" > "$OUT/image.json"
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED"
printf 'Built minimal v7 Home policy layer. Evidence: %s\n' "$OUT"
