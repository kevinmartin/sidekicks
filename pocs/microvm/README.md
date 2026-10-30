> **Current shared desktop revision:** all three VM profiles now launch Sidekicks Home in a normal window, with cyan wallpaper and a floating Browser/Terminal/Files dock. Independent scoped UX interaction/restart verification is complete for all five desktops/eight routes, with explicit shortcut/blur limits. See [ux/README.md](ux/README.md) and [current UX verification](../../docs/ux-verification.md). Historical test counts, image IDs and performance below retain their original revision scope.

> **NAT alternative:** The separate session `nat` now has direct guest-IP UDP media plus verified public DNS/HTTPS at viewer7907/API7797. Independent functional/lifecycle checks and fresh relay-versus-NAT control/load measurements are complete. See [nat/README.md](nat/README.md). NAT is egress-enabled, not egress-isolated; the one-run results do not establish causal performance superiority.

> **Additive Neko media profile:** A separate no-NIC guest now implements loopback UDP edges through framed reliable vsock, viewer7906/API7796, UDP59120–59139. Builder and independent media, control, capture, restart/cleanup checks pass, with documented transport and takeover limits. See [neko/README.md](neko/README.md). Baseline noVNC A remains on7903/7793 and unchanged. Historical UDP limits below describe that original profile.

# Sidekicks microVM layer POC

**Live demo: http://127.0.0.1:7903/**. This is an actual 1280×720 Linux desktop delivered by noVNC/RFB over a host TCP → virtio-vsock bridge. It includes Xvfb, Openbox/tint2/picom, Chromium, xterm, PCManFM and the shared Home. The separate synthetic fixture remains at guest `/fixture`; the viewer streams an actual guest desktop.

This additive POC uses **direct Apple Virtualization.framework**, one first-level Linux VM per session. It is separate from Apple `container` CLI, the Containerization Swift package, Kata runtime and the accepted Docker POCs. The initial microVM POC did not modify the workbench; the shared desktop revision now links all five runtime variants there. The separate [review report](sidekicks-microvm-report.md) contains the exact21 builder checks and independent findings.

## Prerequisites and selected backend

The actual host is Apple M1 Max / ARM64, macOS **15.3 (24D60)**, Xcode **14.2**, Swift **5.7.2**. Apple `container` is absent. The current [Apple CLI](https://github.com/apple/container#requirements) requires macOS26, and [current Containerization source](https://github.com/apple/containerization#requirements) requires macOS26/Xcode26. That path is **prerequisite-blocked**; no unsupported install, OS upgrade or approval bypass was attempted.

Direct Virtualization.framework reports `supported:true` and runs on this host. No nested KVM/Kata is attempted on M1. A future Linux/KVM adapter is a separate backend, not a claim of tested EKS/GKE/Kata deployment. No cloud provisioning is authorized or performed.

## Reproduce

From the Sidekicks root, with existing Docker Desktop available for rootfs preparation only:

```bash
# Preparation downloads one official pinned Linux-kernel archive if absent,
# exports the existing ARM64 noVNC image, and builds/signs the task-local runner.
bash pocs/microvm/prepare.sh

python3 pocs/microvm/microvm.py create a 7903 7793
python3 pocs/microvm/microvm.py start a
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py endpoints a

# Exec stays inside the disposable guest, as uid1000.
python3 pocs/microvm/microvm.py exec a -- /usr/bin/id
python3 pocs/microvm/microvm.py exec a -- /bin/cat /etc/os-release
python3 pocs/microvm/microvm.py rpc a '{"op":"info"}'

# Files are limited to guest /workspace. Supply synthetic project files only.
python3 pocs/microvm/microvm.py write a shared/desktop-fixture.html fixture-copy.html
python3 pocs/microvm/microvm.py read a fixture-copy.html pocs/microvm/artifacts/file-roundtrip.html
python3 pocs/microvm/microvm.py capture a pocs/microvm/artifacts/manual-capture.png
```

`create` refuses an existing session directory; `start` refuses an already-running session. Default resources are fixed at2 vCPUs /2GiB and a3GiB writable raw ext4 disk. APFS clones provide separate writable disks from the prepared template. TCP endpoints are bound only to127.0.0.1. A long checkout path may exceed the Unix socket path limit and is rejected explicitly.

In a restricted executor, Docker socket access, process inspection, local listening and Virtualization.framework launch require the supported scoped approval route (`require_escalated`). The runner uses a local ad-hoc signature with `com.apple.security.virtualization`; no persistent signing credential or system security setting is changed.

## Same session boundary

| Operation | CLI / route | Behavior |
|---|---|---|
| Create/start/status | `microvm.py create/start/status ID` | One VZ runner, kernel boot and writable disk per session |
| Exec | `exec ID -- ARGV...` or RPC `op:exec` | Guest uid1000, `/workspace`; shell only if explicitly supplied as argv |
| Exec timeout | RPC `{"op":"exec","argv":[...],"timeout":2}` | Default20s, maximum30s; timeout kills the guest process group |
| Files | `read/write` or RPC `op:read/write` | Guest-relative `/workspace` path,1MiB per file, traversal rejected |
| Capture | `capture` or adapter `/screenshot` | Fresh X11 PNG, max1920×1080/4MiB server bounds; CLI requests1280×720 |
| Viewer/input/ownership | Session viewer/API URLs | Reused noVNC lease/input gate; real keyboard/pointer events reach the guest |
| Stop | `stop ID` | Guest `sync`, then VZ power-off; not application save or suspend |
| Crash | `crash ID` | Deliberate SIGKILL of verified task runner only; unsynced data may be lost |
| Destroy/reset | `destroy ID`, then `create` | Refuses active session; removes only that named disposable session directory |

Host RPC uses a mode0600 Unix socket under mode0700 `.state`. The host fixes each TCP connection's destination to guest loopback6080(viewer) or8080(adapter), over virtio-vsock port8000. HTTP callers cannot select a different guest target or invoke guest exec. Exec/files use the private Unix channel. Guest agent PID1 is root, while desktop and exec run as uid1000.

No virtual NIC, virtiofs/9p share, host home, Docker/containerd socket, USB or GPU is attached. There is no guest network egress. This paragraph describes the original noVNC profile and its fixed TCP bridge. The separate Neko relay and NAT profiles above now implement and verify media through their documented routes. No host networking/security setting was changed.

The original alternate-session capability reply incorrectly advertised viewer7903. That failure is preserved. A small metadata-only revision now passes assigned viewer/API ports through the VM boot command line into the guest environment; copied adapter discovery and local-Origin validation use those assigned values. Targeted independent A/qa-b discovery/decode/cleanup retest passed5 checks. Outer session manifests remain authoritative. The cached template refresh took60.1s; no accepted POC image/source was changed.

## Exact image and kernel origin

- Guest rootfs: Debian GNU/Linux12(bookworm), exported from existing accepted `sidekicks-novnc:local`, immutable image `sha256:64c4e5e66e745ee817c0baecf37e8f9084f4ba30c3d988f5c5089fb351c87b0c`, linux/arm64.
- Application stack observed in guest: Chromium156.0.8078.47, Xvfb21.1.7, Openbox3.6.1, xterm379, Python3.11.2, official-source x11vnc0.9.17 and noVNC1.6.0 from the accepted image.
- Local rootfs additions: Python PID1/vsock agent, guest pseudo-filesystem/loopback setup, additional loopback-Origin ports in a copied adapter. The accepted image/container files are not changed.
- Kernel: Linux6.12.28, taken from [official Kata3.17.0 ARM64 release archive](https://github.com/kata-containers/kata-containers/releases/tag/3.17.0). This uses a **Kata-distributed kernel asset only**, not Kata runtime or nested virtualization.
- Archive SHA256: `647c7612e6edf789d5e14698c48c99d8bac15ad139ffaa1c8bb7d229f748d181`.
- Kernel SHA256: `67bac9f416af4cdc9b151e4ba4962d6515e0ad7acc53816761cf964aa6af6ea0`.

Digests were locally recorded. The saved release metadata provides no asset digest; a detached maintainer signature/checksum was **not independently verified**. Kernel archive, source-image inspection and raw disk digest are preserved locally. The runner source is `Runner.swift`; exact compile/sign commands are in `build-runner.sh`. No package manager or host install is required for the fallback beyond the already-installed tools.

## Executed builder evidence

`artifacts/verification.json` records **21 PASS checks**, including:

- Two concurrently live guest boot IDs, independent disks and2vCPU/~2GiB visible in both guests.
- Non-root exec, guest exit17 propagation, file roundtrip, cross-session file absence and traversal rejection.
- No host filesystem/socket visibility and guest loopback as the only network interface.
- Actual decoded1280×720 RFB framebuffer, typed unique text reaching guest Chromium and fresh desktop capture over vsock.
- A real guest xterm launched and captured on that same desktop/kernel.
- VM A runner crash while B remains healthy with the same boot ID; A restarts with a new boot ID.
- **Explicitly synced** file persistence across crash, clean stop/warm restart and fresh-disk reset.
- Two disposable Docker comparison containers sharing one Docker Linux-kernel boot ID, unlike the two VZ sessions.
- Accepted noVNC/Neko/workbench container IDs/images/start times unchanged; existing demos healthy; additive host listeners loopback-only.

Run only while no user/QA owns the additive VM:

```bash
# Mutates only microvm sessions a/b and named disposable comparison containers.
# Leaves fresh session A running; clears B and comparison helpers on successful completion.
python3 pocs/microvm/verify.py
node pocs/microvm/viewer-smoke.mjs http://127.0.0.1:7903 vm-a
```

Independent QA owns `docs/microvm-verification.md` and `artifacts/independent-qa/microvm/`. Its live testing follows an explicit handoff; builder assertions are not independent acceptance.

### Startup observations—not a fair speed ranking

All image/kernel/download/export/ext4/Swift compilation was already cached/prepared. Timing starts immediately before spawning the runner(or starting the comparison container). Readiness boundaries are reported separately.

| Prepared-image observation | VZ first fresh disk(n=1) | VZ same-disk restarts(n=3) | Docker new container(n=1) | Docker restarts(n=3) |
|---|---:|---:|---:|---:|
| Guest agent |0.360s|0.344–0.350s|Not measured|Not measured|
| Desktop health |1.225s|0.925–0.965s|1.251s|1.119–1.161s|
| Synthetic guest fixture ready |2.451s|2.458–2.507s|2.083s|1.933–2.017s|

VZ fresh disk clone took0.0033s separately; it is an APFS clone of a cached template, not a download or factory-cold start. Warm here means reboot of a persistent disk, not memory snapshot restore. Docker uses a2CPU quota/2GiB cgroup within its shared VM; VZ has2 assigned vCPUs/2GiB per VM. Docker baseline stays `--network none`: health and capture are measured through `docker exec` into guest loopback, including exec/poll overhead. No Docker host viewer or input-latency comparison is claimed. These different probe paths and small sample counts do not establish a winner.

## Preserved failures and limits

- Initial guest `/dev` mount assumption caused a PID1 panic; corrected and console saved.
- Interrupted concurrent preparation/verifier cloned a207MiB partial disk while the final3GiB export was being copied. Final preparation publishes atomically and clone size is checked; old disk/logs retained.
- Crash recovery initially hit EADDRINUSE from TCP TIME_WAIT. Runner now uses **SO_REUSEADDR only**, not REUSEPORT; it still fails when another active listener owns a port. Failed run/log retained.
- A recently acknowledged **unsynced** file write was observed lost after SIGKILL. `write` is not an fsync/durability fence. The passing crash-persistence test explicitly calls guest `sync` first; normal stop syncs too. This failure is not relabeled PASS.
- Guest Chromium uses `--no-sandbox` in the reused disposable image; the VM is an added boundary, not proof of hardened browser isolation or adversarial escape resistance.
- Root PID1 agent and local bridge have no production multi-tenant/concurrency hardening. Exec output is clipped after `communicate`; that does **not** bound its transient buffering. Do not run hostile/unbounded output as a security test on this POC.
- Apple CLI/Containerization current release blocked by host OS/toolchain; Neko UDP, snapshots, GPU, network egress and cloud/Kata/GKE/EKS validation are unsupported/not tested. No resource-exhaustion/adversarial escape tests or performance benchmark claims.

## Safe cleanup

Use only session IDs you created. QA owns `qa-b` during its exclusive window and cleans it itself. No command targets unrelated VMs/containers.

```bash
# Stop the live additive demo only (sync guest then power off).
python3 pocs/microvm/microvm.py stop a
# Optional removal of this disposable session disk after it is stopped.
python3 pocs/microvm/microvm.py destroy a
# Explicit crash only if its agent is unresponsive; exact runner PID/path is checked.
python3 pocs/microvm/microvm.py crash a
```

Successful verification removes `sidekicks-microvm-baseline`, `sidekicks-microvm-peer` and session B. If a failed attempt leaves one, inspect its exact name/state before stopping/removing it; do not use Docker prune, killall or broad process-prefix kills. Recovered truncated state is retained under `.state/recovered-a-truncated`; source/kernel/template cache remains under `.build` for inspection/reuse. No autostart service was installed. Host sleep/logout can interrupt the runner; use the documented start command after checking status.

Independent core run now records6 viewer and12 boundary/lifecycle PASS. A fast immediate click/Control+A/type sequence appended to existing text; paced input passes, cause remains unestablished. This failed attempt and the alternate-session viewer URL metadata failure remain documented. Do not infer fast-shortcut reliability or final parent acceptance from the builder21/21 result.

The metadata-only follow-up now has5 independent PASS checks for discovery, Origin, fresh RFB decode at both assigned URLs and cleanup. The earlier18 core independent checks and21 builder checks are separately preserved; the full suite was not rerun after the small metadata change. All live work is released/frozen. The separate review report is saved to Library; QA images remain repo-local evidence.

Review delivery is complete: the written combined review/independent report is saved as a separate new Library item. Exact identity/version are in artifacts/library-deliverables.json. Screenshots were not uploaded; the actual local QA native-terminal image remains artifacts/independent-qa/microvm/native-terminal.png in the project root and was personally inspected. Parent review is pending, with the fast immediate shortcut issue still visible.
