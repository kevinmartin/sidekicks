#!/usr/bin/env bash
# A separate noVNC-only template; never changes active guests or Neko assets.
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")/.." && pwd)"
image="${1:?exact tested noVNC image ID required}"
dest="$here/.build/novnc-live-drag"
mkdir -p "$dest" "$here/artifacts/ux/live-drag"
test "$(docker image inspect -f '{{.Id}}' "$image")" = "$image"
for name in sidekicks-novnc-drag-export sidekicks-novnc-drag-disk; do
 if docker inspect "$name" >/dev/null 2>&1; then echo "Inspect existing $name before retry" >&2; exit 1; fi
done
docker image inspect "$image" > "$here/artifacts/ux/live-drag/source-image.json"
docker create --name sidekicks-novnc-drag-export --network none --user 0 --entrypoint /bin/sh "$image" >/dev/null
docker cp "$here/guest-init.py" sidekicks-novnc-drag-export:/microvm-init.py
docker cp "$here/microvm_network.py" sidekicks-novnc-drag-export:/microvm_network.py
docker cp "$here/microvm_udp.py" sidekicks-novnc-drag-export:/microvm_udp.py
docker export sidekicks-novnc-drag-export -o "$dest/rootfs.tar"
docker rm sidekicks-novnc-drag-export >/dev/null
docker create --name sidekicks-novnc-drag-disk --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh "$image" -c 'set -eu
mkdir /stage
tar --exclude=dev/* -xpf /rootfs.tar -C /stage
mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp
truncate -s 3G /guest.ext4
/usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4
/usr/sbin/e2fsck -fn /guest.ext4' >/dev/null
docker cp "$dest/rootfs.tar" sidekicks-novnc-drag-disk:/rootfs.tar
docker start -a sidekicks-novnc-drag-disk
docker cp sidekicks-novnc-drag-disk:/guest.ext4 "$dest/base.ext4.partial"
python3 - "$dest/base.ext4.partial" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size==3*1024**3,'incomplete disk copy'
PY_SIZE
mv "$dest/base.ext4.partial" "$dest/base.ext4"
docker rm sidekicks-novnc-drag-disk >/dev/null
printf '%s\n' 'Separate noVNC template ready; current guests and default templates untouched.'
