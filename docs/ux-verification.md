# Independent desktop UX verification

Completed 2026-10-26, final read-only audit at 14:05:19 EDT (18:05:19 UTC). **All five desktops are usable under the scoped checks, with the shortcut/blur limitations below.** Eight human input paths and workbench were covered. No implementation files, images or host settings were changed by QA. No new benchmark was run.

| Variant | Viewer | Real app/window sequence | Cursor, guest F11, viewer Escape | App switching | Launcher boundary | Normal restart |
|---|---|---|---|---|---|---|
| Docker noVNC | [http://127.0.0.1:7901/](http://127.0.0.1:7901/) | PASS (rfb) | PASS on each route | Visible Next app control | 19 PASS | 6 PASS |
| Docker Neko | [http://127.0.0.1:7902/](http://127.0.0.1:7902/) | PASS (gated) | PASS on each route | Gated keyboard PASS; native SDK EMULATED | 19 PASS | 6 PASS |
| noVNC VM A | [http://127.0.0.1:7903/](http://127.0.0.1:7903/) | PASS (rfb) | PASS on each route | Visible Next app control | 19 PASS | 5 PASS |
| Relay Neko VM | [http://127.0.0.1:7906/](http://127.0.0.1:7906/) | PASS (gated) | PASS on each route | Gated keyboard PASS; native SDK EMULATED | 19 PASS | 5 PASS |
| NAT Neko VM | [http://127.0.0.1:7907/](http://127.0.0.1:7907/) | PASS (gated) | PASS on each route | Gated keyboard PASS; native SDK EMULATED | 19 PASS | 5 PASS |

Each desktop's app sequence used its actual viewer to launch Browser/Terminal/Files through Home and dock; minimize/restore twice with the SAME client/PID/prior geometry; prove browser tab behavior using Ctrl+W versus Ctrl+T/Ctrl+W; maximize/restore; observe a real closed-client interval then explicit reopen; drag/resize; type an exact terminal `printf` proof and read/remove only its task-owned guest file; navigate the separate `/fixture`, type/click a fresh marker, capture it, then restore `/home`. Read-only X11/EWMH/XFixes observations independently checked actual windows, focus, geometry, keys/buttons and cursor hotspot. Matching source alone was not a PASS.

For Neko VMs, full app lifecycle ran through **gated** input; native mode separately exercised actual dock/focus/terminal close, cursor/scaled mapping, guest F11 and viewer fullscreen held-input release. Native's first seven summary entries explicitly reference the same VM's gated desktop evidence. Docker Neko also has independently executed native app-core evidence. Resumed attempts retain source references; they are not fresh reruns of skipped checks. XID reuse after a closed interval was allowed; minimized restoration still required identical client/PID/geometry.

Both noVNC variants and all three Neko variants proved actual Shift plus left-button holds BEFORE real viewer Escape, then empty actual X11 input AFTER Escape before local key/button cleanup. RFB/gated modes also proved mixed normalized-API-held input with viewer logical mouse mask up, followed by actual viewer Exit and empty X11 state. Single cursor evidence combines decoded-frame crops, CSS/overlay state, XFixes hotspot and scaled coordinate observations (≤2px assertion), rather than claiming a screenshot alone shows the host pointer. Final images were personally visually inspected: shared cyan wallpaper, normal browser chrome and visible desktop margins, Home and floating dock.

Relay Neko VM selected UDP `127.0.0.1:59134`, VP8 1280×720; decoded frames increased 5→1461 during its functional checks.

NAT Neko VM selected UDP `192.168.64.4:59237`, VP8 1280×720; decoded frames increased 4→1444 during its functional checks.

## Limits and preserved failures

- **EMULATED:** native Neko Linux Alt+Tab was composed directly through the official SDK. This proves Linux/SDK switching, not ordinary native browser keyboard UX. Native viewers have no Next app button. **UNTESTED:** ordinary native Mac reserved shortcuts and physical keyboard/trackpad gestures. noVNC switching passed using the actual human-facing **Window → Next app** control; gated Neko used ordinary synthetic browser Alt+Tab events.
- **UNTESTED:** real tab-blur behavior. Activating another task-owned headless tab did not deactivate the original document. A separately labeled **EMULATED** window-blur event cleared proven actual held input. This handler proof is not passed off as a physical tab-switch test.
- A noVNC titlebar-only keyboard sequence did not act on Chromium; actual content click before typing resolved it. One VM A rapid click→type attempt lost the first `u`; a 120ms click-settling wait then produced the exact text. These observations remain preserved; no loss-free immediate-input claim is made.
- Earlier attempts retain clipboard-helper failure, uppercase proof-path/native Shift mismatch, titlebar-border/button classification, covered focus/Close hit points, asynchronous dock focus and fullscreen frontend readiness, and an overly strict QA rejection of a valid underscore-prefixed random capability. Original timeouts were not erased. Fullscreen failure before a proven hold was not mislabeled an Escape-release defect. No backend rebuild was required.
- Headless Chrome limits remain explicit. There is no generic application-event queue flush, physical zero-latency handoff, new audio proof, or post-UX performance claim. Historical measurements describe earlier workloads/images.

## Workbench and safety

Workbench [7790](http://127.0.0.1:7790/) passed **8 checks** after its normal restart: current cyan UI, all five correct runtime links, live API versus historical pre-UX evidence labels, non-executing Scout/Builder/Reviewer intent records, Host/Origin/bearer rejection, absent host-shell route, bounds/unknown-adapter rejection, and a separately labeled direct-browser fixture check. Its two adapter-control cards remain Docker adapters; the five runtime links open the individual viewers. No autonomous agent/model execution is claimed.

All five launcher boundary runs passed 19 checks each (**95 total**): a valid internal page capability was retained only in memory; forged internal Host/Origin/X-Forwarded-For requests through both host API/viewer ports were rejected across unowned/current-agent/current-human phases without borrowing the current lease. Actual guest TCP witnesses showed 127.0.0.2 for VM forwards and a non-internal Docker peer. Valid Home/dock launches were proved separately through viewer actions.

This remains a local disposable POC boundary, not production tenant hardening. Private guest RPC is privileged by design; Chromium's guest `--no-sandbox` flag is not a host isolation guarantee. VM A/relay have no NIC/shared directories; NAT is internet-enabled and not egress isolation. No fallback unrestricted host shell was used. Each desktop normal restart preserved its image/profile and checked unchanged neighbor identities/boots; the workbench restart did likewise.

Final read-only audit: **13 PASS**. All five targets had one normal Home browser, no Terminal/Files, ownernull and actual keys/buttons empty. Every guest had a real compositor selection owner/picom process and identical Home SHA 45899f0abc84afd59fec79aa7d3b149202965f32a1052f87dcad33002b1cc14a. All 11 expected web/API listeners and 20 relay UDP listeners were actually loopback-bound. All 13 historical report/raw-file hashes matched preparation. QA viewers were closed; the final audit required no active QA lock. **Live input/restart windows released; no further runtime work planned.**

## Reproduction and evidence

Repository root is `/Users/kevinmartin/Development/kevinmartin/sidekicks`. Run only with an exclusive target handoff and preserve an existing controller/user activity. Scripts fail rather than preempt an unknown owner. Example (each invocation writes a timestamped attempt):

```sh
node artifacts/independent-qa/ux/launch-boundary.mjs --target vm-novnc --handoff
python3 artifacts/independent-qa/ux/restart.py --target vm-novnc --handoff
node artifacts/independent-qa/ux/ux-viewer.mjs --target vm-novnc --mode rfb --handoff
```

Other fixed target IDs are `docker-novnc`, `docker-neko`, `vm-relay-neko`, `vm-nat-neko`; Neko routes are `gated`/`native`. The machine summary `artifacts/independent-qa/ux/summary.json` resolves every retained core/route reference, lists exact boundary/restart counts, runtime/final-audit evidence, preserved FAILs, media facts and SHA256 provenance. Immutable attempt folders contain actual viewer/desktop images, fixture/terminal proof, source snapshots and traces. Screenshots remain local; no new Library images were uploaded by QA. Original failed evidence and earlier reports/raw samples remain unchanged.
