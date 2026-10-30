#!/usr/bin/env bash
# Explicit configuration rebuild over the verified, clean/pruned v7 runtime.
# Produces a separate candidate; never starts a service or overwrites v7.
set -euo pipefail
export LC_ALL=C LANG=C
HERE="$(cd "$(dirname "$0")" && pwd)"
BASE=sidekicks-neko:polish-xfce-stage-v7
EXPECTED=sha256:36e11c585ece469a01f5bf705d5e1357afe767ba8e858eafba548d38786457a1
IMAGE="${SIDEKICKS_NEKO_XFCE_IMAGE:-sidekicks-neko:polish-xfce-stage-rebuild-$(date -u +%Y%m%dT%H%M%SZ)}"
case "$IMAGE" in sidekicks-neko:polish-xfce-stage-rebuild-*) ;; *) printf 'Use a separate polish-xfce-stage-rebuild-* candidate tag.\n' >&2; exit 1 ;; esac
test "$(docker image inspect -f '{{.Id}}' "$BASE")" = "$EXPECTED" || { printf 'Missing/mismatched verified base. See XFCE.md for the full recorded rebuild chain.\n' >&2; exit 1; }
if docker image inspect "$IMAGE" >/dev/null 2>&1; then printf 'Candidate tag already exists; choose a new tag.\n' >&2; exit 1; fi
SIDEKICKS_NEKO_XFCE_IMAGE="$IMAGE" SIDEKICKS_NEKO_XFCE_BASE="$EXPECTED" bash "$HERE/build-xfce-stage.sh"
printf 'Candidate only: %s. Run isolated stage verification before changing current-image.json.\n' "$IMAGE"
