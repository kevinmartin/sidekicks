# Sidekicks / noVNC computer-use POC

> Integration promoted the frozen whole-desktop Xfce image to7901 on2026-10-28; independent local/LAN quick QA is underway. Current image, build/start commands, rollback and evidence are documented in [POLISH.md](POLISH.md). The original protocol/latency evidence below is historical and is not an Xfce performance benchmark. [UX.md](UX.md) preserves the first desktop pass.

A real, disposable Linux ARM64 desktop, rendered in an embedded noVNC viewer. It combines Xvfb 1280×720, Openbox, a themed tint2 panel, Chromium, xterm and x11vnc. This is a measured computer-use adapter/workbench POC; model calls and a production multi-bot platform are outside this implementation.

## Run

From the repository root, with the existing Docker Desktop daemon running:

```sh
./pocs/novnc/start.sh
./pocs/novnc/status.sh
./pocs/novnc/stop.sh
```

- Viewer: <http://127.0.0.1:7901/>
- Adapter: <http://127.0.0.1:7791/health>
- PNG capture: <http://127.0.0.1:7791/screenshot?maxWidth=1280&maxHeight=720>
- Shared synthetic fixture runs inside the desktop at `http://127.0.0.1:6080/fixture/`. The host viewer's `/fixture/` is the same HTML but should not be used to prove remote input.
- `start.sh` selects the exact Xfce image pinned in `current-image.sh`, starts a matching stopped container, and rejects a historical mismatch without changing it. Missing images produce a clear restore/build-candidate error. `rebuild.sh` explicitly builds `Dockerfile.xfce-clean` into a separate QA candidate and never replaces a container. `stop.sh` preserves the container. Promotion is a coordinated stop/rename/preserve operation after independent QA; see [POLISH.md](POLISH.md).
- Linux container runs natively on this Apple M1 Max/ARM64 host, under Docker Desktop's Linux VM. It has 2 CPUs, 2 GiB memory and 512 MiB shared memory. No host directories are mounted, no Linux capabilities are granted, no-new-privileges is enabled, and the raw VNC port is not published. Actual port/listener evidence is in `artifacts/novnc/`.

In the viewer, **Take control** acquires a human lease and pauses agent writes at the server. **Release control**, **Stop input**, viewer-window blur, owning websocket disconnect, and lease expiry release held inputs. **Run synthetic bot** uses the normalized API to type a unique marker in the fixture and releases its lease. It is rejected while a human owns control. The desktop globe launcher opens the fixture; the xterm launcher or `Super+Enter` opens the themed disposable terminal.

## Adapter and ownership boundary

The adapter implements the shared `docs/status/contracts.md` routes: `/health`, `/capabilities`, `GET/POST /control`, `/input`, `/screenshot`. It also provides `/metrics` and synthetic fixture snapshots at `/fixture-events?latest=1`.

`POST /control` acquires a token/session lease for `human` or `agent`. Human acquisition preempts an agent; neither another human nor an agent can silently steal a human lease. The maximum lease is 60 seconds, default 30. The viewer renews every 10 seconds. Releasing/expiring/preempting invalidates the token, and automation checks again between commands and between text chunks of at most 4 characters, paced at 20 ms per character. Human preemption revokes the agent before releasing held inputs and waiting a bounded 250 ms settling period. The viewer shows takeover pending until input ownership is ready. `GET /control` exposes the injection cutoff and settling duration; `applicationQueueFlushed:false` explicitly distinguishes ownership from application delivery. Already-injected events cannot be rolled back, and native app queues have no generic flush guarantee. An invalid batch from the authenticated current owner stops it and releases inputs; a foreign rejected request does not disturb its held state.

All browser RFB input passes an authoritative websocket gate. It frames the RFB 3.8/None client stream, including fragmented/coalesced websocket data; keyboard, pointer, clipboard, power and resize messages require the current human token/session. Unleased viewers still receive pixels and harmless framebuffer/encoding requests. Unknown framing closes the connection. Client `viewOnly` is a convenience, and bypassing it was explicitly tested: raw key packets were dropped at the gate. The backend VNC listener is container-loopback only. Tokens are bound over a socket text control message; the RFB stream remains binary.

The viewer accepts `/?session=...&token=...` for workbench integration, consumes it and removes it from the visible location. `GET /control` does not return tokens. Browser Origins are restricted to explicit loopback demo origins; there is no wildcard CORS. This local lease is arbitration, not production identity or authentication. Production use would require authenticated users, HTTPS/WSS, stricter session association, and a more complete audited protocol implementation. Chromium's sandbox is disabled inside this disposable container, so use only the synthetic test content.

Normalized events support key down/up, text, pointer move/down/up, single/double/right/middle click, both wheel axes, and releaseAll. Keys use the contract's browser-style names. Unsupported events return 422. No shell execution endpoint exists. Coordinates are source desktop pixels; local viewer scaling does not resize the remote desktop. For a640×360 capture of1280×720 source, multiply image coordinates by2 before sending input. The capture headers report both sizes.

## Executed verification

```sh
PYTHONPATH=pocs/novnc python3 -m unittest discover -s pocs/novnc/tests -v
npm ci --prefix pocs/novnc/tests --ignore-scripts --no-audit --no-fund
node pocs/novnc/tests/e2e.mjs
node pocs/novnc/tests/restart.mjs
node pocs/novnc/tests/native-takeover.mjs
node pocs/novnc/tests/malformed.mjs
```

Tests use pinned Playwright 1.56.1 with an isolated **headless** instance of the existing `/Applications/Google Chrome.app`; no user Chrome profile/tabs or global Mac pointer is used. These establish browser automation behavior, not physical human, mobile, or Mac trackpad behavior. The tested host Chrome version was 155.0.8059.85. The final 0.9.17 E2E suite passed 24 checks. `artifacts/novnc/e2e.json` retains assertions, fixture events, actual native X11 input-state queries and raw timing samples. Five framing tests passed separately. Restart/launcher results are in `artifacts/novnc/restart.json`.

| Capability | Normalized automation API | Browser viewer automation | Physical human/mobile |
|---|---|---|---|
| Text, Shift, Ctrl+A | PASS | PASS through actual RFB stream | UNTESTED |
| Single / double / right click | PASS | PASS | UNTESTED |
| Drag/drop with release | Implemented; not separately asserted by own suite | PASS | UNTESTED |
| Vertical/horizontal wheel | PASS, both axes asserted | PASS on0.9.17, both axes asserted;0.9.16 horizontal FAIL | UNTESTED |
| Resize coordinate mapping | Fixed1280×720 source | PASS at target + four fixture viewport corners | UNTESTED |
| Native pinch/swipe | UNSUPPORTED as native X11/RFB multitouch | No native claim; browser gesture translations untested | UNTESTED |
| Zoom via Ctrl+wheel or shortcut | Implementable key/wheel EMULATION; not separately asserted | UNTESTED | UNTESTED |
| Human takeover / stale agent rejection | Own PASS for injection cutoff, queued command cancellation and bounded settling | Own PASS for raw viewer gate, two viewers and immediate post-ACK input; no generic app flush guarantee | UNTESTED physically |
| Held release on takeover/disconnect/blur/expiry/rejection | PASS with XQueryKeymap/XQueryPointer | PASS; blur was simulated at browser event level | UNTESTED physically |
| Screenshot for LLM processing | PASS, fresh PNG1280×720 and640×360 | Decoded stream independently PASS | No model API call performed |
| Audio transport | UNSUPPORTED | No audio stream | No listening test |
| Wallpaper/panel/terminal/browser launcher | Restart check/evidence below | Actual rendered desktop | UNTESTED physically |

The shared fixture source is copied unchanged to `web/shared-fixture.html`. A served supplement adds a unique `SK-...` boot marker, alternating magenta/green badge, geometry and bounded snapshot reporting. It records only synthetic contents. `input_state.py` independently reads native X11 key/pointer state for QA; it sends no input.

## Observed timings and stream variants

Two presets request real [noVNC RFB quality/compression properties](https://novnc.com/noVNC/docs/API.html): Text=`quality9/compression2`; Motion=`quality4/compression1`. x11vnc logs confirm Tight encoding and requested JPEG quality/compression negotiation. No H264 was negotiated. The Text preset requests JPEG Q100 and is **not guaranteed lossless**. The independent root-window PNG capture is lossless at native resolution; resized PNG uses Pillow resampling.

The final own run used the same 1280×720 shared fixture, 2 CPU / 2 GiB desktop, fixed probe pixel and 35 repetitions per preset. Start was the trusted browser `mousedown` on the noVNC canvas. End was the first changed decoded badge pixel from `RFB.getImageData()`. Both timestamps use that viewer's `performance.now()` clock. Sampling used requestAnimationFrame; observed polling median 16.6ms, p95 18.1ms, max 19.0ms. Browser composition/physical display presentation is not measured separately. The solid badge/text workload does not establish video performance or general JPEG image quality.

| Observation | n | Median ms | p95 ms | Max ms | Failures |
|---|---:|---:|---:|---:|---:|
| Text preset input→observed decoded badge |35|65.30|80.80|120.60|0|
| Motion preset, same static fixture click workload |35|64.10|87.50|315.60|0|
|640×360 screenshot HTTP request+body |30|49.87|53.95|57.02|0|
|scrot root capture + initial PNG file write |30|24.61|26.07|27.82|0|
|Server capture+resize+PNG encode |30|46.80|49.33|50.20|0|

Capture/encode numbers are server-side monotonic measurements. The scrot duration includes its first PNG encoding and file write; raw X11 capture and that encoding were not isolated. The total additionally includes Pillow resize/re-encoding. HTTP timing is a different host clock/duration and includes transport. They are not human action-to-photon latency. One takeover request during a queued text batch completed in351.82ms including 270.3 ms server settling; that is one functional race observation, not a percentile benchmark. The fresh full-resolution capture was206,807 bytes with the unique fixture marker visible; exact headers, dimensions and marker are in `e2e.json`.

These are local synthetic POC observations, not matched noVNC-vs-Neko benchmarks. Earlier exploratory runs coincided with Neko compilation; the final 0.9.17 run was executed03:09UTC after build completion; host load was not isolated. Host load was not controlled, no physical input/display was measured, and no WAN conditions were emulated. Idle/motion resource comparisons remain unmeasured unless explicitly added by independent QA.

Automation key values matter: the successful Playwright viewer shortcut is `keyboard.press('Control+a')`, while a shifted character is `keyboard.press('Shift+A')`. These match real DOM `key` values. Artificial `key='A'` without Shift can cause x11vnc keysym modifier correction to synthesize Shift; this was observed during test development. Physical Mac keyboard input has not been verified.

## Evidence and reproducibility

- `artifacts/novnc/viewer.png`, `desktop.png`, `desktop-640.png`, `resized-viewer.png`: actual canvas and independent PNG path, visually inspected.
- `viewer-after-restart.png`, `wallpaper.png`, `terminal.png`, `desktop-after-restart.png`: restart and real desktop customization.
- `e2e.json`, `restart.json`, `x11vnc.log`, `listeners.txt`, `container-inspect.json`, `image-inspect.json`, `package-versions.txt`, `novnc-source.sha256`, `build.log`: measured/tested provenance. Debug files are intermediate exploratory evidence, not a final PASS claim.
- Base is pinned to Debian bookworm slim manifest digest `sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8` with `--platform linux/arm64`.
- noVNC upstream v1.6.0 archive is verified against SHA256 `5066103959ef4e9b10f37e5a148627360dd8414e4cf8a7db92bdbd022e728aaa`.
- Exact tested Debian package versions are recorded, rather than claiming package repositories are frozen. Key versions: active upstream x11vnc 0.9.17 (2025-04-11, compiled natively); packaged x11vnc 0.9.16-9 remains installed but is not launched, Xvfb21.1.7 Debian revision, Chromium156.0.8078.47, Python3.11/aiohttp3.8.4/Pillow9.4.0. Future clean builds can receive Debian security updates; retain the recorded image digest for exact binary identity.

Remaining limits: physical keyboard/trackpad/mobile coverage; audio; general motion/quality and controlled cross-backend resource benchmarks; full authentication; complete RFB extension coverage beyond this pinned backend. No current install/runtime blocker remains for noVNC. Independent final basic14/14 and appearance5/5 checks passed. Controlled cross-backend timing is pending Neko idle confirmation; this does not block the noVNC demo.

## Independent defects and repaired variant

The first Debian-packaged server, x11vnc 0.9.16, could display and accept basic inputs but silently ignored horizontal wheel buttons. Independent QA found this despite an earlier own assertion that checked only vertical movement. Upstream [0.9.16 params.h](https://raw.githubusercontent.com/LibVNC/x11vnc/0.9.16/src/params.h) defines MAX_BUTTONS 5; [0.9.17 params.h](https://raw.githubusercontent.com/LibVNC/x11vnc/0.9.17/src/params.h) defines 8. The final image compiles official0.9.17 in a separate build stage, verifies archive SHA256 `3ab47c042bc1c33f00c7e9273ab674665b85ab10592a8e0425589fe7f3eb1a69`, and launches `/usr/local/bin/x11vnc` explicitly. Both viewer and normalized horizontal/vertical axes now have separate own assertions. This is a server-version compatibility comparison, not a noVNC transport winner claim.

Independent QA also found that32-character, zero-delay text chunks could deliver 160 characters to Chromium, with DOM input events continuing 516 ms after the takeover ACK. Future HTTP commands were correctly canceled and held inputs released, but that did not erase already-injected application events. The final path uses4-character chunks at 20 ms per character plus a bounded 250 ms generic settling period. It enforces no new agent injection after revocation and reports ownership ready; it does not claim generic application queues are empty. In the final own fixture run, no bot text appeared after ACK or mixed with immediate human typing during 650 ms observation. Original failures remain under `artifacts/novnc/baseline-0.9.16/` and `artifacts/independent-qa/`; those are not overwritten by the repair.

`node pocs/novnc/tests/native-takeover.mjs` separately passed native xterm ownership and queued-command rejection, with immediate viewer typing of a synthetic shell comment. The screenshot visibly contains `# native-human-...`; no fixture acknowledgment or special function key participates. This proves the default takeover remains usable outside the browser test page. It is not a native-application queue-drain guarantee.

The terminal shortcut is X11 Super+Enter. The installed noVNC Mac keyboard code maps left Meta to Alt and right Meta to Super; independent automation uses MetaRight+Enter. Panel launchers provide a pointer path. Physical Mac key behavior remains untested.

## Additional takeover stress and recording

`node pocs/novnc/tests/stress.mjs` passed4 additional long-queue handoffs, alternating immediate human input through the real viewer and normalized human API. Two rounds ran one busy Python CPU for3 seconds inside the2CPU desktop. Each canceled the remaining4096-character batch and rejected its stale request. No post-ACK bot text events or intermix were observed during650ms per-round observation. Acquire requests took274.9–296.0ms including251–259ms bounded settling. These observations do not upgrade the API into a generic application-queue flush.

`artifacts/novnc/interaction-evidence.mp4` is a51-frame, nominal5fps sampled recording of the task-owned synthetic viewer, encoded locally with existing ffmpeg. Its H264 file encoding is only the evidence playback format: the actual noVNC transport negotiated Tight, not H264. `x11vnc-e2e.log` records the final0.9.17 backend negotiating Motion JPEG quality4/compression1 and Text quality9/compression2.

## Independent acceptance and pending comparison

Final independent QA basic14/14 checks and native appearance5/5 checks passed. The accepted checks include real decoded stream, both wheel axes, viewer/API input, native xterm takeover, raw unleased gate rejection, actual held-input cleanup, queued/stale rejection, no observed post-ACK text growth, screenshots/proxy freshness and reconnect. The repaired independent takeover took323.8ms including settling, with text length8→8 and no post-ACK bot input events.

Independent standard counter-row timing also produced30/30 successful samples: median98.3ms, p95132.1ms, max151.7ms; sampling median16.6ms, p9517.6ms, max19.3ms. This run is PROVISIONAL because unrelated backend activity was not confirmed idle. It uses same-clock viewer mousedown→first decoded change of the shared synchronous counter row (at least12 pixels differing by>45 RGB), rather than this worker's supplemental badge probe. Do not compare the two methods as the same benchmark. `artifacts/independent-qa/novnc-viewer-measure-provisional.json` preserves it. A controlled sequential noVNC/Neko comparison is pending QA's Neko-idle confirmation.
