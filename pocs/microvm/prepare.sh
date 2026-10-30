#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")" && pwd)"
build="$here/.build"
mkdir -p "$build" "$here/artifacts"
archive="$build/kata-static-3.17.0-arm64.tar.xz"
url=https://github.com/kata-containers/kata-containers/releases/download/3.17.0/kata-static-3.17.0-arm64.tar.xz
if [ ! -f "$archive" ]; then
  # No silent retries: inspect a failed download before rerunning.
  curl -fL --max-time 180 "$url" -o "$archive.partial"
  mv "$archive.partial" "$archive"
fi
shasum -a 256 "$archive" > "$here/artifacts/kernel-archive.sha256"
tar -xJf "$archive" -C "$build" ./opt/kata/share/kata-containers/vmlinux.container ./opt/kata/share/kata-containers/vmlinux-6.12.28-153
cp -L "$build/opt/kata/share/kata-containers/vmlinux.container" "$build/vmlinux"
shasum -a 256 "$build/vmlinux" > "$here/artifacts/kernel.sha256"
image=$(docker image inspect -f '{{.Id}}' sidekicks-novnc:local)
docker image inspect "$image" > "$here/artifacts/source-image.json"
source_name=sidekicks-microvm-export
builder_name=sidekicks-microvm-disk-builder
for name in "$source_name" "$builder_name"; do
  if docker inspect "$name" >/dev/null 2>&1; then
    echo "Refusing to replace existing $name; inspect and clean the previous attempt explicitly." >&2
    exit 1
  fi
done
docker create --name "$source_name" --network none --user 0 --entrypoint /bin/sh "$image" >/dev/null
trap 'docker rm "$source_name" >/dev/null 2>&1 || true' EXIT
chmod +x "$here/guest-init.py"
docker cp "$here/guest-init.py" "$source_name:/microvm-init.py"
# Extend allowed local origins only inside this copied rootfs, preserving the original image.
docker cp "$source_name:/app/server.py" "$build/server.py"
python3 - "$build/server.py" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1])
s = p.read_text()
if 'SIDEKICKS_PUBLIC_VIEWER_PORT' in s and 'SIDEKICKS_PUBLIC_API_PORT' in s:
    # Current UX images already read per-boot public ports from guest-init.
    assert "{PUBLIC_VIEWER_PORT}" in s and 'PUBLIC_API_PORT' in s
else:
    assert 'ALLOWED_PORTS = {6080,8080,7790,7791,7901}' in s
    s = s.replace('ALLOWED_PORTS = {6080,8080,7790,7791,7901}', "ALLOWED_PORTS = {6080,8080,7790,7791,7901} | {int(os.environ.get('SIDEKICKS_VIEWER_PORT','7903')), int(os.environ.get('SIDEKICKS_API_PORT','7793'))}")
    assert "'viewerUrl':'http://127.0.0.1:7901/'" in s
    s = s.replace("'viewerUrl':'http://127.0.0.1:7901/'", "'viewerUrl':'http://127.0.0.1:' + os.environ.get('SIDEKICKS_VIEWER_PORT','7903') + '/'")
p.write_text(s)
PY
docker cp "$build/server.py" "$source_name:/app/server.py"
docker export "$source_name" -o "$build/rootfs.tar"
docker rm "$source_name" >/dev/null
trap - EXIT
docker create --name "$builder_name" --network none --user 0 --cpus 2 --memory 2g --security-opt no-new-privileges --entrypoint /bin/sh "$image" -c 'set -eu; mkdir /stage; tar --exclude=dev/* -xpf /rootfs.tar -C /stage; mkdir -p /stage/dev /stage/proc /stage/sys /stage/run /stage/tmp; truncate -s 3G /guest.ext4; /usr/sbin/mkfs.ext4 -q -F -d /stage /guest.ext4; /usr/sbin/e2fsck -fn /guest.ext4' >/dev/null
docker cp "$build/rootfs.tar" "$builder_name:/rootfs.tar"
docker start -a "$builder_name"
docker cp "$builder_name:/guest.ext4" "$build/base.ext4.partial"
python3 - "$build/base.ext4.partial" <<'PY_SIZE'
from pathlib import Path
import sys
assert Path(sys.argv[1]).stat().st_size == 3 * 1024**3, "incomplete guest disk copy"
PY_SIZE
mv "$build/base.ext4.partial" "$build/base.ext4"
docker rm "$builder_name" >/dev/null
shasum -a 256 "$build/base.ext4" > "$here/artifacts/base-disk.sha256"
bash "$here/build-runner.sh"
echo 'Prepared first-level VZ VM assets; no VM is running yet.'
