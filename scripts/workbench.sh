#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
run="$root/.run"
mkdir -p "$run/go-cache" "$run/go"
container=sidekicks-workbench
has_container() { docker container inspect "$container" >/dev/null 2>&1; }
verify_owner() { test "$(docker inspect -f '{{index .Config.Labels "sidekicks.project"}}' "$container")" = lab || { echo 'Refusing to modify a container without the Sidekicks ownership label.' >&2; exit 1; }; }
build_image() {
 arch=$(docker info --format '{{.Architecture}}')
 case "$arch" in aarch64|arm64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo "Unsupported Docker architecture $arch" >&2; exit 1;; esac
 mkdir -p "$run/workbench-image/shared"
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOCACHE="$run/go-cache" GOPATH="$run/go" go build -o "$run/workbench-image/sidekicks-workbench" ./harness/workbench
 cp shared/desktop-fixture.html "$run/workbench-image/shared/desktop-fixture.html"
 for id in novnc neko; do
  mkdir -p "$run/workbench-image/pocs/$id"
  if test -f "pocs/$id/status.json"; then cp "pocs/$id/status.json" "$run/workbench-image/pocs/$id/status.json"; else printf '{}\n' > "$run/workbench-image/pocs/$id/status.json"; fi
 done
 cp harness/workbench/Dockerfile "$run/workbench-image/Dockerfile"
 docker build --platform "linux/$arch" -t sidekicks-workbench:local "$run/workbench-image"
}
run_container() {
 docker run -d --name "$container" --label sidekicks.project=lab --restart unless-stopped --cpus 0.5 --memory 128m --pids-limit 64 --cap-drop ALL --security-opt no-new-privileges --read-only --tmpfs /app/.run:rw,noexec,nosuid,size=1m,mode=1777 -p 127.0.0.1:7790:7790 sidekicks-workbench:local
}
case "${1:-status}" in
 start|rebuild)
  if has_container; then
   verify_owner
   if test "${1:-start}" = rebuild; then
    # Build while the current workbench remains available, then preserve its
    # exact container/image for rollback instead of deleting it first.
    build_image
    previous="$container-preserved-$(date -u +%Y%m%dT%H%M%S)"
    docker stop "$container" >/dev/null
    docker rename "$container" "$previous"
    printf '%s\n' "$previous" > "$run/workbench-previous-container"
    run_container
   elif test "$(docker inspect -f '{{.State.Running}}' "$container")" = true; then echo 'Workbench already running.'
   else docker start "$container"; fi
  else build_image; run_container; fi
  for attempt in {1..30}; do
   if curl -fsS --max-time 1 http://127.0.0.1:7790/health >/dev/null 2>&1; then echo 'Workbench ready: http://127.0.0.1:7790'; exit 0; fi
   sleep 0.2
  done
  echo 'Workbench failed to start; inspect docker logs sidekicks-workbench' >&2; exit 1;;
 start-host)
  GOCACHE="$run/go-cache" GOPATH="$run/go" go build -o "$run/sidekicks-workbench" ./harness/workbench
  exec "$run/sidekicks-workbench" -root "$root";;
 stop)
  if has_container; then verify_owner; docker stop "$container"; fi
  if test -f "$run/workbench.pid"; then
   pid=$(cat "$run/workbench.pid")
   if [[ "$pid" =~ ^[0-9]+$ ]] && ps -p "$pid" -o command= | grep -F -- "$run/sidekicks-workbench -root $root" >/dev/null; then kill "$pid"; fi
  fi
  echo 'Workbench stopped (task-owned processes only).';;
 status)
  if has_container; then docker inspect -f '{{.Name}} {{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} restart={{.HostConfig.RestartPolicy.Name}}' "$container"; fi
  curl -fsS --max-time 3 http://127.0.0.1:7790/health; echo;;
 test) GOCACHE="$run/go-cache" GOPATH="$run/go" go test -race ./harness/workbench; node --check harness/workbench/web/app.js;;
 *) echo 'Usage: scripts/workbench.sh start|rebuild|start-host|stop|status|test' >&2; exit 2;;
esac
