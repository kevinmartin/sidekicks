#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
test "$(docker image inspect -f '{{.Id}}' sidekicks-neko:polish-xfce-stage-v5)" = sha256:dae6e83f7ce48757ccd4d80182c5ae61c4e954e6233cb0199d5b89ce908521a4
OUT="$HERE/test/artifacts/xfce-stage/build-confirmation-$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$OUT"
python3 - "$ROOT" "$HERE" "$OUT" <<'PY'
from pathlib import Path
import hashlib,json,sys,xml.etree.ElementTree as E
workspace,root,out=map(Path,sys.argv[1:])
source=workspace/'shared/desktop/polish/xfce/templates/xfce4-session.xml'
target=root/'config/polish/xfce/templates/xfce4-session.xml'
def values(node,prefix=''):
 result={}
 for p in node:
  key=prefix+'/'+p.get('name','')
  result[key]={k:v for k,v in p.attrib.items() if k!='name'}
  result.update(values(p,key))
 return result
old=values(E.parse(target).getroot());new=values(E.parse(source).getroot())
assert new['/general/PromptOnLogout']['value']=='true'
expected={**old,'/general/PromptOnLogout':{**old['/general/PromptOnLogout'],'value':'true'}}
assert expected==new,'canonical session contains an unexpected product-property change'
target.write_bytes(source.read_bytes())
tree=E.parse(source);E.indent(tree)
tree.write(root/'config/xfce-session-confirmed.xml',encoding='utf-8',xml_declaration=True)
(out/'source.json').write_text(json.dumps({'productChange':{'/general/PromptOnLogout':{'before':old['/general/PromptOnLogout']['value'],'after':'true'}},'canonicalSha256':hashlib.sha256(source.read_bytes()).hexdigest(),'generatedSha256':hashlib.sha256((root/'config/xfce-session-confirmed.xml').read_bytes()).hexdigest(),'otherProductPropertiesIdentical':True,'hygiene':'Exclude/remove stale host __pycache__; no runtime module added.'},indent=2)+'\n')
PY
docker build --platform linux/arm64 --network none --progress plain -f "$HERE/Dockerfile.xfce-confirmation" -t sidekicks-neko:polish-xfce-stage-v6 "$HERE" > "$OUT/build.log" 2>&1
docker image inspect sidekicks-neko:polish-xfce-stage-v6 > "$OUT/image.json"
test "$(docker image inspect -f '{{.Id}}' sidekicks-neko:polish-xfce-stage-v5)" = sha256:dae6e83f7ce48757ccd4d80182c5ae61c4e954e6233cb0199d5b89ce908521a4
printf 'Built small v6 confirmation layer. Evidence: %s\n' "$OUT"
