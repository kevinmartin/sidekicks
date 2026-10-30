#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")" && pwd)"
build="$here/.build"
mkdir -p "$here/artifacts/nat"
test -f "$build/neko/rootfs.tar"
test -f "$build/vmlinux"
name=sidekicks-microvm-nat-disk
if docker inspect "$name" >/dev/null 2>&1; then echo "Refusing to replace existing $name" >&2; exit 1; fi
# Same prepared Neko rootfs/software as relay. Only PID1/network setup differs.
docker create --name "$name" --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh sidekicks-novnc:local -c 'set -eu; mkdir /stage; tar --exclude=dev/* -xpf /rootfs.tar -C /stage; cp /guest-init.py /stage/microvm-init.py; chmod +x /stage/microvm-init.py; cp /microvm_network.py /stage/microvm_network.py; mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp; truncate -s 4G /guest.ext4; /usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4; /usr/sbin/e2fsck -fn /guest.ext4' >/dev/null
docker cp "$build/neko/rootfs.tar" "$name:/rootfs.tar"
docker cp "$here/guest-init.py" "$name:/guest-init.py"
docker cp "$here/microvm_network.py" "$name:/microvm_network.py"
docker start -a "$name"
docker cp "$name:/guest.ext4" "$build/base-nat.ext4.partial"
python3 - "$build/base-nat.ext4.partial" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size == 4 * 1024**3
PY_SIZE
mv "$build/base-nat.ext4.partial" "$build/base-nat.ext4"
docker rm "$name" >/dev/null
swiftc -module-cache-path "$build/module-cache" "$here/Runner.swift" -o "$build/runner-nat.next"
codesign --force --sign - --timestamp=none --entitlements "$here/vz.entitlements" "$build/runner-nat.next"
mv "$build/runner-nat.next" "$build/runner-nat"
shasum -a 256 "$build/base-nat.ext4" "$build/runner-nat" > "$here/artifacts/nat/build.sha256"
echo 'Separate NAT guest assets prepared; no VM started.'
