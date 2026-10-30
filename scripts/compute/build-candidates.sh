#!/bin/bash
# Build a separate, run-scoped image set. Never retag or delete existing images.
set -eEuo pipefail
cd "$(dirname "$0")/../.."

arch="${SIDEKICKS_IMAGE_ARCH:-$(go env GOARCH)}"
case "$arch" in arm64|amd64) ;; *) echo 'SIDEKICKS_IMAGE_ARCH must be arm64 or amd64' >&2; exit 1 ;; esac
socket="${SIDEKICKS_DOCKER_SOCKET:-}"
if [[ -z "$socket" ]]; then
  if [[ "$(uname -s)" == Darwin ]]; then socket="$HOME/.docker/run/docker.sock"; else socket=/var/run/docker.sock; fi
fi
[[ "$socket" == /* && "$socket" != *$'\n'* ]] || { echo 'SIDEKICKS_DOCKER_SOCKET must be an absolute local Unix path' >&2; exit 1; }
[[ -x .run/compute-bin/sidekicks-agent ]] || { echo 'Run make build first' >&2; exit 1; }
[[ -s .run/compute-web/neko.js ]] || { echo 'Run make web-build first' >&2; exit 1; }

if [[ ${SIDEKICKS_CANDIDATE_RUN_ID+x} ]]; then
  run_id="$SIDEKICKS_CANDIDATE_RUN_ID"
else
  run_id="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
fi
[[ "$run_id" =~ ^[0-9a-f]{32}$ ]] || { echo 'Candidate run ID must be 32 lowercase hex characters' >&2; exit 1; }
umask 077
mkdir -p artifacts/compute/candidate-build
dir="artifacts/compute/candidate-build/$run_id"
mkdir "$dir" || { echo 'Candidate evidence directory already exists; refusing reuse' >&2; exit 1; }
stage=preflight
failed() {
  local code=$?
  trap - ERR
  printf 'FAIL stage=%s exit=%s\n' "$stage" "$code" > "$dir/FAILED"
  printf 'Candidate build failed at %s; partial images and evidence remain in %s\n' "$stage" "$dir" >&2
  exit "$code"
}
trap failed ERR
fail() {
  printf 'FAIL stage=%s exit=1\n' "$stage" > "$dir/FAILED"
  printf '%s; partial images and evidence remain in %s\n' "$1" "$dir" >&2
  exit 1
}

hash_stream() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'
  else shasum -a 256 | awk '{print $1}'
  fi
}
source_fingerprint() {
  bash scripts/compute/source-fingerprint.sh
}
hash_file() { hash_stream < "$1"; }
revision="$(git rev-parse HEAD)"
[[ "$revision" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]] || fail 'Invalid source revision'
source_sha="$(source_fingerprint)"
agent_sha="$(hash_file .run/compute-bin/sidekicks-agent)"
web_sha="$(hash_file .run/compute-web/neko.js)"
[[ "$source_sha" =~ ^[0-9a-f]{64}$ && "$agent_sha" =~ ^[0-9a-f]{64}$ && "$web_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'Invalid build input hash'
dirty=false
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || dirty=true
agent_info="$(go version -m .run/compute-bin/sidekicks-agent)"
printf '%s\n' "$agent_info" | awk -v arch="$arch" -v revision="$revision" -v dirty="$dirty" '
  $1 == "build" && $2 == "GOOS=linux" { os=1 }
  $1 == "build" && $2 == "GOARCH=" arch { cpu=1 }
  $1 == "build" && $2 == "CGO_ENABLED=0" { cgo=1 }
  $1 == "build" && $2 == "vcs.revision=" revision { vcs=1 }
  $1 == "build" && $2 == "vcs.modified=" dirty { worktree=1 }
  END { exit !(os && cpu && cgo && vcs && worktree) }
' || fail 'Agent binary build metadata does not match the current Linux architecture and Git state'

adapters=(shell browser-chromium desktop-novnc desktop-neko)
tags=()
for adapter in "${adapters[@]}"; do tags+=("sidekicks-compute/$adapter:verify-$run_id"); done
list_tags() { docker --host "unix://$socket" image ls --no-trunc --format '{{.Repository}}:{{.Tag}}'; }
tag_exists() {
  local line
  while IFS= read -r line; do [[ "$line" != "$1" ]] || return 0; done <<< "$existing"
  return 1
}
# An image-list error is a failure, not evidence that the tags are unused.
stage=collision-preflight
existing="$(list_tags)"
for tag in "${tags[@]}"; do
  if tag_exists "$tag"; then fail "Candidate tag already exists: $tag"; fi
done

ids=()
for i in "${!adapters[@]}"; do
  adapter="${adapters[$i]}"; tag="${tags[$i]}"; stage="build-$adapter"
  [[ "$(source_fingerprint)" == "$source_sha" && "$(hash_file .run/compute-bin/sidekicks-agent)" == "$agent_sha" && "$(hash_file .run/compute-web/neko.js)" == "$web_sha" ]] || fail 'Source or generated build input changed; refusing candidate build'
  existing="$(list_tags)"
  if tag_exists "$tag"; then fail "Candidate tag appeared during build: $tag"; fi
  # Missing pinned base images may be fetched by Docker. --pull=false avoids
  # refreshing already-present bases; APT/source downloads remain build inputs.
  docker --host "unix://$socket" build --platform "linux/$arch" --pull=false \
    --target "$adapter" --iidfile "$dir/$adapter.iid" \
    -f infrastructure/images/clean/Dockerfile -t "$tag" . > "$dir/$adapter.log" 2>&1
  id="$(cat "$dir/$adapter.iid")"
  [[ "$id" =~ ^sha256:[0-9a-f]{64}$ ]] || fail 'Build did not return an immutable image ID'
  tag_id="$(docker --host "unix://$socket" image inspect --format '{{.Id}}' "$tag")"
  exact_id="$(docker --host "unix://$socket" image inspect --format '{{.Id}}' "$id")"
  platform="$(docker --host "unix://$socket" image inspect --format '{{.Os}}/{{.Architecture}}' "$id")"
  [[ "$tag_id" == "$id" && "$exact_id" == "$id" && "$platform" == "linux/$arch" ]] || fail "Candidate identity or platform mismatch: $tag"
  if [[ "${#ids[@]}" -gt 0 ]]; then
    for seen in "${ids[@]}"; do [[ "$id" != "$seen" ]] || fail 'Different workloads produced the same image identity'; done
  fi
  ids+=("$id")
  printf '%s %s %s\n' "$adapter" "$tag" "$id" >> "$dir/completed.txt"
done
stage=final-source-check
[[ "$(source_fingerprint)" == "$source_sha" && "$(hash_file .run/compute-bin/sidekicks-agent)" == "$agent_sha" && "$(hash_file .run/compute-web/neko.js)" == "$web_sha" ]] || fail 'Source or generated build input changed; candidates are not accepted'

stage=manifest
{
  printf '{\n  "schemaVersion": 1,\n  "status": "built-not-verified",\n  "runId": "%s",\n' "$run_id"
  printf '  "architecture": "%s",\n  "gitRevision": "%s",\n  "dirty": %s,\n' "$arch" "$revision" "$dirty"
  printf '  "sourceFingerprintSHA256": "%s",\n  "agentSHA256": "%s",\n  "webBundleSHA256": "%s",\n' "$source_sha" "$agent_sha" "$web_sha"
  printf '  "images": [\n'
  for i in "${!adapters[@]}"; do
    suffix=,; [[ "$i" != 3 ]] || suffix=''
    printf '    {"adapter": "%s", "tag": "%s", "id": "%s"}%s\n' "${adapters[$i]}" "${tags[$i]}" "${ids[$i]}" "$suffix"
  done
  printf '  ],\n  "limitations": ["Candidate images are not verified or promoted", "APT repositories are not snapshot-pinned", "Source fingerprint is not a signed build attestation"]\n}\n'
} > "$dir/manifest.json.tmp"
mv "$dir/manifest.json.tmp" "$dir/manifest.json"
trap - ERR
printf 'Candidate images built but not verified: %s/manifest.json\n' "$dir"
