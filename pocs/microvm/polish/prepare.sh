#!/usr/bin/env bash
# Separate candidate templates only. Current VM state/templates/runners stay unchanged.
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")/.." && pwd)"
novnc_image="${1:?exact frozen noVNC Xfce image ID required}"
neko_image="${2:?exact frozen Neko Xfce image ID required}"
for image in "$novnc_image" "$neko_image"; do
 [[ "$image" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Exact image IDs required.' >&2; exit 1; }
 test "$(docker image inspect -f '{{.Id}}' "$image")" = "$image"
done
loopback_image=sha256:c27c3aab3181aa24dad92efb70a707d3c24f019d63c60279e42e1654c5346146
test "$(docker image inspect -f '{{.Id}}' "$loopback_image")" = "$loopback_image"
# Keep filesystem build tools out of the clean runtime. This preserved image
# already supplied mkfs.ext4/e2fsck for the accepted template preparation.
formatter_image=sha256:0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7
test "$(docker image inspect -f '{{.Id}}' "$formatter_image")" = "$formatter_image"
revision="${SIDEKICKS_POLISH_REVISION:-v1}"
reuse_novnc="${SIDEKICKS_POLISH_REUSE_NOVNC_FROM:-}"
case "$revision" in v1|v2) ;; *) echo 'Supported candidate revisions: v1, v2' >&2; exit 1;; esac
case "$reuse_novnc" in ''|v1) ;; *) echo 'Only the verified v1 noVNC template may be reused' >&2; exit 1;; esac
test -z "$reuse_novnc" || test "$revision" != "$reuse_novnc" || { echo 'Cannot reuse into the same revision' >&2; exit 1; }
build="$here/.build/desktop-polish-$revision"
evidence="$here/artifacts/desktop-polish/template-$revision"
mkdir -p "$build" "$evidence"
test ! -e "$build/novnc/base.ext4" && test ! -e "$build/neko/base.ext4" || { echo 'Candidate exists: inspect provenance, do not overwrite.' >&2; exit 1; }
python3 - "$build" <<'PY_FREE'
import shutil,sys
# Sequential export: retain3GiB first template, then at most~13GiB of
# Neko export/helper/copy data plus a4GiB free-space reserve. APFS NAT
# template cloning shares extents. This is a build budget, not a host limit.
if shutil.disk_usage(sys.argv[1]).free < 20*1024**3:
    raise SystemExit('At least20GiB free required for sequential preparation; no files were removed.')
PY_FREE
docker image inspect "$novnc_image" "$neko_image" "$loopback_image" "$formatter_image" > "$evidence/template-source-images.json"
docker image tag "$neko_image" sidekicks-neko:microvm-polish-base
docker image tag "$loopback_image" sidekicks-neko:microvm-loopback-polish-pinned
docker build --pull=false --network none -f "$here/polish/Dockerfile.neko" -t "sidekicks-neko:microvm-polish-$revision" "$here/polish"
neko_vm_image=$(docker image inspect -f '{{.Id}}' "sidekicks-neko:microvm-polish-$revision")
docker image inspect "$neko_vm_image" > "$evidence/template-neko-loopback-image.json"
if [ -n "$reuse_novnc" ]; then
 python3 - "$here/.build/desktop-polish-$reuse_novnc" "$novnc_image" <<'PY_REUSE'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);manifest=json.loads((root/'manifest.json').read_text())
assert manifest['novncSource']==sys.argv[2],'reused noVNC source differs'
path=root/'novnc/base.ext4';expected=manifest['files']['novnc/base.ext4'];h=hashlib.sha256()
with path.open('rb') as stream:
    while data:=stream.read(8*1024*1024):h.update(data)
assert path.stat().st_size==expected['bytes'] and h.hexdigest()==expected['sha256'],'reused template changed'
PY_REUSE
 mkdir -p "$build/novnc"
 cp -c "$here/.build/desktop-polish-$reuse_novnc/novnc/base.ext4" "$build/novnc/base.ext4"
fi
for profile in novnc neko; do
 if [ "$profile" = novnc ] && [ -n "$reuse_novnc" ]; then continue; fi
 export_name="sidekicks-polish-export-$profile"
 disk_name="sidekicks-polish-disk-$profile"
 for name in "$export_name" "$disk_name"; do
  if docker inspect "$name" >/dev/null 2>&1; then echo "Existing $name: inspect uncertain outcome before retry." >&2; exit 1; fi
 done
 mkdir -p "$build/$profile"
 image="$novnc_image"
 if [ "$profile" = neko ]; then image="$neko_vm_image"; fi
 docker create --name "$export_name" --label sidekicks.project=lab --label component=polish-export --network none --user 0 --entrypoint /bin/sh "$image" >/dev/null
 docker cp "$here/guest-init.py" "$export_name:/microvm-init.py"
 docker cp "$here/microvm_udp.py" "$export_name:/microvm_udp.py"
 docker cp "$here/microvm_network.py" "$export_name:/microvm_network.py"
 if [ "$profile" = neko ]; then
  docker cp "$export_name:/app/server.mjs" "$build/neko/server.mjs"
  docker cp "$export_name:/etc/sidekicks/neko.yaml" "$build/neko/neko.yaml"
  python3 "$here/neko/prepare-rootfs.py" "$build/neko"
  docker cp "$build/neko/server.mjs" "$export_name:/app/server.mjs"
  docker cp "$build/neko/neko.yaml" "$export_name:/etc/sidekicks/neko.yaml"
 fi
 docker export "$export_name" -o "$build/$profile/rootfs.tar"
 docker rm "$export_name" >/dev/null
 size=3G; bytes=3221225472
 if [ "$profile" = neko ]; then size=4G; bytes=4294967296; fi
 docker create --name "$disk_name" --label sidekicks.project=lab --label component=polish-disk --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh "$formatter_image" -c "set -eu
mkdir /stage
tar --exclude=dev/* -xpf /rootfs.tar -C /stage
mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp
truncate -s $size /guest.ext4
/usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4
/usr/sbin/e2fsck -fn /guest.ext4" >/dev/null
 docker cp "$build/$profile/rootfs.tar" "$disk_name:/rootfs.tar"
 docker start -a "$disk_name"
 docker cp "$disk_name:/guest.ext4" "$build/$profile/base.ext4.partial"
 python3 - "$build/$profile/base.ext4.partial" "$bytes" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size==int(sys.argv[2]),'incomplete copy; partial retained'
PY_SIZE
 mv "$build/$profile/base.ext4.partial" "$build/$profile/base.ext4"
 docker rm "$disk_name" >/dev/null
 # Remove only this new reproducible export after successful filesystem validation.
 rm "$build/$profile/rootfs.tar"
done
# NAT uses the same image/software; per-boot profile supplies its NIC/IP/ports.
cp -c "$build/neko/base.ext4" "$build/base-nat.ext4"
python3 - "$build" "$novnc_image" "$neko_image" "$neko_vm_image" "$formatter_image" "$revision" "$reuse_novnc" <<'PY_MANIFEST'
import datetime,hashlib,json,pathlib,sys
p=pathlib.Path(sys.argv[1])
def digest(path):
    h=hashlib.sha256()
    with path.open('rb') as stream:
        while data:=stream.read(8*1024*1024):h.update(data)
    return h.hexdigest()
files={name:{'bytes':(p/name).stat().st_size,'sha256':digest(p/name)} for name in ['novnc/base.ext4','neko/base.ext4','base-nat.ext4']}
manifest={'createdAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'status':'PREPARED_NOT_BOOT_VERIFIED','novncSource':sys.argv[2],'nekoSource':sys.argv[3],'nekoLoopbackImage':sys.argv[4],'formatterImage':sys.argv[5],'revision':sys.argv[6],'noVNCReusedFrom':sys.argv[7] or None,'files':files,'canonicalTemplatesChanged':False,'runnerChanged':False,'productionGuestsChanged':False}
(p/'manifest.json.partial').write_text(json.dumps(manifest,indent=2)+'\n')
(p/'manifest.json.partial').replace(p/'manifest.json')
PY_MANIFEST
echo 'Separate candidate templates prepared. No current guest or canonical template changed.'
