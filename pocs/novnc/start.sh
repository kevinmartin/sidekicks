#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
source "$here/current-image.sh"
name=sidekicks-novnc
if docker inspect "$name" >/dev/null 2>&1; then
  actual="$(docker inspect -f '{{.Image}}' "$name")"
  if [ "$actual" != "$NOVNC_CURRENT_IMAGE" ]; then
    printf 'Existing %s uses %s; current Xfce image is %s.\n' "$name" "$actual" "$NOVNC_CURRENT_IMAGE" >&2
    printf 'No container was changed. Coordinate stop/rename/preserve and promotion after candidate QA.\n' >&2
    exit 1
  fi
  if [ "$(docker inspect -f '{{.State.Running}}' "$name")" != true ]; then docker start "$name" >/dev/null; fi
else
  if ! docker image inspect "$NOVNC_CURRENT_IMAGE" >/dev/null 2>&1; then
    printf 'Pinned current Xfce image %s is absent. Restore its saved image, or run %s/rebuild.sh to build a QA candidate from Dockerfile.xfce-clean.\n' "$NOVNC_CURRENT_IMAGE" "$here" >&2
    printf 'A fresh build needs independent QA and explicit promotion; start never falls back to a historical desktop.\n' >&2
    exit 1
  fi
  docker run -d --init --name "$name" --platform linux/arm64 --cpus 2 --memory 2g --shm-size 512m --cap-drop ALL --security-opt no-new-privileges -p 127.0.0.1:7901:6080 -p 127.0.0.1:7791:8080 "$NOVNC_CURRENT_IMAGE" >/dev/null
fi
for i in $(seq 1 90); do
  if curl -fsS http://127.0.0.1:7791/health >/dev/null 2>&1 && curl -fsS http://127.0.0.1:7791/desktop/state >/dev/null 2>&1; then printf 'noVNC viewer: http://127.0.0.1:7901/\nAdapter: http://127.0.0.1:7791\n'; exit 0; fi
  sleep 1
done
docker logs --tail 60 "$name"
exit 1
