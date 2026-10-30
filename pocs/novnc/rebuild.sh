#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
source "$here/current-image.sh"
if [ "$#" -gt 1 ]; then printf 'usage: %s [new-candidate-tag]\n' "$0" >&2; exit 2; fi
tag="${1:-sidekicks-novnc:xfce-candidate}"
case "$tag" in
  sidekicks-novnc:local|sidekicks-novnc:live-drag|sidekicks-novnc:cloud-ux|sidekicks-novnc:polish-xfce-final|sha256:*)
    printf 'Refusing to overwrite a preserved/current image identity: %s\n' "$tag" >&2; exit 2;;
esac
baseline="$(docker image inspect -f '{{.Id}}' sidekicks-novnc:live-drag 2>/dev/null || true)"
if [ "$baseline" != "$NOVNC_TRANSPORT_BASELINE" ]; then
  printf 'The clean recipe requires preserved transport image sidekicks-novnc:live-drag = %s. Restore that recorded image first.\n' "$NOVNC_TRANSPORT_BASELINE" >&2
  printf 'No fallback recipe, runtime replacement or baseline retag was performed.\n' >&2
  exit 1
fi
docker build --platform linux/arm64 -f "$here/Dockerfile.xfce-clean" -t "$tag" "$here"
candidate="$(docker image inspect -f '{{.Id}}' "$tag")"
printf 'Built Xfce clean candidate %s (%s). No container was changed.\n' "$tag" "$candidate"
printf 'Run isolated stage QA, then coordinate preservation/cutover and pin the accepted image in current-image.sh.\n'
