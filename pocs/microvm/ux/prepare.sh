#!/usr/bin/env bash
# Build new templates while current guest disks/runners remain untouched.
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")/.." && pwd)"
novnc_image="${1:?exact tested noVNC UX image ID required}"
neko_image="${2:?exact tested Neko UX image ID required}"
loopback_image="sha256:c27c3aab3181aa24dad92efb70a707d3c24f019d63c60279e42e1654c5346146"
build="$here/.build"
ux="$build/ux1"
mkdir -p "$ux" "$here/artifacts/ux"
test -s "$build/vmlinux" || { echo 'Existing pinned kernel missing; run baseline preparation first.' >&2; exit 1; }
python3 - "$ux" <<'PY_FREE'
import shutil,sys
if shutil.disk_usage(sys.argv[1]).free < 25 * 1024**3:
    raise SystemExit('At least25GiB free required for bounded template/helper preparation; preserve existing evidence and inspect space.')
PY_FREE
test "$(docker image inspect -f '{{.Id}}' "$novnc_image")" = "$novnc_image"
test "$(docker image inspect -f '{{.Id}}' "$neko_image")" = "$neko_image"
test "$(docker image inspect -f '{{.Id}}' "$loopback_image")" = "$loopback_image"
docker image inspect "$novnc_image" "$neko_image" "$loopback_image" > "$here/artifacts/ux/source-images.json"
# BuildKit interprets raw sha256 IDs in FROM as repository names. Local aliases
# are assigned from the verified IDs; --pull=false keeps these local bases.
docker image tag "$neko_image" sidekicks-neko:microvm-ux-base
docker image tag "$loopback_image" sidekicks-neko:microvm-loopback-pinned
docker build --pull=false --network none -f "$here/ux/Dockerfile.neko" -t sidekicks-neko:microvm-ux1 "$here/ux"
neko_vm_image=$(docker image inspect -f '{{.Id}}' sidekicks-neko:microvm-ux1)
docker image inspect "$neko_vm_image" > "$here/artifacts/ux/neko-loopback-image.json"
for profile in novnc neko; do
 name="sidekicks-ux-export-$profile"
 disk="sidekicks-ux-disk-$profile"
 for container in "$name" "$disk"; do
  if docker inspect "$container" >/dev/null 2>&1; then echo "Inspect existing $container before retry" >&2; exit 1; fi
 done
 mkdir -p "$ux/$profile"
 image="$novnc_image"
 if [ "$profile" = neko ]; then image="$neko_vm_image"; fi
 docker create --name "$name" --network none --user 0 --entrypoint /bin/sh "$image" >/dev/null
 docker cp "$here/guest-init.py" "$name:/microvm-init.py"
 docker cp "$here/microvm_udp.py" "$name:/microvm_udp.py"
 docker cp "$here/microvm_network.py" "$name:/microvm_network.py"
 if [ "$profile" = neko ]; then
  docker cp "$name:/app/server.mjs" "$ux/neko/server.mjs"
  docker cp "$name:/etc/sidekicks/neko.yaml" "$ux/neko/neko.yaml"
  python3 "$here/neko/prepare-rootfs.py" "$ux/neko"
  docker cp "$ux/neko/server.mjs" "$name:/app/server.mjs"
  docker cp "$ux/neko/neko.yaml" "$name:/etc/sidekicks/neko.yaml"
 fi
 docker export "$name" -o "$ux/$profile/rootfs.tar"
 docker rm "$name" >/dev/null
 size=3G
 bytes=3221225472
 if [ "$profile" = neko ]; then size=4G; bytes=4294967296; fi
 docker create --name "$disk" --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh "$novnc_image" -c "set -eu
mkdir /stage
tar --exclude=dev/* -xpf /rootfs.tar -C /stage
mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp
truncate -s $size /guest.ext4
/usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4
/usr/sbin/e2fsck -fn /guest.ext4" >/dev/null
 docker cp "$ux/$profile/rootfs.tar" "$disk:/rootfs.tar"
 docker start -a "$disk"
 docker cp "$disk:/guest.ext4" "$ux/$profile/base.ext4.partial"
 python3 - "$ux/$profile/base.ext4.partial" "$bytes" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size==int(sys.argv[2]),'incomplete disk copy'
PY_SIZE
 mv "$ux/$profile/base.ext4.partial" "$ux/$profile/base.ext4"
 docker rm "$disk" >/dev/null
done
# NAT and relay use the identical Neko template; per-boot profile sets networking.
cp -c "$ux/neko/base.ext4" "$ux/base-nat.ext4"
swiftc -module-cache-path "$build/module-cache" "$here/Runner.swift" -o "$ux/runner.next"
codesign --force --sign - --timestamp=none --entitlements "$here/vz.entitlements" "$ux/runner.next"
mv "$ux/runner.next" "$ux/runner"
printf '%s\n' 'UX templates prepared separately; no live VM or default template was changed.'
