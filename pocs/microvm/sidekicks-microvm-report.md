# Sidekicks desktop UX closeout

Completed October26,2026. All five desktops and the workbench completed independent scoped acceptance. On Kevin's Mac, open **http://127.0.0.1:7790/** for all five runtime links. The shared desktop now has original cyan/blue wallpaper, a light Sidekicks Home, a real translucent floating Browser/Terminal/Files dock, and normal browser windows with real controls. Neko's encoded pointer was removed in favor of hotspot-aware viewer cursors; its previously inert window buttons and kiosk startup were corrected. noVNC fullscreen and mixed-input release defects were corrected before the final images were frozen.

| Current desktop | Viewer | Core desktop UX | Boundary | Normal restart |
|---|---|---|---|---|
| Docker noVNC | http://127.0.0.1:7901/ | PASS scoped |19PASS|6PASS|
| Docker Neko | http://127.0.0.1:7902/ | PASS scoped, both routes |19PASS|6PASS|
| noVNC VM | http://127.0.0.1:7903/ | PASS scoped |19PASS|5PASS|
| Relay Neko VM | http://127.0.0.1:7906/ | PASS scoped, gated core + native route |19PASS|5PASS|
| NAT Neko VM | http://127.0.0.1:7907/ | PASS scoped, gated core + native route |19PASS|5PASS|

Eight human input paths were covered. Independent totals:95 launcher-boundary checks;27 desktop restart subchecks;8 workbench checks;13 final-audit checks. These categories are not a fabricated all-purpose UI-test count. Core app lifecycle was executed once per VM; route-specific native evidence references that same VM's gated core where stated. Resumed attempts preserve references instead of pretending skipped checks ran again.16 failed attempts remain in the evidence tree with their diagnoses.

Verified interactions include same-client/PID/geometry dock restore without extra browser tabs; real Browser/Terminal/Files launch, minimize, maximize, close and reopen; drag/resize and focus; fresh fixture input then Home restoration; single-pointer presentation and scaled hotspots; distinct guest F11 and viewer fullscreen; actual held Shift+button before Escape and empty X11 state afterward; and mixed API-held input released by the viewer Exit control. Actual selected UDP decoded media remains working in both VM Neko variants. The final audit found one Home browser per desktop, no Terminal/Files, no owner or actual held inputs, real compositors, identical Home assets and actual loopback server/relay listeners. The integration lead personally inspected the final images from all five desktops.

**Limits:** native Neko Alt+Tab is EMULATED through the SDK; ordinary native Mac reserved shortcuts and physical gestures are UNTESTED. noVNC app switching was verified through its visible Window→Next app control. Real headless tab deactivation could not be established; explicit blur-handler emulation released actual input but does not prove physical tab switching. Focus/content timing nuances and a dropped first character during rapid VM click→type remain documented. No generic application-queue flush or post-UX latency/performance claim is made. Chromium/Firefox differences remain visible. This is a disposable local POC, with no model/API integration; trusted private guest exec can bypass desktop input arbitration and is not an untrusted public shell.

Home is guest /home; synthetic diagnostics remain separate at /fixture. Take control inside the viewer before interacting. Normal stop commands are `bash scripts/stop.sh` for Docker demos, plus `python3 pocs/microvm/microvm.py stop nat`, `stop neko`, and `stop a` for the three current VMs. Existing Docker Desktop, Go/Node/Python and the task-local signed VZ runner are the dependencies; no new host security/network setting or permanent daemon was installed. Source/runtime profiles and old guest disks/images were preserved. Apple container CLI remains prerequisite-blocked on this macOS15.3 host; Kata/cloud provisioning was not attempted. Direct VZ no-NIC/relay/NAT demos are working.

Repository evidence: docs/ux-verification.md; artifacts/independent-qa/ux/{freeze,summary}.json; pocs/microvm/artifacts/ux/{preparation,deployment-state,stream-smoke,final-report-review}.json; shared/desktop; pocs/microvm/ux/README.md. Historical raw files/reports below remain unchanged and describe earlier images/workloads. The user-requested screenshot uses Library identity libfile_65d0def3be588191b4121a4a25f88068. Its representative final QA image is the live Docker Neko desktop saved13:38:11EDT at artifacts/ux/sidekicks-desktop-neko.jpg; the earlier12:53:27EDT preview is historical. No additional screenshot identity is created.

## Current noVNC live-content dragging follow-up

The later user-observed outline behavior was confirmed in the streamed view. It came from x11vnc's default wireframe optimization, while Openbox already drew actual contents. The only startup change is `-nowireframe`. Current Docker noVNC and VM A use image sha256:0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7 and entrypoint SHA256803129717c7467d54ddbdc1a8ccf17ddceef3e5d1523f37f6c2894386045ae8a. The prior Docker container/core image and VM A state/template remain preserved; Neko, workbench, runner and kernel were not changed.

Focused independent titlebar-drag verification passed on both current noVNC targets before and after normal restart. It measured intermediate decoded content while the mouse remained held, with zero old-position outline matches in all fixed runs.27/29 eligible captures met the tight synchronized-content criterion; two non-tight intermediates and partial repaint remain documented. This proves live contents, not Neko-equivalent smoothness, atomic rendering or a new performance result. The original broader core UX report below remains unchanged; only the affected drag/restart behavior was rerun after this one-flag change. Normal run/status/stop commands and public URLs are unchanged.

### Frozen focused independent addendum

# noVNC live-content dragging — independent addendum

**PASS: Docker noVNC 7901 and noVNC VM A 7903 now show moving window contents while the mouse remains held. The behavior persisted after a normal restart of each target.** These were focused titlebar-drag checks, not a repeat of the broader UX suite.

The independent VM A baseline exposed the distinction: Linux geometry and direct X11 captures showed an opaque moving browser, while seven decoded RFB frames kept the old content position and drew an outline. The original broader UX geometry checks did not establish intermediate streamed-content behavior. Docker's before-fix check was builder-owned; this worker independently verified both fixed targets.

The owners' single startup change disables x11vnc wireframing. Independent process reads recorded **`-nowireframe` in every fixed run, including after both restarts**. Docker used image `sha256:0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7`, container `d5905223026615a33239a263ec6abb6fa17d0c2ce4b1b68b629339c62b356de3`. VM A's final observed boot was `51e94113-7937-497f-93c0-4bd079c96962`.

| Target | Phase | Eligible moved/held captures | Tight live-content matches | Old-position outline matches | Result |
|---|---|---:|---:|---:|---|
| VM A | Independent pre-fix baseline | 7 | 0 | 7 | OUTLINE confirmed |
| Docker noVNC | Fixed image | 7 | 7 | 0 | LIVE / PASS |
| Docker noVNC | After normal restart | 7 | 6 | 0 | LIVE / PASS |
| VM A | Fixed image | 8 | 7 | 0 | LIVE / PASS |
| VM A | After normal restart | 7 | 7 | 0 | LIVE / PASS |

The same parameterized test dragged the actual native titlebar through a task-owned RFB viewer. It concurrently saved decoded RFB PNGs, independent X11 geometry/button state and direct guest PNGs. Pixel matching used distinctive **interior Home content**, excluding the frame/outline and clock. All analyzed held captures finished before the recorded mouse-up and showed actual X11 button1 held. Multiple translated content positions were required; final geometry alone could not pass. The before/after images were personally visually inspected.

Across the four fixed runs, 27 of 29 eligible held captures met the tight decoded/native position-and-pixel match; none matched an old-position outline. Two non-tight intermediates remain in the evidence. Capture pairs are asynchronous, and a retained intermediate shows partial repaint: **this proves live contents, not atomic/tear-free frames, loss-free updates, physical mouse behavior or a latency/frame-rate result**. After release, all four runs had matching native/RFB content translation (-51,+14), with decoded-template mean RGB error about 0.40 on the 0–255 scale.

Normal restart checks passed **Docker 6 / VM 5**; canonical neighboring container identities/start times and VM boots were unchanged across each restart. Each run restored the same client/PID and original window position, released its own lease, verified ownernull and actual keys/buttons empty, and closed its own headless browser. Both live windows are **RELEASED**, with no further runtime actions planned. No Neko or host configuration changes were made by QA.

One baseline tooling failure is deliberately preserved: the first pixel matcher assumed diagonal movement, but Openbox workarea snapping kept vertical displacement zero. Its raw result remains FAIL/INCONCLUSIVE. `vm-novnc-observe-2026-10-26T18-20-18-483Z/analysis-2d.json` corrected this using only preserved PNGs and a bounded 2D search, confirming 7/7 outline captures; it did not rerun or mutate the guest. The four fixed runs used that corrected matcher.

Exact raw paths, counts, process argv, final geometry/cleanup, runtime identities and SHA256 hashes are in `summary.json`. Every timestamped attempt retains PNGs, X11 observations, input/capture timing brackets and source snapshots. Reproduce only after exclusive target handoff:

```sh
node artifacts/independent-qa/ux-live-drag/live-drag.mjs --target docker-novnc --handoff --expect live
python3 artifacts/independent-qa/ux-live-drag/restart.py --target docker-novnc --handoff
node artifacts/independent-qa/ux-live-drag/live-drag.mjs --target docker-novnc --handoff --expect live
```

Use `--target vm-novnc` for VM A. Screenshots remain local. This addendum makes no new independent live-content resize claim or unaffected full-UX regression claim. The original `docs/ux-verification.md` remains byte-identical at SHA256 `fd7ff6a83336a007e66cd602b544a028000684248480040a6f6d2f99f1b66495`; its existing shortcut/blur limitations remain in force.


## Frozen independent UX report

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


## Historical microVM, relay and NAT report (preserved pre-UX version2)

# Sidekicks microVM report — NAT desktop and reviewed relay comparison

**The separate NAT POC is working and independently verified.** Public DNS/certificate-verified HTTPS, actual guest-browser public rendering, direct guest-IP UDP WebRTC, native/gated input, capture and normal restart passed. The first matched local comparison retained120/120 valid observations with no discarded outliers or timeouts. This establishes a useful working alternative, not a causal performance ranking.

Live NAT viewer: http://127.0.0.1:7907/ · native input: http://127.0.0.1:7907/?mode=native · API: http://127.0.0.1:7797 . Assigned guest IPv4 is reported through vsock (currently192.168.64.4), direct UDP59220–59239. Relay remains7906/7796 with no NIC and bounded reliable-vsock media. Baseline noVNC VM and accepted Docker demos remain unchanged. Both comparison guests are left running, unowned, with no held input or load workers.

NAT is egress-enabled and may reach host/LAN services; it is not an egress isolation policy. No private-network scan, user account, host firewall/routing/VPN/security change, paid resource or new persistent credential was used. No new OS approval or unlock was required for the demonstrated operations. Screenshots remain local and were personally inspected; only this written report is saved to Library.

The control medians were92.00ms relay /89.55ms NAT, with NAT's control p95 higher. Under guest CPU load the observed medians were141.00ms relay /116.85ms NAT. The2.45ms control-median difference is smaller than the16.6ms observation interval. Fixed order,30samples/block, short windows and cache/age/process-set differences preclude declaring NAT generally faster or more memory-efficient. Detailed raw/correctness/resource evidence and limitations follow.

This revision updates the same microVM Library identity and preserves its earlier report versions. The original noVNC and relay reports are included as historical evidence, with their original failures and scope unchanged.

## NAT implementation and builder review

# Apple NAT versus relay: Sidekicks microVM POC

**Functional NAT and the independent local comparison are complete.** The first fixed-order run retained120/120 valid observations. Builder arithmetic/integrity review independently matched every median, nearest-rank p95, CPU delta and interval jitter-buffer calculation; no performance superiority is established from this single run.

The opt-in session `nat` uses basic Apple `VZNATNetworkDeviceAttachment`, one virtio NIC, guest DHCP/routes/DNS, and the same 2 vCPU / 2 GiB / 4 GiB Neko desktop software as the existing relay guest. Host viewer/signaling/control stay at http://127.0.0.1:7907/ and API7797 over the private vsock channel. Media goes directly from the task browser to the guest's assigned private IPv4, ports59220–59239; there is no host UDP relay for this guest. The assigned address is returned through vsock info and refreshed each boot, not assumed from a subnet or ARP table.

The builder observed actual selected ICE UDP192.168.64.4:59231 and decoded1280×720 video. Nine end-to-end checks passed, separately exercising native SDK data-channel and gated HTTP input, normalized automation, human takeover/rejection and fresh bounded capture. Five lifecycle checks passed, including NAT-only stop/restart, repeatable guest setup, fresh direct media/capture/internet and unchanged protected A/relay boots and Docker state. These are builder results, separate from independent QA below.

The guest resolved `example.com` and `www.iana.org` to public IPv4 addresses and returned HTTPS200 with normal certificate verification, bounded response bodies and an explicit benign redirect allowlist. No user accounts, cookies or private-service requests were involved. NAT enables outbound network connectivity; it is not an egress allowlist or proof of host/LAN isolation. Tests do not scan private networks. No host firewall, routing, VPN, security setting, persistent credential or paid resource was changed. Framework-managed NAT resources are distinct from manually editing host networking. No new OS permission prompt or restricted networking entitlement was needed on this Mac.

## Why the design changed

The earlier no-NIC/vsock choice was conservative scope, not a tested rejection of NAT or measured performance superiority. Reliable stream framing changes UDP ordering/loss semantics and can add head-of-line delay. Basic Apple NAT is available on this macOS15.3 host; it does not require the restricted bridged-network entitlement. [Apple NAT API](https://developer.apple.com/documentation/virtualization/vznatnetworkdeviceattachment), [Apple DTS entitlement clarification](https://developer.apple.com/forums/thread/710763). This is direct Virtualization.framework, not Apple container CLI, Containerization, Kata or cloud validation.

## Preserved implementation failure

The first NAT VM started successfully, but guest networkd waited for udev initialization omitted by the custom PID1. DHCP timed out and PID1 exited, causing a guest panic. The stopped attempt and console are preserved under `.state/nat-attempt1` and `artifacts/nat/attempt1-network-console.log`. Guest-only udev net initialization repairs the startup; subsequent setup errors are reported through the agent instead of killing PID1. Per-boot config validates and replaces the previous IPv4 entry, rather than requiring the original loopback value. Reboot succeeded; the observed DHCP address stayed the same, so changed-address behavior is supported by the validated generation path rather than claimed from an observed address change.

## Matched comparison protocol

Fresh relay and NAT observations use the same pinned Neko binary, Firefox/fixture, VP8 interactive encoder, desktop1280×720@30, resources and task-owned Chrome native SDK input route. The required network/init/candidate differences are recorded. Existing VMs remain up idle as a shared background; viewers and guest load run sequentially. The order is relay-control, NAT-control, NAT-load, relay-load. There are at least30 measured attempts per block, declared warmup, an8-second passive media/resource window, and one bounded guest-only CPU worker for the load condition. No WAN impairments are manufactured.

Latency begins at native-overlay mousedown and ends at the first observed decoded change in the same synchronous click-counter region, sampled using the same browser performance.now clock and rAF. Counter increments are checked separately. All samples, failures/timeouts, ROIs and sampling intervals are retained. Standard median averages the middle pair; p95 uses nearest rank. This is local input-to-observed-frame latency, not ping, screenshot HTTP time, physical human display latency or isolated transport causality.

RTP jitter, interval jitter-buffer delay, packet loss, native freeze counters, rVFC gaps and observer rAF gaps remain separate. Missing fields are unavailable; negative loss deltas are retained. Host CPU uses cumulative time deltas for the exact VM XPC process identified through its open disk, plus separate runner/relay/task-browser components. RSS is a process observation, not unique physical RAM or configured guest capacity. Short windows and fixed ordering limit inference.

## Reproduce / stop only this POC

```bash
bash pocs/microvm/prepare-nat.sh
python3 pocs/microvm/microvm.py create nat 7907 7797 neko-nat # if absent
python3 pocs/microvm/microvm.py start nat # if stopped
python3 pocs/microvm/microvm.py rpc nat '{"op":"info"}'
node pocs/microvm/nat/e2e.mjs
python3 pocs/microvm/nat/internet.py
python3 pocs/microvm/nat/lifecycle.py # exclusive window, NAT-only restart
python3 pocs/microvm/microvm.py stop nat
python3 pocs/microvm/microvm.py destroy nat # optional, stopped only
```

NAT direct UDP avoids the relay's stream framing, but no superiority is inferred in advance. Application input queues are not generically flushed by takeover. Browser/PID1/concurrency/output-buffer hardening, physical trackpad/audio listening, WAN behavior and production network policy remain outside the demonstrated scope. Screenshots stay repo-local; only the existing written microVM Library report is updated after verification.


## Reviewed local observations

One run, fixed order relay-control → NAT-control → NAT-load → relay-load,30 attempts each, all valid; zero timeouts/invalid attempts. Each active load block kept its exact bounded guest CPU worker alive through the final sample and stopped it afterward. Standard median averages the middle pair and p95 uses nearest rank. Each observation separately confirmed exactly one fixture-counter increment. The timed endpoint equals the summed browser polling intervals.

| Window | Valid/attempts | Median ms | p95 ms | Max ms |
|---|---:|---:|---:|---:|
| relay-control |30/30|92.00|94.6|102.4|
| nat-control |30/30|89.55|127.0|127.8|
| nat-load |30/30|116.85|212.2|269.1|
| relay-load |30/30|141.00|271.8|340.7|

Typical latency observation interval was16.6ms for every block. Control medians differ by only2.45ms, smaller than that interval, and NAT's control p95 is higher. NAT's loaded median/p95 are lower in this run; fixed order, one run,30 observations/block, VM age, warm caches, host scheduling and whole-pipeline differences prevent a causal transport or general performance conclusion. No WAN/loss impairment was introduced. This does not measure the magnitude of head-of-line effects under packet loss.

Media/resource windows were about8seconds each, separate from timed clicks:

| Window | RTP jitter median/p95 ms | Interval jitter-buffer mean ms | Reported packet-loss delta | Native freeze delta | Max observer frame gap ms |
|---|---:|---:|---:|---:|---:|
| relay-control |3.0 / 16.0|20.45|0|0|83.4|
| nat-control |2.0 / 8.0|14.25|0|0|83.4|
| nat-load |8.0 / 27.0|21.42|0|0|183.3|
| relay-load |18.0 / 24.0|38.76|0|0|166.6|

All windows reported zero additional decoder dropped frames and no observer video gaps≥250ms. Short-window zero counts are observations, not a guarantee of no rare freezes/loss. RTP jitter, browser buffering, native freezes and observer gaps have different meanings; the longest observer gap was actually larger for NAT-load than relay-load. All raw counters, timestamps, SSRC and fields are retained. [W3C metric definitions](https://www.w3.org/TR/webrtc-stats/).

| Window | VM XPC core-% | Runner core-% | Relay core-% | Available Chrome component core-% sum | VM XPC median RSS MiB |
|---|---:|---:|---:|---:|---:|
| relay-control |54.26|0.38|1.00|18.80 (incomplete)|1098.4|
| nat-control |41.74|0.00|not applicable|11.50|645.8|
| nat-load |139.64|0.00|not applicable|11.09|531.5|
| relay-load |150.83|0.25|0.87|10.49|550.5|

100 core-% means one CPU core. The first relay-control Chrome initial-component sum is incomplete because a late Unzipper utility process was not in its initial set; the other three observed sets stayed stable. These values must not be treated as whole-browser totals when the process set changes. The actual VM XPC helpers were identified by exact open guest-disk paths; CPU is cumulative-time difference divided by measured elapsed time. Chrome components use task-owned process counters, separately retained. RSS can double-count shared pages and is not unique physical RAM or configured guest capacity. Relay-control RSS ranged~718–1272MiB and changed substantially during the window; older relay VM age and first-window browser warmup/cache state confound apparent efficiency differences. The relay Python process itself consumed only~1 core-% here; the larger whole-VM differences must not be attributed solely to it. This is a scoped observation, not proof that NAT saves memory or CPU.

Raw directory: `artifacts/independent-qa/microvm/nat/comparison-2026-10-26T14-44-44-063Z`. Builder recalculation: `pocs/microvm/artifacts/nat/statistics-review.json`; source/fact/counter integrity: `raw-integrity-review.json`. Raw JSON was not edited.

## Independent functional closeout and preserved helper failure

Independent functional9/9, NAT lifecycle5/5 and final-state9/9 checks PASS. Public DNS/TLSHTTPS and actual public-page rendering in guest Firefox were verified. The browser helper then attempted `/fixture/` rather than the actual `/fixture` path and timed out restoring the fixture; that failed attempt remains visible. A separate NAT-only restart successfully restored a fresh fixture, direct media/capture and internet. A passing restart does not erase the helper failure.

Both NAT and relay are left running, ownernull with all actual X11 keycodes/buttons empty. Exact CPU-worker PIDs are gone, the owned headless Chrome is closed, and the comparison lock is removed. Accepted container identities/images/start/ports and A/relay/NAT boot IDs stayed unchanged across measurement. No further runtime work is planned. Integration personally inspected the actual public webpage and native guest terminal images; screenshots remain local and are not uploaded. Parent review remains pending.

The independent NAT takeover diagnostic also retains text snapshot growth33→37 after a650ms observation wait while controller sends stayed313→313. Queued/stale suffixes did not execute. This is injection cutoff with a failed strict stable-text diagnostic, not generic application draining or a measured final delivery time. All known limitations remain visible. RSS values below are resident-process observations; compression is an interpretation confound, not an independently measured memory pool.


## Complete frozen independent NAT verification and comparison

Source: `artifacts/independent-qa/microvm/nat/report.md`, SHA256 `9c25ad5b55950b838d4fc0749711492c191d0fb5b71a9362c14a39fb35faf7a1`. Appended verbatim; its raw data remain unchanged.

# Independent NAT Neko microVM verification and relay comparison

Completed 2026-10-26 **10:48 EDT / 14:48 UTC**; live window released immediately afterward. Implementation remained frozen throughout independent execution. No accepted POC source/image or relay/A boot was changed. This report supplements the original microVM and UDP-relay reports, which remain unchanged.

**Result:** NAT provides a usable actual Linux desktop, direct guest-UDP WebRTC, native and gated input, fresh capture, public HTTPS and repeatable normal restart. The fresh four-window comparison completed **120/120 valid observations**. One browser-helper restore failure remains preserved. Application events already delivered before takeover can still appear afterward; no generic application-queue flush is claimed.

## Current endpoints and boundary

- NAT session `nat` / profile `neko-nat`: viewer **http://127.0.0.1:7907/**, native **http://127.0.0.1:7907/?mode=native**, API **http://127.0.0.1:7797**. Assigned guest `192.168.64.4`, direct guest UDP `59220–59239`.
- Relay session `neko`: viewer **http://127.0.0.1:7906/**, native **http://127.0.0.1:7906/?mode=native**, API **http://127.0.0.1:7796**, host-loopback UDP `59120–59139` carried over vsock.
- Both: Virtualization.framework, Linux `6.12.28`, 2 vCPU / 2 GiB / 4 GiB disk, actual Firefox desktop and software VP8 at 1280×720. No shared host directory or host-shell endpoint was added. Host API/viewer listeners remain loopback.
- NAT is **egress enabled, not egress isolation**. Its basic Apple NAT attachment can reach host/LAN services; those were not probed. No host firewall, routes, VPN, entitlement, OS settings or runtime installs were changed by QA. Relay retains its no-NIC configuration. Reliable ordered vsock alters loss/head-of-line behavior; it is not a transparent WAN UDP equivalent.

## Independently executed acceptance

| Area | Result | Evidence relative to this directory |
|---|---|---|
| Actual media and topology | PASS | `media-native-post-restart.json`: growing decoded/RTP counters, open native data channel, selected UDP remote equals assigned guest IP and expected port range. HTTP readiness alone was not accepted. |
| Native and gated viewer input | PASS, 9 functional checks overall | `functional.json`, `native-fixture.json`, `gated-fixture.json`: exact paced text/replacement, Ctrl/Shift, click/double/right click, drag/release and separate horizontal/vertical wheel effects. Native controller counter stayed 0→0, demonstrating the SDK data-channel path rather than HTTP input. |
| Lease enforcement and held input | PASS | Native mode rejects normalized/bot writes; raw unleased SDK writes have no effect; stale normalized requests return409; actual XQueryPointer/XQueryKeymap confirms released buttons and all keycodes. |
| Injection cutoff versus application settling | PASS cutoff; FAIL strict stable-text-at-ACK diagnostic | At `14:32:34.013Z`, controller sends stayed **313→313**; queued suffix/stale writes were absent. Text length was **33 at the first post-ACK snapshot →37 at the next snapshot after a650ms wait**. Four previously delivered characters settled afterward. These snapshots do not identify the precise last processing time. `applicationQueueFlushed:false`; human ownership is not rollback/drain. |
| Native Linux application | PASS | `native-terminal.png` plus independently read guest output: bounded native-viewer `printf` command produced its exact unique file contents. Test file removed. Image personally inspected locally. |
| Fresh independent capture | PASS | `fresh-capture.jpg` and `post-restart-capture.jpg`: decoded JPEG1280×720 agrees with the fresh decoded-viewer marker/color. Fresh post-restart capture103,554bytes. No model API or pretend model-loop proof. |
| Public internet | PASS | `internet-before-restart.json`, `internet.json`: example.com public DNS, pinned public destination104.20.23.154, certificate/hostname-verified TLS1.3, HTTP200, no redirects, bounded577byte response; repeated after restart. `internet-browser.png` visibly rendered Example Domain in guest Firefox through native input. No private-network requests. |
| Browser-helper restoration | FAIL, retained | `internet-browser.json`: public render passed, then helper used `/fixture/` and timed out restoring it. The actual copied route is `/fixture` without trailing slash. No rerun concealed this failure. Separate normal-restart evidence subsequently proves the startup fixture/media recovered. |
| NAT-only lifecycle | PASS, 5 checks | `lifecycle.json`: normal stop synced the filesystem, removed only NAT TCP/private sockets; new boot `d120dbfc-6188-4d91-be59-0981efe58d49`; DHCP/DNS/config repeatable; fresh stream/capture and HTTPS passed; protected Docker/A/relay unchanged. Assigned IP remained192.168.64.4; changed-IP reassignment was not forced. |
| End-of-window cleanup | PASS, 9 checks | `final-state.json` at14:48:02Z: both owners null; all actual X11 held keycodes/buttons empty; both exact load workers absent; owned Chrome closed; comparison lock removed; four TCP and all20 relay UDP listeners loopback; protected container IDs/images/start times and boot IDs unchanged. Both VMs left running. |

Physical Mac gestures/listening, long-run reliability, WAN impairment, changed-DHCP-address boot, bare-VM/no-viewer idle cost, crash/reset tests for the new NAT profile, and production multi-tenant hardening were **NOT RUN** here. Drag/wheel are **EMULATED** input, not physical pinch/swipe proof. Earlier independent relay/microVM failures and builder DHCP-attempt1 remain separate evidence; this addendum does not erase them.

## Fresh matched observations

Raw run: `comparison-2026-10-26T14-44-44-063Z/`, 14:44:44–14:46:26UTC. One fixed order: **relay-control → NAT-control → NAT-load → relay-load**. Each block used a new context in the same task-owned Chrome155.0.8059.85, window1440×1040/DPR1, native SDK input, reloaded local fixture,5s warmup,8s passive media/resource window, then30 clicks. Load added one guest CPU loop with45s hard deadline; both workers were confirmed alive through all observations, then stopped by exact marker/PID. Other VMs remained up without test viewers.

Both guests independently matched Neko binary SHA256 `ccbf8d7f2c10b584629349ddc8d882fe90a6471932d5b5c3d91d9cd75fcd8512`, Firefox153.5.1esr, fixture SHA256 `c4ba137b17fdfd31612c40a8bbb78c596bc97171b5d7e627418b82d828ae900c`, and config hash after removing only NAT address/UDP-range lines. Configured software VP8 profiles/threads/targets match; observed codec and1280×720 match. Active encoder target bitrate was not measured independently from the configured profiles.

Latency starts at capture-phase native-overlay mousedown and ends at first observed decoded synchronous click-counter ROI change, using the same browser monotonic clock. Full RGB is sampled by rAF; ROI excludes clock/hover/marker, threshold≥12pixels with RGB difference>45. Counter increments are confirmed separately **outside latency**; validation took at most8ms. Timeout2500ms; raw observations are checkpointed before validation and validation failures would be retained. No failed/invalid samples occurred; no outliers were discarded. Standard median averages the middle pair; p95 is nearest rank `ceil(.95*n)-1`.

| Block | Valid/attempts | Median ms | p95 ms | Max ms | Poll interval median / p95 / max ms |
|---|---:|---:|---:|---:|---:|
| Relay control |30/30|92.00|94.60|102.40|16.60 /17.30 /18.80|
| NAT control |30/30|89.55|127.00|127.80|16.60 /17.40 /17.70|
| NAT load |30/30|116.85|212.20|269.10|16.60 /16.80 /17.50|
| Relay load |30/30|141.00|271.80|340.70|16.60 /17.40 /17.70|

The control median difference2.45ms is below the typical16.6ms observation interval; NAT's control p95 was higher. Loaded NAT observations were lower than relay in this run. One fixed order, short windows, differing guest ages/cache/memory states, host background work and observer overhead prevent a causal transport ranking. These are whole-route local observations, not ping/RTT, screenshot HTTP duration, physical display latency or WAN predictions.

## Media and host resources

| 8s media window | Decoded frame delta | RTP jitter median / p95 ms | Interval jitter-buffer mean ms | Largest rVFC callback gap ms |
|---|---:|---:|---:|---:|
| Relay control |237|3 /16|20.45|83.4|
| NAT control |240|2 /8|14.25|83.4|
| NAT load |223|8 /27|21.42|183.3|
| Relay load |220|18 /24|38.76|166.6|

All four intervals reported packet-loss delta0, decoder-drop delta0, native freeze/pause deltas0, and no rVFC gap≥250ms. Concurrent rAF maximum gap16.8ms. These are short receiver observations, not a no-loss/no-freeze guarantee. Native freeze counters and observer callback gaps are distinct. Jitter-buffer mean uses delta(delay)/delta(emitted), not a cumulative ratio. Raw playback-quality and RTP snapshots remain available. Relay internal queue/drop metrics were **not sampled** in this comparison, so RTP loss0 cannot establish relay datagram-loss0.

Actual VM XPC helpers were mapped by exact open disk path: relayPID50515, NATPID74354, separately from runners50514/74353 and host relay50531. CPU uses cumulative CPU-time delta divided by actual sampled elapsed time;100%=one core. Values below describe the passive media window, not the full click block or a stopped/idle VM.

| Block | VM XPC CPU core-% | VM XPC RSS median MiB (min–max) | Runner CPU / median RSS MiB | Relay CPU / median RSS MiB |
|---|---:|---:|---:|---:|
| Relay control |54.26|1098.4 (718.3–1272.2)|0.38 /4.5|1.00 /11.0|
| NAT control |41.74|645.8 (626.1–648.1)|0.00 /5.2|N/A|
| NAT load |139.64|531.5 (527.6–533.8)|0.00 /5.1|N/A|
| Relay load |150.83|550.5 (533.1–558.2)|0.25 /5.4|0.87 /11.1|

Task Chrome component CPU/RSS are preserved separately in raw resources. Initial-component CPU sums were18.80%,11.50%,11.09%,10.49% in order; **relay-control is incomplete as a total** because an Unzipper utility process appeared late. The other three observed Chrome process sets stayed stable. First-block startup work and the relay guest's older memory/cache state confound comparisons. RSS can change dramatically and includes shared/compressed accounting; it is not unique physical RAM or proof NAT saves memory. A sampled runner delta0 at ps resolution is not proof zero work.

## Reproduction and provenance

Review `protocol.md`, `compare.mjs`, `host.py` and `analysis.mjs`. The protocol retains its original preparation wording as the reviewed preflight document; execution evidence above establishes current completion. Commands from repository root, only during a coordinated exclusive test window:

```sh
node artifacts/independent-qa/microvm/nat/analysis.mjs --self-check
node artifacts/independent-qa/microvm/nat/functional.mjs --active
python3 artifacts/independent-qa/microvm/nat/internet.py
python3 artifacts/independent-qa/microvm/nat/lifecycle.py --active
node artifacts/independent-qa/microvm/nat/compare.mjs --ready --reviewed
python3 artifacts/independent-qa/microvm/nat/final-state.py
```

Lifecycle intentionally restarts only NAT. Comparison creates a fresh run directory, preserves every attempt and prevents duplicate execution with an active lock. Do not blindly repeat a command after interruption; inspect exact process/lease/worker and partial manifest first. `final-state.py` is read-only and pinned to this run.

`recomputed-summary.json` independently recomputes standard medians/p95/CPU from raw observations. Builder's separate read-only arithmetic/integrity review (`pocs/microvm/artifacts/nat/statistics-review.json`, `raw-integrity-review.json`) also passed all120 counters+1, changed-pixel/hash checks, summed-poll latency, interval buffer ratios and CPU deltas. `provenance.json` records hashes; raw samples were not rewritten. No new measurements or optional tests followed cleanup. Screenshots stay local; only this written addendum is intended for the existing combined microVM Library report. No functional blocker remains within tested scope; limitations and the preserved helper failure remain as stated.


---

## Preserved previous noVNC and relay report

The following is the previous report before NAT expansion, retained unchanged. Its earlier scope/limitations apply to the profiles described there.

# Sidekicks microVM report — verified Neko media expansion

**The previously unimplemented Neko media path now works in a separate no-NIC Virtualization.framework guest.** Independent media, native/gated input, human takeover injection cutoff, native terminal command output, capture and restart/cleanup checks passed. The bounded relay carries UDP at the browser/Neko edges through reliable ordered vsock streams. Native UDP parity and application input-queue flushing are not claimed.

Live viewer: http://127.0.0.1:7906/ · native input: http://127.0.0.1:7906/?mode=native · adapter: http://127.0.0.1:7796 . Media UDP binds only127.0.0.1:59120–59139. The guest is left running, unowned and with no held keys/buttons. Original noVNC VM A and accepted Docker services remain unchanged.

This updates the same written microVM Library item; original POC Library identities remain untouched. Screenshots stay repo-local only. This revision adds an optional Neko profile; historical statements that UDP was unsupported describe the original noVNC-only bridge. The original report and failed attempts are preserved below, not relabeled.

## Neko implementation and builder evidence

# Sidekicks Neko microVM UDP — builder verification

The previously unimplemented media path now runs in a separate no-NIC Virtualization.framework guest. Actual Chrome ICE selected UDP to 127.0.0.1:59122 and decoded 1280×720 video; after an explicit guest/relay stop and new-kernel start, a fresh browser selected UDP port 59137 and decoded increasing frames. This is UDP at the edges transported inside reliable ordered vsock streams. It is not native VM UDP networking or demonstrated parity.

The live demo is http://127.0.0.1:7906/ (gated input), http://127.0.0.1:7906/?mode=native (official Neko SDK data-channel input), adapter http://127.0.0.1:7796. Host media binds only to 127.0.0.1:59120–59139. No host firewall, routing, VPN, NAT, security permissions, persistent credentials or cloud resources were changed. Original Docker demos and noVNC VM A remain unchanged/live. Independent raw artifacts now record initial media 4/4, functional 9/9 and lifecycle 5/5 checks passing, with fresh media/capture included in lifecycle. Preflight and final state also pass. The independent report is frozen and its live window released; parent review remains pending.

## Implementation and origin

Neko 3.2.0's pinned Pion defaults exclude loopback ICE gathering. A single isolated source insertion enables `SetIncludeLoopbackCandidate(true)` in the EPR path; no UDP mux/TCP mux fallback is enabled. Accepted ARM64 base image a7120e…fec85 is unchanged; separate image c27c3a…46146 adds only the rebuilt binary (SHA256 ccbf8d7f2c10b584629349ddc8d882fe90a6471932d5b5c3d91d9cd75fcd8512). Official pinned source archive, original/patched source, exact diff, Go dependencies/compiler, ELF verification, base layers and build logs are retained in pocs/microvm/neko-source. Primary upstream references: [Pion loopback filtering](https://github.com/pion/ice/blob/v4.2.2/net.go), [Pion SettingEngine](https://github.com/pion/webrtc/blob/v4.2.11/settingengine.go), [Neko WebRTC configuration](https://neko.m1k1o.net/docs/v3/configuration/webrtc).

Rootfs is Debian 13 (trixie), copied from the accepted Neko source-built image. The 4 GiB ext4 guest disk adds copied init/config/endpoint adaptations; 2 vCPU / 2 GiB and Linux 6.12.28 boot asset match the first-level VZ design. There is no NIC, guest external egress, shared host directory, host home/engine socket, USB or GPU. Guest desktop and exec use uid 1000; PID1/root supervisor agent remains a POC boundary. No Docker daemon runs in the guest. This remains direct Virtualization.framework, not Apple container CLI, Containerization, Kata, nested KVM, GKE or EKS. Host macOS 15.3 and Apple CLI prerequisites remain unchanged.

## Relay semantics and bounds

One hostUDPmedia-port/client-loopback-port pair receives one private stream, one two-byte network-order datagram framer and one connected guest UDP socket. Port and peer association is stable for that stream; empty datagrams are preserved. Host replies use the same bound listening port and only the endpoint which first sent a packet. No payload selects an address. Guest destination is fixed to 127.0.0.1 within 59120–59139; outside-range requests are rejected. Private host UDS is mode 0600 under mode 0700 state. Host caps 64 peers, 262144 queued bytes per direction/peer and 60-second idle expiry. Stream backpressure drops excess UDP rather than allowing unbounded queues. Only one Neko media range/session is supported at a time.

A reliable ordered stream changes UDP loss/reordering/congestion and introduces head-of-line behavior. No WAN/loss/jitter/congestion or matched media-latency/performance comparison was run. Native physical Mac gestures, audio waveform/physical listening, hostile escape/exhaustion, production multi-tenant hardening and GPU/snapshots remain untested. Generic takeover revokes new bot injection and cancels future queued events; already delivered application events may still render later.

## Builder tests and preserved failure

- **9 E2E checks passed:** actual selected ICE UDP/decoded frames; gated browser and native SDK keyboard/text/modifier/click/right/double/drag/drop/scroll/release; unleased native host request denial; normalized automation; copied-session/forged token/Origin/Host denial; human preemption; simultaneous human/agent ownership; fresh bounded 640×360 JPEG capture. Raw E2E evidence includes transport and fixture state.
- **8 transport and lifecycle checks passed:** separate kernel/resources; non-root exec/noNIC/share/socket; empty, binary and 8192-byte real-VM datagrams and two-peer separation; outside-range denial; actual 20 loopback host UDP listeners; stop closes all 20; new-kernel restart restores health/fixture/relay; original accepted containers and A boot unchanged and endpoints healthy.
- **2 checks after restart passed:** fresh browser selects UDP and decodes frames 4→64 over~2s, DTLS connected, plus fresh bounded capture. Short raw RTC observation reports packetsLost=0 and framesDropped=17; retained without causal/comparative inference.
- **4 unit checks passed:** fragmentation/coalescing/empty/max framing, malformed/oversized frame and invalid destination rejection, real local UDP peer separation. Unit frame capacity is not host payload support.

The first 65507-byte end-to-end datagram attempt failed on macOS send with EMSGSIZE. The failed report/log remain transport-attempt1-host-max-datagram.json/log. Read-only `net.inet.udp.maxdgram` is 9216: host-local 8192/9216 PASS, 9217/65507 FAIL. No sysctl/security setting was changed. Actual guest-bound roundtrip is proven through 8192 bytes; 9216 has only host-local proof. Oversized host return packets are dropped/counted.

Cached prepared first startup observed agent 1.30 s / health 4.03 s / fixture 5.57 s; reboot 0.36 / 1.95 / 3.59 s. These are host polling boundaries excluding build/export/disk preparation, not first decoded-frame time or a comparative benchmark.

## Reproduce and clean up only this guest

```bash
bash pocs/microvm/neko-source/build.sh
bash pocs/microvm/prepare-neko.sh
python3 pocs/microvm/microvm.py create neko 7906 7796 neko # only if absent
python3 pocs/microvm/microvm.py start neko # only if stopped
node pocs/microvm/neko/e2e.mjs
python3 pocs/microvm/neko/verify-transport.py # includes Neko-only restart
node pocs/microvm/neko/smoke.mjs
python3 pocs/microvm/microvm.py capture neko /tmp/neko-vm.jpg
python3 pocs/microvm/microvm.py stop neko
python3 pocs/microvm/microvm.py destroy neko # optional, only when stopped
```

Preparation uses named unprivileged/no-network disposable export/disk helpers with no host mounts; successful preparation removes them. The task-local signed runner/relay use detached processes/logs, without autostart service. Stop validates exact runner/relay PID commands, syncs only this guest and releases UDP/private endpoints; no broad kills/prunes. Read README for asset caches and safe retry/diagnostics. Screenshots remain local QA evidence and are not uploaded to Library.

## Source/runtime provenance note

The prepared guest module hash is57e673fb86692fdc8098118f40a28962f28d801bfbbd1f4ee001789e253ef54f. After preparation, the host-only UDP reply error handling was broadened from BlockingIOError to OSError to count/drop an oversized Mac UDP send. The current host module hash is0b08cb4509fd7c67762cbc38ef43e2c5f52ad7659e726670e97f9717c0159a00 and was loaded on the builder restart. The guest_udp function AST is identical in both files, independently recorded in relay-source-provenance.json. No guest runtime was silently replaced during QA.

## Independent control observation

The independent functional run records 9 PASS checks. In its takeover observation, the controller injection count remained 315→315 after acknowledgement, while fixture snapshots showed text length 27→38, separated by a 650 ms observation wait. This does not distinguish application processing from fixture-telemetry settling or establish delivery latency. Injection cutoff passed; application-queue flush was not established and is not promised. Human readiness is an ownership acknowledgement, not proof every prior application event has rendered. This is retained as an explicit limitation. Independent lifecycle and final-state artifacts also pass. This remains a reviewable POC with documented limits, rather than production or generic application-drain acceptance.


## Complete frozen independent Neko verification

Source: `artifacts/independent-qa/microvm/neko-udp/report.md`, SHA256 `3414735c38a4ce3b2b7414e9cd668c1eac20708007115e7af005304a20483b72`. Appended verbatim.

# Independent Neko microVM verification

**Complete: 2026-10-26 09:05:40 EDT.** The isolated Neko VM demonstrated actual UDP-selected WebRTC video, native and gated input, fresh capture, and normal stop/restart with relay cleanup. Live window released; Neko remains running at **http://127.0.0.1:7906/** (native mode: **http://127.0.0.1:7906/?mode=native**), adapter **http://127.0.0.1:7796**. NoVNC VM A and the accepted Docker services remained unchanged. No further live checks are planned.

| Independent evidence | Result |
|---|---|
| Runtime preflight | PASS: actual patched guest binary hash, separate kernel, 2 vCPUs/2 GiB, 4 GiB disk, uid1000 exec, no NIC/shared host path/runtime socket, private 0600 sockets; all 20 UDP listeners and both TCP listeners loopback-only. |
| Initial media observer | 4 PASS: selected pair from transport.selectedCandidatePairId, both candidate protocols UDP, remote 127.0.0.1:59134, VP8 decoded 1280×720, frames 4→27 and video bytes150839→193447, native data channel open. |
| Functional checks | 9 PASS: separate native/gated text+modifiers/click/right/double/drag/wheel, direct native/HTTP exclusion, raw unleased native keys rejected, actual held key/button release, normalized agent input, takeover cancellation, JPEG agreement and native terminal output. |
| Lifecycle checks | 5 PASS: bidirectional relay counters grew; normal stop freed all 20 UDP ports, both TCP ports and private sockets; restart produced a new kernel/fixture; post-restart UDP media/capture passed; protected baseline unchanged. |
| Post-restart media observer | 5 PASS **nested within lifecycle**, not an additional full suite: selected UDP 127.0.0.1:59131, frames 4→27, video bytes155723→201475, data channel and fresh capture. |
| Final state | PASS: Neko boot e93d0d8b-9021-41ea-aa93-7cf5299d5e59, owner `null`, all X11 keycodes/buttons empty, relay running. HTTP 7790/7791/7792/7793/7796 all 200. A boot afa49c8a-80fe-4ee5-9771-b032da5d5c12 unchanged. |

Native SDK input left the HTTP-controller frame counter **0→0**; gated input changed it **0→214**, independently distinguishing the paths. Native mode rejected normalized HTTP and competing bot control with 409. Native and gated release each cleared an actually held Shift and button 1, verified through XQueryKeymap/XQueryPointer before any client-side keyup. Native xterm viewer commands produced the exact independently read guest file marker `vm_neko_native_1793019574355`; that temporary file was removed. I personally viewed the local native/gated viewer images, terminal output and post-restart capture.

Fresh capture agreed with the decoded fixture marker before restart: **1280×720 JPEG, 156421 bytes**, marker d3b6feca/count3. Post-restart capture was **1280×720 JPEG, 103601 bytes**, new marker 8360352b/count0. These are actual image checks without a model call, not latency measurements. Images remain local; **no Library image upload**.

## Takeover limit retained exactly

At **08:59:31.489 EDT**, the human acquisition returned. The controller injection counter stayed **315→315**, the queued producer and stale request returned 409, and neither queued-tail nor stale marker appeared. However, the first and later fixture snapshots showed text length **27→38**, separated by a **650 ms observation wait**. This establishes injection cutoff, not a stable application-text or queue-flush guarantee. It does not distinguish later application processing from fixture-telemetry settling or establish exact delivery latency. Already-injected events cannot be generically rolled back. No fixture fence or implementation workaround was introduced.

## Transport and provenance limits

The browser and Neko edges use UDP; the VM boundary uses **length-framed reliable ordered vsock streams**, one stream/connected guest UDP socket per learned peer. This changes ordering/loss behavior and can add head-of-line delay. It is not native end-to-end UDP networking, general guest egress or evidence of parity with Docker/WAN performance. No new benchmark was run.

The actual guest Neko binary matched the isolated ARM64 artifact SHA256 `ccbf8d7f2c10b584629349ddc8d882fe90a6471932d5b5c3d91d9cd75fcd8512`. Offline review proved the pinned 3.2.0 archive plus exactly one loopback-candidate setting insertion. Guest/host relay module hashes differ because the guest copy predates a host-only sendto/EMSGSIZE adjustment; their actual `guest_udp` function source hashes match byte-for-byte. Full hashes are in final-state.json.

Read-only host inspection returned UDP payload limit **9216 bytes**. Builder evidence separately reports empty/binary/8192-byte VM roundtrips/two-peer isolation and preserves the failed 65507-byte attempt. Independent QA verified real selected UDP/media/control traffic, not maximum payload or concurrent-peer stress. The framing's theoretical 65507 limit must not be presented as supported Mac payload size. No OS/network/security setting changed.

Physical trackpad gestures/listening, audio PCM through this new VM, WAN/loss tests, idle/concurrency/resource stress, unexpected-runner-death cleanup and adversarial isolation tests were **NOT RUN** independently. Wheel/drag are **EMULATED**. Guest browser sandbox/production-hardening limitations remain as documented. Builder 9/8/2/4 test groups are supporting evidence, separate from the independent results above.

## Evidence and reproduction

All raw evidence and QA scripts are in this folder:

- [preflight.json](preflight.json), [media-native.json](media-native.json), [functional.json](functional.json).
- [lifecycle.json](lifecycle.json), [media-native-post-restart.json](media-native-post-restart.json), [preflight-after.json](preflight-after.json), [final-state.json](final-state.json).
- [static-source-review.json](static-source-review.json); native/gated fixtures and local images accompany the reports.

Run only with an exclusive window for session `neko`; these commands do not target accepted Docker Neko:

```sh
python3 artifacts/independent-qa/microvm/neko-udp/preflight.py
node artifacts/independent-qa/microvm/neko-udp/observe-media.mjs --ready --native
node artifacts/independent-qa/microvm/neko-udp/functional.mjs --active
python3 artifacts/independent-qa/microvm/neko-udp/lifecycle.py --active
```

The preflight refuses to overwrite an existing baseline; preserve prior artifacts before an authorized rerun. Lifecycle restarts only `neko` and leaves it running. Scoped executor access is required. No user's browser/profile/native cursor, secrets/model API, unrelated service or host shell endpoint was used. The original microVM report SHA remains `f8d7bfd26e0f273fff83af7117eb30917bebf3a4356a7633b0eefc8a8785ae25`; this is a separate addendum for the existing written Library report's version history.


---

## Preserved original noVNC microVM report (before Neko expansion)

The following historical report remains unchanged. Its previous UDP limitation is superseded only by the separate bounded Neko profile above.

# Sidekicks microVM POC — parent review report

**Ready for review with documented limits.** Builder21/21 checks PASS; independent6 viewer +12 boundary/lifecycle checks PASS on the initial revision, followed by5 targeted discovery/readiness/cleanup PASS after a small metadata-only fix. A fast immediate shortcut failure remains documented; final acceptance requires parent review.

**Live desktop:** http://127.0.0.1:7903/ . This is an actual Linux noVNC/RFB framebuffer with guest Chromium and xterm, not just a host-served fixture. Adapter: http://127.0.0.1:7793 . The accepted workbench/noVNC/Neko POCs were unchanged.

## What was built and what is blocked

One first-level Linux VM per session using direct Apple **Virtualization.framework**, with2vCPUs/2GiB, separate3GiB ext4 disk and virtio-vsock. No NIC, host filesystem share, host home/engine sockets, USB or GPU is attached. Host endpoints bind127.0.0.1; exec/files use a0600 Unix socket under a0700 session directory. Desktop/exec run as guest uid1000, while PID1 agent is root.

The M1 Max actually runs **macOS15.3(24D60), Xcode14.2/Swift5.7.2**, with no Apple container CLI. Current [Apple container](https://github.com/apple/container#requirements) requires macOS26; current [Containerization](https://github.com/apple/containerization#requirements) requires macOS26/Xcode26. No OS upgrade or unsupported install was attempted. The fallback is **not** Apple container CLI, Containerization package, a Kata runtime or nested KVM. Kata/cloud/GKE/EKS remain untested; Neko UDP is unsupported by this TCP-only offline bridge. No host networking/security workaround was added.

Guest rootfs is Debian12, exported from existing accepted ARM64 noVNC image `sha256:64c4e5e66e745ee817c0baecf37e8f9084f4ba30c3d988f5c5089fb351c87b0c`. The copied rootfs adds a guest init/vsock agent and per-session endpoint metadata. Kernel6.12.28 comes from the [official Kata3.17.0 ARM64 release](https://github.com/kata-containers/kata-containers/releases/tag/3.17.0) as a boot asset only. Local archive/kernel hashes are recorded; no independently trusted maintainer signature/digest was verified.

## Measured scope and preserved failures

With all image/kernel/export/ext4/compile assets already cached, the builder observed VZ fixture readiness2.451s on one fresh disk and2.458–2.507s on three persistent-disk reboots. Docker's network-disabled comparison was2.083s and1.933–2.017s, probed internally through docker exec; VZ uses host UDS/HTTP. These different probes, caches, kernels and CPU controls are **not a fair speed ranking**. Fixture readiness is not decoded-viewer latency or memory snapshot restore. The later metadata refresh cost60.1s and is not a new benchmark.

- Preserved init/rootfs panic, partial207MiB disk clone and TCP-rebind failures led to corrected mounts, atomic3GiB template publication/clone checks and per-socket SO_REUSEADDR(not REUSEPORT).
- A recent **unsynced acknowledged write was lost after SIGKILL**. Passing persistence explicitly syncs before crash; normal stop syncs. Write ACK is not an fsync guarantee.
- Independent fast click→Control+A→type appended to existing text; paced input replaced it correctly. Cause is unestablished; paced PASS is not proof of cause or general fast-shortcut reliability.
- Alternate discovery originally advertised7903 for B. Per-session boot metadata fixed it;5 targeted checks passed, including A/B discovered RFB decode and cleanup. Original FAIL is preserved; no full suite/input/timing rerun was implied.
- Chromium uses--no-sandbox in the guest; PID1 is root. Output is clipped after buffering; concurrency/peak buffering, hostile escape, resource exhaustion, GPU/snapshots, network egress and Neko UDP are not hardened/tested.

## Safe use and cleanup

From the Sidekicks root, with prepared assets:

```sh
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py endpoints a
python3 pocs/microvm/microvm.py start a   # only if stopped
python3 pocs/microvm/microvm.py exec a -- /usr/bin/id
python3 pocs/microvm/microvm.py capture a pocs/microvm/artifacts/manual.png
python3 pocs/microvm/microvm.py stop a   # only this task-owned VM; guest sync then power off
python3 pocs/microvm/microvm.py destroy a # optional disposable disk removal, stopped only
```

Preparation/build commands are `bash pocs/microvm/prepare.sh` and `bash pocs/microvm/build-runner.sh`. Create a missing session with `python3 pocs/microvm/microvm.py create a 7903 7793`. Do not run broad kill/prune/cleanup. Successful tests removed B/qa-b and named Docker helpers; A remains live and unowned. No autostart service, persistent credential, model API or cloud resource was added. README contains exact environment/provenance and reproduction details.

## Exact21 builder check list

These checks precede the metadata-only revision. The independent5-check follow-up validates the affected discovery/readiness/cleanup surface on the revised template.

1. **PASS** — separate live Linux guests.
2. **PASS** — 2 vCPU and 2 GiB configuration visible in both guests.
3. **PASS** — guest exec is non-root, no host home/socket, loopback only.
4. **PASS** — guest mount evidence has no shared host filesystem.
5. **PASS** — exec preserves nonzero guest exit status.
6. **PASS** — guest files round-trip.
7. **PASS** — per-session disk data isolation.
8. **PASS** — files API rejects path outside guest workspace.
9. **PASS** — decoded viewer, input, and capture over vsock.
10. **PASS** — native terminal launched in same guest session.
11. **PASS** — explicit pre-crash guest sync completes.
12. **PASS** — runner crash stays contained to session A.
13. **PASS** — restart after crash gets a new kernel boot.
14. **PASS** — explicitly synced guest file survives VM crash/restart.
15. **PASS** — destroy/create resets guest disk.
16. **PASS** — Docker containers share one guest kernel boot.
17. **PASS** — baseline has CPU/memory caps, no network/host mounts/published ports.
18. **PASS** — same-image Docker baseline guest-internal capture.
19. **PASS** — accepted POC containers unchanged by verification.
20. **PASS** — existing POCs still healthy.
21. **PASS** — new live viewer/control host ports bind only loopback.

## Independent QA — complete frozen report

The following is the independent report, appended verbatim from `docs/microvm-verification.md` (SHA256 `f8d7bfd26e0f273fff83af7117eb30917bebf3a4356a7633b0eefc8a8785ae25`). Its original pre-discovery report and raw failures remain preserved. This combined document is a **separate new Library item**, leaving original POC report identities unchanged.

The actual native-terminal image remains repo-local only: `artifacts/independent-qa/microvm/native-terminal.png` (SHA256 `4922886ac6ca586556b9d3798d6da8f91a493446fc4a2a8790df0ebb2055cf7b`). Integration and QA personally viewed it: a real guest desktop/native xterm shows exact unique command output. It is from the core run before metadata-only revision; later read-only RFB checks cover both revised URLs. **No screenshot is uploaded to Library.**

---

# Independent microVM verification

Latest revision: **2026-10-26 05:39:46 EDT** — targeted discovery/readiness regression **5/5 PASS**. A now advertises 7903 and a fresh QA guest advertises 7904, each matching its outer manifest. The prior URL metadata failure is fixed for the tested revision. Fast-shortcut reliability remains unresolved; the passing paced result does not erase that failure.

The original 6-viewer/12-boundary checks below were performed on the **prior revision**. After the endpoint fix and template refresh, only the five targeted checks in the addendum were run; no full-suite, input, screenshot or benchmark rerun is claimed. The original report is preserved [unchanged](../artifacts/independent-qa/microvm/report-before-discovery-fix.md), SHA256 `f5d7190cea6a98937866ccf35c526a99aba3583f85d162a0e7b01e9af7754c99`. Original raw results and metadata FAIL remain untouched.

**Library scope: report only.** The native-terminal image remains local; no microVM screenshot is authorized for upload. Original accepted-POC Library identities are preserved.

Initial core verification completed **2026-10-26 05:26 EDT (09:26 UTC)**. The local direct-Virtualization.framework demonstration passed **6 independent viewer checks and 12 independent boundary/lifecycle checks**. Known failures and unsupported features remain explicit below. This is a local POC, not production multi-tenant hardening or an adversarial escape assessment.

Live demo: **http://127.0.0.1:7903/**. Adapter: **http://127.0.0.1:7793**. Session A is left running, owner `null`, with no held X11 keycodes/buttons. QA session `qa-b`, builder session B and Docker comparison helpers are removed. Accepted noVNC/Neko/workbench containers were unchanged and their services returned200 at final inspection.

## What actually runs

This is a first-level ARM64 Linux VM created directly with Apple Virtualization.framework. It has an exported Debian 12 noVNC desktop filesystem, Linux 6.12.28, Xvfb, Openbox/tint2, x11vnc, Chromium and xterm. The host bridges loopback TCP to guest virtio-vsock; the fixture runs in **guest Chromium**. It is not merely a host-served fixture or an image pretending to be a desktop.

Independent evidence includes an actual decoded 1280×720 RFB frame, typed fixture text and a real xterm receiving a bounded viewer-typed command. The command wrote a unique marker inside guest `/workspace`; a separate private RPC read returned that exact marker. Guest process inspection found Xvfb/Openbox/x11vnc/Chromium/xterm. I personally viewed both saved viewer and terminal screenshots.

Host inspection independently returned macOS 15.3 (24D60), arm64, absent standard Apple CLI paths, and the runner's virtualization entitlement. The current Apple `container` CLI supports macOS 26; current Containerization requires macOS 26 / Xcode 26. That path is prerequisite-blocked, not denied approval. No OS upgrade or unsupported install was attempted. This fallback is distinct from those products and from Kata runtime. [Apple requirements](https://github.com/apple/container#requirements), [Containerization requirements](https://github.com/apple/containerization#requirements).

## Independent result matrix

| Check | Result | Actual evidence / boundary |
|---|---|---|
| Live decoded viewer | PASS | Fresh RFB connection over TCP → vsock,1280×720 framebuffer; viewer.json. |
| Viewer keyboard/pointer | PASS with pacing | Exact replacement of pre-existing text, Control modifier and click counter; final marker `independent-microvm-1793006503074`. Fast-sequence failure below remains separate. |
| Fresh image capture | PASS | Real 1280×720 PNG, 216,917 bytes, source 1280×720; decoded/captured marker `SK-mvp1exy8` / probe 2 agreed at magenta pixel. No model API. |
| Native terminal | PASS | Viewer opened actual xterm and typed `printf`/`cat`; independent file read returned `native_microvm_qa_1793006505169`. File removed afterward. |
| Ownership/stale input | PASS | Human takeover released a real held Shift; stale agent request 409; direct unleased RFB key packets increased dropped counter 3 → 5. |
| Final held state | PASS | Independent XQueryKeymap found **no pressed keycodes**, XQueryPointer no held buttons; owner `null`, viewers 0. |
| Two concurrent VM kernels | PASS | A boot `03d1e24c-9a7e-4aa8-8a37-6571a404380c`; QA guest boot `ed029ee1-35b6-40bd-b3c8-58c37853f332`; distinct disk inodes. |
| Resources | PASS for configured/guest-visible resources | Both guests: 2 vCPUs, MemTotal 2,045,784 kB; each configured for 2 GiB, separate 3 GiB raw disk. This is not total host-memory accounting or dedicated physical cores. |
| Host-path/network exposure | PASS for inspected boundary | Exec uid 1000; host home/Docker/containerd sockets absent; no virtiofs/9p/host path mounts; only `lo`, route table empty. Config contains no NIC/shared directory. |
| RPC locality | PASS / source reviewed | Session directories 0700, Unix sockets 0600. Host TCP destination prefixes fixed to guest 6080/8080; private RPC executes guest commands, not host shell. |
| Guest files/exit code | PASS | Exact file roundtrip, marker absent in A, outside-workspace paths rejected, guest exit 17 preserved. |
| Crash containment/recovery | PASS | SIGKILL only QA guest; A retained same boot ID. QA restart got new boot `ca4a84fe-de7f-468d-b16c-07c0685747a7`. |
| Synced persistence | PASS | **Explicit guest sync before crash**; exact marker survived. Does not establish unsynced-write durability. |
| Stop/reset/cleanup | PASS | Sync/power-off removed runner/RPC/listeners; destroy/create removed marker; recreated guest boot `da43c457-4528-475d-bf84-b32fb908e871`; QA guest then stopped/destroyed. |
| Docker versus VZ kernel boundary | PASS | Existing Docker desktops share one kernel boot ID, distinct from all tested VZ boots. No accepted container mutation. |
| Actual host listeners | PASS | Only 127.0.0.1:7903/7793 remain; 7904/7794 closed. No NIC/host networking change was used. |
| Accepted POCs/workbench | PASS | IDs/images/start times/restart counts/ports unchanged during independent lifecycle; final 7790/7791/7792 HTTP 200. |
| Alternate-session URL metadata | **Historical FAIL; targeted fix PASS** | QA guest/capabilities advertises 7903 while its correct outer manifest says 7904. Use manifest/endpoints; no shared-disk/control conclusion follows from this metadata error. |
| Immediate click→Ctrl+A→type | **FAIL in one retained attempt** | Existing text was appended to despite logged `ctrl=true` events. Cause remains unestablished. Paced repeat replaced the complete previous string and passed; no implementation fix or full fast-input reliability claim. |
| Unsynced crash durability | **Observed FAIL in builder run** | Acknowledged recent/workspace marker lost after SIGKILL. Not independently repeated. File write acknowledgment is not an fsync fence; normal stop syncs. |
| Apple CLI / Neko UDP / egress | UNSUPPORTED here | Current Apple CLI OS prerequisite unmet. No guest NIC/egress; bridge TCP-only, so no Neko UDP/WebRTC success. |
| Snapshots/GPU/cloud/Kata runtime | UNSUPPORTED / NOT RUN | No snapshot restore, GPU, cloud deployment, nested KVM or Kata runtime validation. |
| Physical gestures/audio listening/security stress | NOT RUN | No native Mac cursor/user tabs, physical trackpad/listening, kernel escape, CPU/memory exhaustion or hostile-output tests. |
| Full gesture/input suite and input latency in VM | NOT RUN | No new 30-sample latency, wheel/drag/pinch/resize/quality benchmark; accepted adapter tests do not automatically prove those on this VM. |

The successful paced shortcut waited 150 ms after the pointer click, held Control 40 ms before a, used 40 ms key press, waited 40 ms before Control release and 100 ms before typing. It is a separate observation from the retained fast failure. The first independent attempt also hit a QA-only JSON-parser error on the expected **plain-text 409** rejection; that attempt is preserved, and the corrected parser rerun is separate evidence.

## Startup and resource method

Independent QA measured three individual starts of its own disposable guest using host monotonic time around the existing start function:

| Prepared/cached observation | Agent ready | DesktopHTTP ready | Fixture telemetry ready |
|---|---:|---:|---:|
| Fresh QA disk |0.713s|1.472s|2.711s|
| Recovery after forced crash |0.357s|0.959s|2.500s|
| Fresh disk after destroy/create |0.455s|1.194s|2.422s|

Each row is n=1. Download/export/ext4 preparation/kernel acquisition/Swift compile and disk cloning are excluded. Readiness is guest-agent/HTTP/fixture-telemetry readiness, **not** first decoded human frame, input latency, memory snapshot restore or a factory-cold boot. No aggregate percentile or causal speed ranking is justified by these three different cases.

Builder’s final 21-check run is supporting evidence, separately labeled in `builder-final-review.json`: fresh VZ fixture 2.451 s (n=1), persistent-disk warm restarts 2.458–2.507 s (n=3); network-disabled Docker fixture 2.083 s (n=1), warm 1.933–2.017 s (n=3). Docker was probed by `docker exec` into internal loopback, including exec/poll overhead; no external Docker viewer/input timing was measured. VZ uses host UDS/HTTP polling. Docker has a 2-CPU quota inside its existing shared VM; VZ has 2 configured virtual processors per guest. Different probes, kernels, caches and resource semantics preclude a fair speed ranking. Total host memory/CPU overhead and sustained load were not independently measured.

## Preserved failures and remaining limits

The builder's original partial-disk/rootfs panic, rebind failure and unsynced-write loss remain preserved. Independent static review identified the missing SO_REUSEADDR during crash recovery; current runner sets that socket option only, not SO_REUSEPORT, and publishes the compiled/signed binary atomically. Independent crash/restart passed after the repair. No OS network setting changed.

Guest Chromium uses `--no-sandbox`, and the private PID1 agent is guest root. VM isolation is an additional boundary; it is not a proof of hardened browser or multi-tenant isolation. Exec output is clipped to 1 MiB **after** `communicate` has buffered it; maximum 30 s timeout and response clipping do not bound transient buffering or concurrent workers. Kernel comes from the official Kata 3.17.0 release asset, used only as a boot asset. Local archive/kernel hashes are recorded, but no separately trusted maintainer signature/digest was verified. These are documented prototype limits, not new host-side privileges or a host shell fallback.

No unresolved execution/approval blocker remains for the demonstrated offline noVNC desktop. General guest internet browsing and Neko media need a future networking design. Fast shortcut reliability remains unresolved. The alternate-session URL finding was fixed and passed the targeted revision check below; the original failure remains preserved.

## Reproduce and inspect

Use only a coordinated exclusive additive window. The viewer script sends bounded synthetic input to A; the boundary script refuses an existing `qa-b` or occupied 7904/7794, creates only `qa-b`, and cleans it while preserving A. It explicitly syncs the marker before forced power-off.

```sh
node artifacts/independent-qa/microvm/independent-viewer.mjs --active
python3 artifacts/independent-qa/microvm/independent-boundary.py --active
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py endpoints a
```

Scoped executor loopback/Docker/Virtualization.framework access is required. No model secrets, user browser profile, native Mac input, cloud service or new runtime install is used. Optional stop of the additive demo only: `python3 pocs/microvm/microvm.py stop a`. Destroy only a named stopped disposable session; no broad cleanup command.

Evidence files:

- [Viewer6-check report](../artifacts/independent-qa/microvm/viewer.json), [actual decoded viewer](../artifacts/independent-qa/microvm/viewer-capture-agreed.png), [native terminal screenshot](../artifacts/independent-qa/microvm/native-terminal.png), [fresh PNG](../artifacts/independent-qa/microvm/capture.png), [fixture events](../artifacts/independent-qa/microvm/fixture-events.json).
- [Boundary/lifecycle12-check report](../artifacts/independent-qa/microvm/boundary.json), [final passive state/source hashes](../artifacts/independent-qa/microvm/final-state.json), [host static evidence](../artifacts/independent-qa/microvm/host-static.json), [initial static review](../artifacts/independent-qa/microvm/static-review-initial.json).
- [Retained fast-shortcut FAIL](../artifacts/independent-qa/microvm/viewer-attempt2-fast-shortcut/viewer.json), [its fixture events](../artifacts/independent-qa/microvm/viewer-attempt2-fast-shortcut/viewer-attempt2-fixture.json), [QA parser failure](../artifacts/independent-qa/microvm/viewer-attempt1-json-parser/viewer.json).
- [Builder final review](../artifacts/independent-qa/microvm/builder-final-review.json), [preserved rebind failure](../artifacts/independent-qa/microvm/builder-crash-recovery-first-failure.json), [preserved unsynced loss](../artifacts/independent-qa/microvm/builder-unsynced-crash-durability-first-failure.json).

The final passive listener command returned lsof exit 1 because the selected QA ports correctly had no listeners, while its output contained the two expected A loopback listeners. The initial QA exit-code-only assertion was corrected by inspecting actual output; the raw diagnostic remains saved. This was not a service failure.


## Targeted endpoint-discovery correction

Independent check completed **2026-10-26 05:39:46 EDT** (2026-10-26T09:39:46.199831+00:00). The builder made a narrow source change: the runner passes assigned viewer/API ports on the guest kernel command line; guest-init validates numeric unprivileged ports and sets environment values; the copied adapter derives viewerUrl and its assigned local Origin ports from those values. The builder refreshed the cached rootfs/runner and recreated A; the old A state and all failure artifacts were retained. Accepted noVNC/Neko/workbench implementation and images were unchanged.

| Targeted check | Result | Evidence |
|---|---|---|
| A discovery/own Origin/readiness | PASS | Manifest and capabilities both http://127.0.0.1:7903/; own Origin 200, untrusted Origin 403; HTTP health ready. |
| Fresh qa-b discovery/own Origin/readiness | PASS | Manifest and capabilities both http://127.0.0.1:7904/; own Origin 200, untrusted Origin 403; HTTP health ready. |
| Discovered viewer endpoints | PASS | Both fresh, task-owned headless RFB connections decoded 1280×720 nonzero frames, no lease held. No input or screenshots. |
| Cleanup and remaining demo | PASS | qa-b stopped/destroyed, 7904/7794 closed. A boot `afa49c8a-80fe-4ee5-9771-b032da5d5c12` unchanged through QA; owner `null`; all X11 keycodes/buttons empty. Actual remaining listeners 127.0.0.1:7903/7793 only. |
| Accepted POCs/workbench protection | PASS | Container IDs/images/start times/ports unchanged; accepted backend APIs healthy. |

The current A fixture marker is `SK-mvp2b7bg`; live viewer remains http://127.0.0.1:7903/. QA's fresh second guest boot `6d22c07a-80a1-432b-9a35-63e981df90d5` was removed after testing. This addendum resolves the advisory discovery URL issue only. Fast immediate Ctrl+A behavior, unsynced power-loss durability limits, unsupported networking/Neko UDP, and other original limitations are unchanged.

[Targeted raw report and revised source hashes](../artifacts/independent-qa/microvm/targeted-discovery.json). Exact targeted reproduction, only after an exclusive builder/user handoff:

```sh
python3 artifacts/independent-qa/microvm/targeted-discovery.py --active
```

The helper creates only `qa-b` on 7904/7794, opens discovered viewers read-only, and cleans its guest. It does not send keyboard/pointer input, acquire control, save screenshots, produce timing statistics or run the full acceptance suite. The selected native-terminal PNG remains a local **prior-revision** artifact and was not regenerated or uploaded.
