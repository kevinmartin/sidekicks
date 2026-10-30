# Shared runtime status

Owner: integration lead. Updated 2026-10-26 UTC.

- Host: macOS 15.3, arm64, Apple M1 Max, 10 cores, 32 GB RAM.
- Existing tools: Docker Desktop client 27.5.1, Go 1.27.3, Node 22.19.0 / npm 10.9.3, Python 3.13.0, Google Chrome, Firefox.
- No OrbStack / Colima / QEMU CLI found. No new runtime installation needed.
- Docker daemon initially stopped. Starting existing Docker Desktop was approved and `open -a Docker` completed; daemon readiness is being checked.
- Runtime choice: Docker Desktop Linux VM; prefer native linux/arm64 images. Document an amd64 fallback as emulated and do not compare its performance as native.
- Docker commands need executor escalation to access the user's socket. Request scoped actions normally; do not alter Docker security settings.
- Workspace was empty. No AGENTS.md or local project skills found in ancestor folders. No existing work modified.
- `git init` was prevented by the workspace's read-only `.git` rule. Continue without a repository; do not block implementation.

## Reserved ports (all host bindings MUST use 127.0.0.1)

| Owner | Purpose | Host port |
|---|---|---:|
| integration | Go workbench + shared fixture | 7790/tcp |
| noVNC | adapter API | 7791/tcp |
| Neko | adapter API | 7792/tcp |
| noVNC | human viewer / websocket | 7901/tcp |
| Neko | human viewer / signaling | 7902/tcp |
| Neko | WebRTC media range, if needed | 59020-59039/udp |

Use project/container prefix `sidekicks-`. Do not stop or modify unrelated containers. Do not expose VNC directly on the host. Do not mount home or use privileged containers. Cap each desktop at 2 CPUs / 2 GiB initially; shared memory may be 512 MiB. The integration lead alone owns shared runtime startup.

## Ready

Docker Desktop startup completed and `docker version` / `docker info` succeeded with executor escalation. Server: Docker Desktop 4.38.0 / Engine 27.5.1, linux/arm64; VM has 10 CPUs and 16,748,548,096 bytes memory (~15.6 GiB). Three pre-existing pickleballworkflows containers were observed and left untouched. Neko worker informed of reserved ports and contract.

## Durable overnight lifecycle

The workbench now runs in Docker as `sidekicks-workbench` with restart policy `unless-stopped`, non-root USER 65532, 0.5 CPU / 128 MiB, read-only filesystem, a 1 MiB ephemeral runtime tmpfs, no host mounts, and only `127.0.0.1:7790`. Docker healthcheck is healthy. A minimal local image is built from the Go binary and synthetic fixture, with no additional downloads. Adapter transport uses Docker Desktop's `host.docker.internal` while preserving each adapter's public loopback Host header; adapter Host gates remain enforced.

A temporary user launch job `io.sidekicks.overnight-awake` holds `caffeinate -i` until a fixed deadline: 2026-10-26 10:30:58 UTC (06:30:58 America/New_York). The wrapper removes itself at the deadline and does not extend the window if relaunched. `pmset -g assertions` confirmed caffeinate PID 96266 owns PreventUserIdleSystemSleep, timeout ~28,800 seconds. No power/security settings or persistent plist changed. `scripts/stop.sh` removes this task-owned assertion. Lid close, explicit sleep, logout or shutdown can still interrupt access; restart commands are documented.

## Final measurement coordination

noVNC owner reports IDLE after restoring a fresh fixture at 2026-10-26T03:28Z (23:28 New York); its four supplemental stress rounds are complete. No further noVNC input/build activity is planned during QA's sequential measurement window. Neko final polish and explicit owner IDLE confirmation are still being coordinated by QA. Integration is doing documentation-only artifact review, with no desktop input, capture requests, builds or test runs. Previous noVNC 98 ms measurement remains provisional and must not be compared until the matched repeat.

Neko owner confirmed IDLE at 2026-10-26T03:30Z (23:30 New York). Both builders are now idle for QA's final native-app/restart and matched sequential measurement window. Integration performs only local artifact/document reads and writes; no service rebuild, POC input/capture or benchmark is scheduled until QA release.


## Final measurement window complete

QA completed the final noVNC then Neko native n=30 runs at approximately 23:36 New York, after both builders were idle. Exact metrics and caveats are in README, integration status and QA's independent report. The workbench alone was rebuilt afterward to include final recorded status snapshots; POC images remain unchanged. QA is refreshing the workbench screenshot/checks. Services remain loopback-only and task-scoped; temporary awake assertion still expires at 06:30:58 New York.


Implementation is frozen after QA's final workbench 7/7 PASS at 23:52 New York. No further container/image/code changes are scheduled. Git initialization was subsequently approved and succeeded; local repository main has no commits, with no remote operation performed. Earlier Git setup restriction is resolved. All demo URLs and cleanup instructions remain in README.


Final QA passive state and artifact transfer are complete: both backends health200, no owner or held inputs, workbench200/bothready. Library report, three sanitized screens (workbench version1) and both raw final timing JSONs saved successfully. No execution or transfer blocker remains. Implementation/runtime are frozen; README/status-only reconciliation completed.


Latest final artifact versions after parent review: verification report v1, workbench screenshot v2, stable Library IDs preserved; metadata helpers succeeded. Post-persona workbench regression7/7PASS. No further runtime/source/input activity is planned. Median arithmetic correction used unchanged raw samples (98.05/62.05ms); guest-browser/viewport/encoder differences are documented as whole-POC context.
