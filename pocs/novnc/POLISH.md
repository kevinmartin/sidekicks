# Accepted Xfce desktop — 2026-10-28

Status: production accepted on2026-10-28. Docker7901 container `98d236dbc3321d82417ec2de98ee39aa02c0beda15fff3c191cd59f2cdf433b8` and no-NIC VM A7903 use frozen image `sha256:f8dee4d59f9bb8b26676928b80f784cd4f982a591812c0f1fe501d0b564a1b38`, tagged `sidekicks-novnc:polish-xfce-final`. Independent bounded production acceptance and exactly one supported normal stop/start per target passed. The final passive audit at14:31America/New_York (18:31UTC) found both ready, one Home each, no owner or actual held keys/buttons, with private bindings intact. Originald590 is stopped and preserved as `sidekicks-novnc-pre-xfce-20261028`; image0bdd remains untouched. Integration stopped the released task-owned stages, preserving their containers/images. This worker made no production runtime/input/build call. [Root desktop report](../../docs/desktop-polish.md) is authoritative; independent results are [production-summary.json](../../artifacts/independent-qa/desktop-polish/production-summary.json) and [final-production-state.json](../../artifacts/independent-qa/desktop-polish/final-production-state.json). Complete frozen source/image hashes: `artifacts/novnc/polish/final-freeze.json`.

## Desktop and scope

The new guest uses a standard `xfce4-session` on a private session D-Bus: one xfwm4 compositor/window manager, xfsettingsd, panel, xfdesktop and Thunar daemon. Chromium has real embedded window controls, managed Sidekicks Home/startup/New Tab settings and a distinct `SidekicksBrowser` class/profile. Three fixed dock/Home/shortcut helpers restore the same existing normal client rather than creating duplicate windows. The desktop menu contains Browser, Terminal and Files. Settings customization is constrained through Xfce's supported system channel named-user lock (`unlocked="root"`); this is menu UX, not a security sandbox.

Thunar replaces GTK2 PCManFM. The primary terminal becomes GTK3/VTE `xfce4-terminal`, with shared original Sidekicks navy/cyan palette, DejaVu Sans Mono11 and88×24 geometry. Debian terminal1.0.4 does not support the newer `--class` option: the helper uses its supported unique `--role=SidekicksTerminal`, real class `Xfce4-terminal`, and both identifiers when finding a managed client. Neko's newer terminal supports a literal `SidekicksTerminal` class; its one-time terminalrc migration was separately observed by that worker.

XTerm/UXTerm were legacy comparison clients, not separate operating systems or evidence of GTK theme support. UXTerm wraps xterm with UTF-8 handling and the UXTerm resource class ([vendor manual](https://docs.oracle.com/cd/E62101_01/html/E62874/uxterm-1.html)). Shared compatibility resources remain available, but the clean candidate omits the old xterm package; preserved baseline images retain those clients. Openbox, tint2, picom, PCManFM and GTK2 are also omitted from the fresh recipe. No WM hot replacement or existing terminal-job migration was performed.

Original shared GTK3 Adwaita/Sidekicks assets and DejaVu fonts are used; no proprietary cloud theme/font/artwork was copied. Canonical backend-neutral session/panel/hotkey/terminal templates are owned under `shared/desktop/polish/xfce/`, with actual monitor substitution (`screen` here, observed `DUMMY0` in Neko). Xfwm is the sole compositor. Native controls, Super+B/Return/E/D, Alt+F4/F9/F10/Tab/F12 and separate Chromium F11 remain. The original x11vnc `-nowireframe`, RFB gate, normalized input arbitration, held-input cleanup, screenshot path and loopback/LAN adapter boundaries remain in place.

## Measured footprint

| Image / method | Docker image bytes | Delta vs0bdd | Layers |
|---|---:|---:|---:|
| Preserved original0bdd |1,059,242,453|0|14|
| Official Thunar dependency addition |1,064,969,480|+5,727,027|15|
| Full Xfce dependency addition, old stack retained |1,079,901,917|+20,659,464|16|
| Frozen clean runtime f8 |1,018,096,080|−41,146,373|10|

The final reduction is about39.2MiB (3.9%). This is a fresh pinned Debian runtime plus official packages, copying only the verified x11vnc/noVNC transport payload from the preserved image. It is not later-layer package deletion or a rootfs repack. Package Installed-Size accounting adds41,284KiB and removes81,073KiB; two official SSL security-revision updates add2KiB, giving−39,787KiB net. Package estimates and Docker image size are different measures. Exact package lists, original/additive/clean image identities and method are preserved in `artifacts/novnc/polish/{dependencies.json,xfce-dependencies.json,clean-footprint.json,final-freeze.json}`; the root report reconciles the final package accounting.

The unowned compiled x11vnc binary's runtime libraries are explicitly retained, including libvncserver1/libvncclient1/SSL/X11; `ldd` reported no missing libraries. No apt autoremove assumption was used. VM prerequisites runuser/mount/id/sync are present. Historical images, stopped stages and evidence remain, so host disk reclaimed is0bytes. No export/repack tar was created. No new causal idle/motion CPU, memory, latency or cross-backend performance claim was measured during this desktop change.

## Executed evidence and limits

All GUI input used an isolated headless Chrome/Playwright context and actual decoded noVNC pixels; no user browser profile, global Mac pointer or physical/mobile input was used. Builder checks below retain their stage scope. Independent production results are separately identified and include explicit references to earlier passing groups rather than relabeling those checks as newly executed.

| Check | Observed status | Evidence under `artifacts/novnc/polish/` |
|---|---|---|
| Standard session/sole stack, actual Chromium CSD, minimize/dock same ID/PID/geometry, maximize/restore | PASS on clean-r2 | `stage-2026-10-28T16-22-21-281Z/result.json` |
| GTK3 terminal exact fresh file proof; Thunar folder create/rename; native shortcuts | PASS on clean-r2 | same stage result and actual screenshots |
| Fixture viewer typing/Home; normal New Tab/close/reopen | PASS with documented CSD fixture calibration | `stage-2026-10-28T16-36-52-220Z/result.json` |
| Held native drag/resize | Actual moving geometry and captured held pixels observed; builder combined-suite FAILs preserved; final production acceptance remains the independently recorded bounded subset | `stage-2026-10-28T16-22-21-281Z/drag-held-*`, `resize-probe.json/png`, later stage failure states |
| Final f8 logout confirmation; Cancel preserves all3app client IDs/PIDs and session PID | PASS | `menu-confirm-2026-10-28T16-52-54-342Z/result.json` |
| Final f8 confirmed session exit reports503/readyfalse; no stale readiness | PASS | same menu result |
| One normal stage restart, confirmation persists, Cancel preserves clients/session | PASS | `menu-confirm-2026-10-28T16-53-46-195Z/result.json` |
| Final f8 guest F11/AltF12 reversible; viewer Escape/fullscreen actual held cleanup | PASS | `final-smoke-2026-10-28T16-54-54-455Z/result.json` |
| Final f8 browser close alone keeps desktop200/readytrue; explicit reopen | PASS | same final smoke result |
| Host start/rebuild stopped/current/mismatch/missing-image/build/no-replacement decisions |8mocked tests PASS; no real Docker call | `launcher-tests.log` |
| Docker7901 bounded production acceptance |8accepted groups:3new PASS +5referenced PASS; exactly one supported normal restart | Independent `docker-novnc-2026-10-28T18-28-35-319Z/result.json` via `production-summary.json` |
| VM A7903 bounded production acceptance |8accepted groups:2new PASS +6referenced PASS; exactly one supported normal restart | Independent `vm-a-2026-10-28T18-17-48-268Z/result.json` via `production-summary.json` |
| Final passive production audit | PASS, ready/unowned/actual held empty, one Home, sole Xfce components and existing private/LAN binding scope | Independent `final-production-state.json` |
| Physical keyboard/mouse/Mac trackpad/mobile | UNTESTED, mobile deferred | no claim |

The combined broader builder stage suite did not finish green: early fixture focus/offset and later fresh CSD drag/resize assertions failed. Those attempts were preserved, not overwritten or counted as full acceptance. Actual Chromium CSD `_GTK_FRAME_EXTENTS` are22/22/19/25; invisible shadows are part of its client bounds, so visible controls and resize borders differ from legacy WM-frame locators. A separate held resize probe observed1120×548→1080×521 while button1 remained held, then1060×508 on release. Independent QA established its own bounded production acceptance; that does not retroactively turn the interrupted builder suite into a full PASS.

Legacy fixture telemetry overstates its content origin by Chromium's25px bottom shadow. The stage test explicitly calibrated using native client/GTK extents; source desktop screenshot/input coordinates remain1280×720. Do not treat the fixture's old `screenY+outerHeight-innerHeight` origin as a validated CSD coordinate transform. No model API loop was used.

Chromium's “Restore pages?” bubble remains an observed restart UX limitation in both noVNC production variants. QA dismissed it through the viewer; VM A's initial decoded-marker failure under that overlay is preserved beside its successful bounded continuation. QA browser-close stalls and an executor-session loss interrupted two continuations. Exact owned-process cleanup, partial hashes and references to original passing checks remain; no extra guest restart was used to repair the test driver. Integration verified recursive raw-hash provenance. The one-browser/unowned/held-empty final audit does not mean bubble-free restart appearance. No post-handoff input or GUI change was performed by this worker.

The accepted production subset covers actual Home/New Tab and fixture input, GTK3/toolkit/window restore, logout confirmation/Cancel retaining three apps and content, Host/Origin/agent lease boundaries, held cleanup, one supported normal restart and fresh decoded input over the existing LAN route. Docker uses8groups (3new+5referenced); VM A uses8(2new+6referenced). Original raw reports, driver interruptions and earlier failures remain unchanged. Exhaustive context-menu actions, physical Mac reserved keys, mobile, performance, audio and resource comparisons are not new production PASS claims. Chromium's actual house button/Home preferences passed; ordinary Mac Alt+Home has a separate upstream modifier translation and is not classified as a broken Home preference.

## Actual stock menu and session behavior

The supported lock removes customization entries. Stock Help/About/Log Out remain. About opens the actual panel4.18.2 dialog; Help opens an online-manual confirmation, which was canceled. No documentation network-load success was claimed. The failed passive-grab experiment could not attach (BadAccess) and had no eligible visible descendants; it and all guard autostart/readiness requirements were removed. Evidence is retained in `failed-panel-context-guard.py` and `panel-guard-descendant-probe.txt`. No global hook, overlay, GTK module or patched panel is shipped.

With old PromptOnLogout=false, Log Out immediately ended all five Xfce components while API/VNC stayed up, and `/desktop/state` falsely returned its one-shot ready marker. This genuine failure is preserved at `menu-legacy-2026-10-28T16-45-18-495Z/`. Final PromptOnLogout=true presents an actual dialog. Cancel preserves app clients/processes and session. Explicit confirmation still deliberately ends the session; it requires a normal container start/restart to recover, with no automatic recovery framework. `/desktop/state` now samples current non-zombie Xfce component processes and returns503 if any are missing. Closing just Chromium leaves readiness true.

Restart/poweroff are unavailable in the observed dialog. Supported Xfce Shutdown kiosk rules concern those power actions, not generic logout ([Xfce session documentation](https://docs.xfce.org/xfce/xfce4-session/advanced)). System power/device management and accessibility bus integration are not implemented by this POC.

## Supported current commands and promotion

```sh
./pocs/novnc/start.sh
./pocs/novnc/status.sh
./pocs/novnc/stop.sh
./pocs/novnc/rebuild.sh sidekicks-novnc:xfce-candidate-new
python3 -m unittest discover -s pocs/novnc/tests -p test_launcher.py -v
```

`current-image.sh` pins the exact accepted f8 image ID. `start.sh` starts an existing matching stopped container, or creates it from that image when absent. A mismatched historical container is preserved and produces an explicit coordinated-cutover error; no stop/remove/rename is automatic. A missing current image gives a restore/rebuild-candidate instruction rather than silently building an old desktop. Docker viewer/API are7901/7791 on loopback; VM A uses7903/7793. Existing LAN viewers are17901/17903 on the integration bridge, whose private/admin boundaries are unchanged by these scripts.

`rebuild.sh` selects `Dockerfile.xfce-clean`, verifies the recorded0bdd `sidekicks-novnc:live-drag` transport artifact, builds a separate new candidate and stops there. It never changes containers, overwrites current/baseline tags or promotes itself. Restore that preserved transport image before a fresh build if missing. Source archives/base are pinned and exact installed versions recorded; Debian apt repositories are not frozen snapshots, so new builds need fresh QA rather than an exact-binary reproducibility claim. Keep/import the frozen image for exact identity.

Production promotion was performed by integration using stop/rename/preserve and the frozen image; independent bounded live verification and exactly one supported normal stop/start per target are complete. All five refreshed production POCs passed the final passive audit at14:31America/New_York; both noVNC targets were healthy, ready, unowned and held-empty. After release, integration stopped only task-owned stages/proxy and retained their data; production remained running. Historical images/recipes/evidence remain explicit baselines. No hot WM replacement, host OS/security changes or unrelated services are part of this work. See the [authoritative root report](../../docs/desktop-polish.md) for rollout/rollback details and final provenance.
