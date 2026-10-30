#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")" && pwd)"
expected=sha256:a7120e76048242bacf22b36e3c916339f71ad82858e0ce69c53d2fe8611fec85
base=sidekicks-neko:3.2.0-source
test "$(docker image inspect -f '{{.Id}}' "$base")" = "$expected"
python3 - "$here" <<'PY'
import hashlib, pathlib, sys
r=pathlib.Path(sys.argv[1])
for name, digest in [('.cache/neko-v3.2.0.tar.gz','3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d'),('artifacts/loopback-manager.go','5baaaa80fc58e56cb37489f151ed75bef8628e995d8c285eefa9a32880b6a163')]:
    assert hashlib.sha256((r/name).read_bytes()).hexdigest()==digest, name
PY
docker image inspect "$base" > "$here/artifacts/base-image.json"
docker build --platform linux/arm64 --progress plain -t sidekicks-neko:microvm-loopback "$here" 2>&1 | tee "$here/artifacts/build.log"
test "$(docker image inspect -f '{{.Id}}' "$base")" = "$expected"
docker image inspect sidekicks-neko:microvm-loopback > "$here/artifacts/built-image.json"
python3 - "$here" <<'PY'
import json, pathlib, sys
r=pathlib.Path(sys.argv[1]); b=json.loads((r/'artifacts/base-image.json').read_text())[0]; n=json.loads((r/'artifacts/built-image.json').read_text())[0]
assert b['Id']=='sha256:a7120e76048242bacf22b36e3c916339f71ad82858e0ce69c53d2fe8611fec85'
assert n['Architecture']=='arm64' and n['Os']=='linux'
assert n['RootFS']['Layers'][:-1]==b['RootFS']['Layers'], 'base layers changed'
assert n['Config']==b['Config'], 'runtime image config changed'
print(json.dumps({'imageId':n['Id'],'architecture':n['Architecture'],'baseLayersUnchanged':True,'runtimeConfigUnchanged':True},indent=2))
PY
