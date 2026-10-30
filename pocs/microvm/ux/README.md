# Shared desktop revision

This directory prepares the shared Sidekicks cyan desktop for the three existing
Virtualization.framework profiles. The old image, template and test reports are
historical evidence. New UX acceptance is recorded separately in
[`docs/ux-verification.md`](../../../docs/ux-verification.md).

The retained Openbox/tint2 desktop uses picom software XRender for real dock
transparency and PCManFM for files. Browsers are normal windows, with real window
manager controls. Chromium and Firefox remain different applications. `/home` is
the default launcher; `/fixture` is a separate deliberate diagnostics page.

## Prepare and apply

Use exact final Docker image IDs after the builders release their tested images:

```bash
bash pocs/microvm/ux/prepare.sh sha256:EXACT_NOVNC_UX_IMAGE sha256:EXACT_NEKO_UX_IMAGE
# Only during the integration-owned VM lifecycle window, after preparation:
bash pocs/microvm/ux/apply.sh --apply
```

Preparation runs capped, no-network, no-host-mount helper containers. It exports
the new root filesystems and creates separate ext4 templates under `.build/ux1`.
The Neko image receives the same previously verified loopback-candidate binary
from its pinned image ID, without rebuilding Go or changing its media software.
The existing cached Linux kernel is reused. No running guest changes during
preparation. Build and startup artifacts are under `pocs/microvm/artifacts/ux`.

Apply is intentionally one-time and refuses an existing backup. It preserves
the previous templates/runners under `.build/pre-cloud-ux`, normally stops each
exact guest (including guest `sync`), and retains its old writable disk under
`.state/a-pre-ux`, `.state/neko-pre-ux`, or `.state/nat-pre-ux`. It then creates
fresh `a`, `neko`, and `nat` sessions at their existing endpoints. These are
disposable POC guest disks; no host home or personal browser session is involved.
Inspect an interrupted operation before resuming: do not rerun blindly or delete
the preserved state. The backup is recovery evidence, not a second active demo.

| Session | Viewer / API | Networking retained |
|---|---|---|
| `a` | 7903 / 7793 | No NIC; fixed TCP over vsock |
| `neko` | 7906 / 7796 | No NIC; loopback UDP59120–59139 over framed reliable vsock |
| `nat` | 7907 / 7797 | Apple basic NAT; direct assigned-guest-IP UDP59220–59239 |

All retain2vCPU/2GiB. NAT remains egress-enabled with possible host/LAN
reachability, not an egress isolation policy. No firewall, routing, VPN, OS
security setting, entitlement or credential change is part of this revision.

## Readiness and launch boundary

`microvm.py start` now waits for `/desktop/state`, after the WM, compositor, dock
and first normal browser client map. Closing the browser later leaves a healthy
desktop. The old fixture-only readiness path remains for historical images;
startup fields identify which readiness endpoint was used. These startup times
are not decoded-viewer latency measurements.

Host-forwarded TCP connections use guest loopback source127.0.0.2. Internal
`/home` page capability and fixed Browser/Terminal/Files launch requests require
exact127.0.0.1, their internal Host/Origin, the perboot page capability and active
control. Forged Host/Origin headers from published endpoints must not borrow
the current owner's launch authority. Private exec/files RPC remains a trusted
task-local channel, separate from the published viewer/API. Trusted guest exec can invoke X11 tools directly and therefore bypass the viewer/input lease gate; this POC does not claim a global GUI security boundary against code already running as the desktop user. An orchestrator must coordinate such exec actions as well.

## Normal lifecycle and safe stop

```bash
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py status neko
python3 pocs/microvm/microvm.py status nat
python3 pocs/microvm/microvm.py stop nat
python3 pocs/microvm/microvm.py stop neko
python3 pocs/microvm/microvm.py stop a
```

`start SESSION` restarts that preserved current guest. Stop verifies the exact
runner and stops only its relay where applicable; it does not touch Docker or
other projects. No broad kill, prune, reset or destroy is needed. Old performance
measurements remain explicitly pre-UX because normal windowed browsers and the
compositor change the workload. Native physical Mac trackpad and reserved-key
behavior still require physical testing.


## noVNC live-content drag follow-up

The current noVNC image adds only `-nowireframe` to x11vnc startup. Openbox
already drew window contents; x11vnc's default remote wireframe optimization
caused the visible outline. The current Docker image is
`sha256:0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7`.
VM A uses the same startup file and a noVNC-only template made by
`bash pocs/microvm/ux/prepare-novnc.sh EXACT_IMAGE_ID` (preparation alone does
not change live guests). The existing runner, kernel and Neko profiles were
not changed. Current source/template startup persists through normal restart.

The old VM A state is preserved under `.state/a-pre-drag`; its previous
template is under `.build/pre-live-drag`. The prior Docker container is stopped
as `sidekicks-novnc-pre-live-drag`; the core UX image remains tagged
`sidekicks-novnc:cloud-ux`. Normal start/status/stop commands above are unchanged.
Focused post-fix and post-restart held-drag checks passed on both targets,
with zero outline samples; a few intermediate native/stream captures were not
tightly synchronized. This is not a loss-free rendering or new latency claim.
Original full-core UX evidence remains unchanged; the narrow addendum is
`artifacts/independent-qa/ux-live-drag/report.md`.
