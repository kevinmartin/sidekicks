#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")" && pwd)"
build="$here/.build"
mkdir -p "$build/neko" "$here/artifacts/neko-udp"
image=$(docker image inspect -f '{{.Id}}' sidekicks-neko:microvm-loopback)
docker image inspect "$image" > "$here/artifacts/neko-udp/source-image.json"
source_name=sidekicks-microvm-neko-export
builder_name=sidekicks-microvm-neko-disk
for name in "$source_name" "$builder_name"; do
  if docker inspect "$name" >/dev/null 2>&1; then echo "Refusing to replace existing $name" >&2; exit 1; fi
done
docker create --name "$source_name" --network none --user 0 --entrypoint /bin/sh "$image" >/dev/null
trap 'docker rm "$source_name" >/dev/null 2>&1 || true' EXIT
chmod +x "$here/guest-init.py"
docker cp "$here/guest-init.py" "$source_name:/microvm-init.py"
docker cp "$here/microvm_udp.py" "$source_name:/microvm_udp.py"
docker cp "$source_name:/app/server.mjs" "$build/neko/server.mjs"
docker cp "$source_name:/etc/sidekicks/neko.yaml" "$build/neko/neko.yaml"
python3 "$here/neko/prepare-rootfs.py" "$build/neko"
docker cp "$build/neko/server.mjs" "$source_name:/app/server.mjs"
docker cp "$build/neko/neko.yaml" "$source_name:/etc/sidekicks/neko.yaml"
docker export "$source_name" -o "$build/neko/rootfs.tar"
docker rm "$source_name" >/dev/null
trap - EXIT
# Existing accepted builder image has mkfs.ext4; no privileged mount or host share.
docker create --name "$builder_name" --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh sidekicks-novnc:local -c 'set -eu; mkdir /stage; tar --exclude=dev/* -xpf /rootfs.tar -C /stage; mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp; truncate -s 4G /guest.ext4; /usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4; /usr/sbin/e2fsck -fn /guest.ext4' >/dev/null
docker cp "$build/neko/rootfs.tar" "$builder_name:/rootfs.tar"
docker start -a "$builder_name"
docker cp "$builder_name:/guest.ext4" "$build/base-neko.ext4.partial"
python3 - "$build/base-neko.ext4.partial" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size == 4 * 1024**3
PY_SIZE
mv "$build/base-neko.ext4.partial" "$build/base-neko.ext4"
docker rm "$builder_name" >/dev/null
swiftc -module-cache-path "$build/module-cache" "$here/Runner.swift" -o "$build/runner-neko.next"
codesign --force --sign - --timestamp=none --entitlements "$here/vz.entitlements" "$build/runner-neko.next"
mv "$build/runner-neko.next" "$build/runner-neko"
shasum -a 256 "$build/base-neko.ext4" "$build/runner-neko" > "$here/artifacts/neko-udp/build.sha256"
echo 'Separate Neko VZ assets ready; no guest started.'
