#!/bin/bash
# Explicit clean-image builds only; no container lifecycle, prune or POC retag.
set -euo pipefail
cd "$(dirname "$0")/../.."
arch="${SIDEKICKS_IMAGE_ARCH:-$(go env GOARCH)}"
case "$arch" in arm64|amd64) ;; *) echo 'SIDEKICKS_IMAGE_ARCH must be arm64 or amd64' >&2; exit 1 ;; esac
socket="${SIDEKICKS_DOCKER_SOCKET:-}"
if [[ -z "$socket" ]]; then
  if [[ "$(uname -s)" == Darwin ]]; then socket="$HOME/.docker/run/docker.sock"; else socket=/var/run/docker.sock; fi
fi
[[ "$socket" == /* ]] || { echo 'SIDEKICKS_DOCKER_SOCKET must be an absolute local Unix path' >&2; exit 1; }
[[ -x .run/compute-bin/sidekicks-agent ]] || { echo 'Run make build first' >&2; exit 1; }
[[ -s .run/compute-web/neko.js ]] || { echo 'Run make web-build first' >&2; exit 1; }
# The static binary is a prerequisite for the selected platform; Make supplies it.
# --pull=false avoids a mutable-tag refresh; absent pinned base layers are still
# downloaded from official registries during this explicit build operation.
for adapter in shell browser-chromium desktop-novnc desktop-neko; do
  docker --host "unix://$socket" build --platform "linux/$arch" --pull=false \
    --target "$adapter" -f infrastructure/images/clean/Dockerfile \
    -t "sidekicks-compute/$adapter:clean" -t "sidekicks-compute/$adapter:dev" .
done
