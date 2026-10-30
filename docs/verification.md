# Independent verification

Verified 2026-10-26 UTC on Kevin's Mac. Both computer-use POCs are usable through real decoded viewers, independent input paths, enforced ownership, fresh image capture, and repeatable container startup. This report is independent of the builders' test assertions. Remaining limitations and failed diagnostic attempts are retained below.

## Open the demos

| Surface | URL |
|---|---|
| Sidekicks workbench | http://127.0.0.1:7790/ |
| noVNC viewer | http://127.0.0.1:7901/ |
| Neko gated viewer | http://127.0.0.1:7902/ |
| Neko native SDK/data-channel viewer | http://127.0.0.1:7902/?mode=native |
| Adapter APIs | http://127.0.0.1:7791/health and http://127.0.0.1:7792/health |

Project: `/Users/kevinmartin/Development/kevinmartin/sidekicks`. Independent evidence is under `artifacts/independent-qa/`; executable checks are under `qa/`.

## Acceptance matrix

PASS means executed and observed. EMULATED describes translated wheel/drag input, not physical trackpad gestures. Viewers were driven in new headless Chrome processes; no user's browser tabs or native Mac cursor were controlled.

| Check | noVNC | Neko | Independent evidence |
|---|---|---|---|
| Actual decoded live stream | PASS, 1280×720 RFB canvas | PASS, 1280×720 VP8 video and open data channel | `novnc-viewer-basic.json`, `neko-native-basic.json`, viewer screenshots |
| Browser viewer text, Control/Shift, click/right/double, drag/drop and release | PASS | PASS on gated and native routes | Fixture event snapshots and changed viewer images |
| Separate normalized automation path | PASS, fixed argv to X11 | PASS, HTTP to Neko v3 control websocket; exact mixed-case ASCII/punctuation | Basic reports; no host shell route |
| Vertical/horizontal wheel | PASS on final x11vnc0.9.17 | PASS on gated and native paths | Basic reports; wheel translation is EMULATED |
| Four desktop corners after viewer resize | PASS | PASS | Actual independent XQueryPointer coordinates; ±2px tolerance |
| Raw unleased viewer packets blocked | PASS, viewOnly deliberately bypassed | PASS, direct SDK data-channel keys sent without lease | Fixture unchanged; noVNC dropped-packet counter increased |
| Copied public session cannot recover token; stale/expired/competing owner rejected | PASS | PASS | 16 contract checks per final backend |
| Native human lease excludes concurrent bot/HTTP input | PASS through RFB lease gate | PASS; native lease HTTP input and competing bot both409 | `neko-native-basic.json` |
| Held key/button release on takeover, authenticated rejection, blur and disconnect | PASS | PASS | Independent XQueryKeymap/XQueryPointer before/after; foreign invalid input preserves legitimate state |
| Generic takeover while actual xterm is active | PASS | PASS | Unique printf output read from disposable container; controller send counters unchanged after ACK |
| Future queued bot commands cancelled | PASS | PASS | In-flight request409, stale request409, queued suffix absent |
| Generic application queue flush | UNTESTED / not promised | UNTESTED / not promised | Ownership stops new sends; previously injected events may be processed later |
| Fresh direct capture and workbench proxy | PASS, PNG | PASS, JPEG | Unique marker/color agrees with decoded viewer at1280×720 and640×360 |
| Repeated start/restart, fresh fixture and theme configuration | PASS | PASS | `novnc-restart.json`, `neko-restart.json`; fresh markers and unchanged config/wallpaper hashes |
| Actual wallpaper, panel and terminal | PASS, personally inspected | PASS, personally inspected | `*-appearance-wallpaper-terminal.png`; native terminal command output proof |
| Synthetic received audio | No configured audio channel | PASS, received remote PCM | `neko-native-audio.json`; peak RMS0.04253, speaker output suppressed |
| Physical audio listening | UNTESTED | UNTESTED | No physical listening performed |
| Physical Mac pinch/swipe | UNTESTED; native route unsupported | UNTESTED; native route unsupported | Wheel/drag are EMULATED; no native multitouch claim |
| Control-wheel zoom | UNTESTED independently | UNTESTED independently | Implemented claims are not promoted to tested |

Workbench: seven independent checks PASS, including Host/Origin/bearer rejection, unsupported host-command route, live browser rendering, explicit intent-only posting, and shared fixture behavior. Direct browser fixture tests are kept separate from remote desktop evidence.

## Sequential viewer measurements

Both owners confirmed idle before the final run. Tests ran sequentially at03:36–03:37UTC, from fresh reloaded copies of the same synthetic fixture. Each Linux desktop was1280×720, native linux/arm64, capped at2 CPUs/2GiB; the viewer was the same installed Chrome at1440×1040, DPR1. Background user services were left untouched.

| Viewer route | n | Median | p95 | Maximum | Minimum | Misses |
|---|---:|---:|---:|---:|---:|---:|
| noVNC RFB, text preset | 30 | 98.05ms | 115.8ms | 117.7ms | 52.6ms | 0 |
| Neko native SDK WebRTC data channel, VP8 default | 30 | 62.05ms | 96.2ms | 96.9ms | 43.4ms | 0 |

Raw samples: `novnc-viewer-measure.json` and `neko-native-measure.json`. **Statistical correction after parent review:** the original JSON `medianMs` fields are nearest-rank 50th percentiles (the lower middle for n30), not standard even-n medians. The standard median averages sorted zero-based indices14 and15. noVNC:97.69999885559082 and98.39999961853027, giving98.04999923706055ms. Neko:61.69999885559082 and62.39999961853027, giving62.04999923706055ms. The table rounds these to two decimals; p95 remains nearest-rank. No observations were rerun or changed. `statistics-correction.json` records the method, middle values and SHA-256 provenance. The historical JSON summaries remain untouched; future checks now use `qa/statistics.mjs`.

Preserved raw file hashes:

- noVNC: `227f04670a4803a8b8f2e2e49c545a2974fcb08a8df8cb9b049de2f7b59718c5`.
- Neko: `ca0c15f6aa381c438c824fb096c3ecd354aaaf32fea929fb0f8f87822ab81d4f`.

 Timing uses one viewer-browser `performance.now()` clock: captured mousedown to the first observed decoded change in the standard synchronous click-counter row. At least12 pixels must differ by more than45 in any RGB channel. The supplemental marker timers are excluded. Sampling uses requestAnimationFrame: the originally recorded nearest-rank q50 interval is16.4ms/16.6ms; p95 interval18.3ms/16.8ms for noVNC/Neko respectively. Raw per-poll intervals were not retained, so their standard medians are not reconstructed.

Guest configurations differ materially. noVNC ran Debian Chromium 156.0.8078.47 (`156.0.8078.47-1~deb12u1`) with browser chrome, an observed 1280×595 application viewport inside the1280×720 desktop, Xvfb and x11vnc0.9.17/RFB text quality9/compression2. Neko ran Debian Firefox ESR 153.5.1 (`153.5.1esr-1~deb13u1`) in kiosk mode with a 1280×720 application viewport, Xorg and VP8/WebRTC configured 30fps (interactive target 2.2Mbps, two encoder threads). Versions come from recorded `artifacts/novnc/package-versions.txt` and `artifacts/neko/build.log`; viewport sizes come from the saved fixture snapshots. The renderers, guest viewport/chrome, capture/encoder configurations and telemetry cadence differ. Matching desktop resolution and resource caps does not isolate a protocol effect. These are **whole-POC route observations, not an isolated protocol benchmark or causal latency ranking**.

These are local POC observations, not physical human latency or a universal protocol ranking. They include input handling, application rendering, encoding, transport, decoding and polling. Browser/encoder implementations and telemetry cadence differ. Controlled motion, sustained quality, resource-cost profiling and WAN tests remain UNTESTED. The earlier98.3/132.1/151.7ms noVNC q50/p95/max run is preserved as `novnc-viewer-measure-provisional.json` and is not the final comparison.

## Ownership and application settling

Generic enforcement is distinct from undoing input already delivered to an app. Final noVNC uses small paced chunks and an explicit pending/ready transition with bounded time settling. Its API says `applicationQueueFlushed:false`; no fixture-specific ACK is required. Neko checks generation/lease before future sends and transfers native host ownership to the leased viewer.

The original noVNC zero-delay test failed the strict no-post-ACK-app-processing criterion: takeover returned at02:56:13.045Z, but already injected `q` events were processed through02:56:13.561Z; observed text grew16→160. This is preserved in `novnc-post-ack-backlog-original.json` and its fixture snapshot. It does not prove new injection after ownership revocation. On the final paced noVNC run, ACK took323.8ms, text stayed8→8 and no later bot text event was observed. This observation is not a generic app-queue-flush guarantee.

The latest Neko fixture diagnostic retained one text event timestamp36ms after ACK, with text16→16 at subsequent observations. Its strict zero-later-app-event criterion remains FAIL in the raw report; generic cutoff, cancellation, held release and native terminal usability PASS. Wall-clock event timestamps are diagnostic, separate from the single-clock viewer latency measurements.

Actual xterm takeover also ran independently of the fixture. Human printf output matched a unique expected marker in the disposable container. noVNC injection commands stayed3→3 while human RFB input continued; Neko controller frames stayed147→147 while native human input continued. Ownership-request durations for those individual runs were322.8ms and5.5ms respectively; these are HTTP/control timings, not decoded-viewer latency.

## Captures and visual evidence

| Final direct capture | Dimensions | Bytes | HTTP duration | Unique marker |
|---|---:|---:|---:|---|
| noVNC scrot/X11 PNG | 1280×720 | 212,175 | 74.1ms | SK-mvopgj97 /32 |
| Neko X11 JPEG | 1280×720 | 164,152 | 37.1ms | 6b4768d1 /32 |

The workbench half-scale proxies returned640×360 with source dimensions1280×720. Their unique marker colors matched the actual decoded stream and direct capture. Mapping is `desktopX=imageX×2`, `desktopY=imageY×2`; full-size capture is1:1. These durations are single HTTP capture observations, not human latency. No model API or pretend model loop was used.

Sanitized screenshots personally inspected: `workbench.png`, `novnc-viewer-measure-changed.png`, and `neko-native-measure-changed.png`. Full desktop images, fixture event JSON, terminal/wallpaper images and raw measurements remain local in the evidence directory.

## Preserved failed attempts and limits

- Packaged x11vnc0.9.16 ignored horizontal RFB buttons. The official0.9.17 native source variant fixed the observed failure; final independent horizontal tests PASS.
- Early QA startup checks read RFB before its first decoded frame or serialized non-enumerable browser video counters as an empty object. Test waits/serialization were corrected; attempts remain saved.
- A noVNC appearance restoration helper mapped a hidden Chromium utility window; that attempt's later marker check failed. The corrected actual panel-button restoration and subsequent captures PASS; the old attempt is retained.
- Neko appearance contains valid terminal takeover/output/counter and wallpaper proof, followed by a failed extra window-restoration/marker step. The helper selected the wrong desktop task. That aggregate report is not relabeled PASS. A separate coordinated restart and final live capture/timing establish the restored clean browser state.
- Native Neko's SDK maps Mac Command to Linux Control. Plain automated uppercase characters without physical Shift are not equivalent to physical Shift typing. Explicit Shift+A PASS; the final viewer supplies Linux-terminal/minimize buttons. Normalized Neko text explicitly supports printable ASCII, LF and Tab; other Unicode returns422. Layout/IME coverage remains UNTESTED.
- The first audio route used a MediaElement source and saw zero. The corrected remote MediaStream AudioSource test, on a clean fixture, detected the synthetic tone with peak RMS0.04253. RTP `totalAudioEnergy` remained0, so RTP counters alone were not accepted as tone proof. Video stayed muted and the analyser used a zero-gain sink; physical listening remains UNTESTED.

## Local POC security boundary

Read-only runtime evidence verifies native linux/arm64 images and all host-published HTTP/WS services bound to127.0.0.1: workbench7790, adapters7791/7792, viewers7901/7902; Neko UDP59020–59039 is also loopback-only. Raw VNC and raw Neko APIs remain internal. Container-internal listeners may bind their namespace interfaces. Actual Docker bindings and host listeners, not only scripts, are recorded in `runtime.json`.

No task container has privileged mode or host mounts. Each desktop has2 CPUs/2GiB and512MiB shared memory. noVNC runs as desktop with capabilities dropped and Chromium `--no-sandbox` inside that disposable container. Neko's supervisor is container-root; desktop, server and adapter programs run as neko. The workbench runs as65532 with a read-only root filesystem. This is not production multi-tenant hardening.

Code review and black-box checks found/repaired copied-session token recovery, missing Host validation, malformed-input cleanup and stale-send risks. Arbitrary shell events are rejected; the workbench has no unrestricted host shell API. The only verification shell commands ran in disposable Linux xterm windows with bounded synthetic output. No user home mounts, model calls, secrets/spending, external publishing, remote pushes or OS security changes were used. Scout, Builder and Reviewer are demo intent records, not connected external bots or autonomous agents; terminal/browser framework interfaces remain contracts.

## Reproduce

From the project root, with the three demos running and no human lease held:

```sh
npm ci --prefix qa
node qa/source-review.mjs
node qa/contract.mjs novnc --active
node qa/contract.mjs neko --active
node qa/workbench.mjs
node qa/desktop.mjs novnc basic
node qa/desktop.mjs neko basic
node qa/desktop.mjs neko basic --native
node qa/desktop.mjs novnc appearance
node qa/desktop.mjs neko appearance --native
node qa/restart.mjs novnc
node qa/restart.mjs neko
node qa/desktop.mjs neko audio --native
node qa/desktop.mjs novnc measure
node qa/desktop.mjs neko measure --native
node qa/runtime.mjs
```

Reserve exclusive task-desktop windows for active tests/restart; run measurements sequentially with builders idle. The appearance helper's Neko restoration limitation above is preserved. This Mac executor requires scoped escalation for Docker/loopback/browser access; sandbox EPERM is not a backend failure. Startup commands are `bash scripts/start.sh all` and `bash scripts/status.sh`. Builds depend on Docker Desktop, the pinned official sources/packages and existing Chrome; no new runtime installation is needed. Core verification has no remaining dependency blocker. Physical gestures, physical listening and production harness features remain outside the verified scope.

## Library visual evidence

The three requested synthetic screenshots were saved successfully for direct parent review, with no public sharing:

- Workbench: `libfile_4fa3387929f88191a3010e6546cb35b5` (`workbench.png`).
- noVNC actual viewer/desktop/fixture: `libfile_dbc3750d92f08191b298043b55262348` (`novnc-viewer-measure-changed.png`).
- Neko actual native viewer/desktop/fixture: `libfile_8e956507b710819183696954cf4d90f4` (`neko-native-measure-changed.png`).

The prepared-transfer helper reported its preparation capability unavailable before any upload. The supported host-file Library create path then saved all three successfully, and local Library identity metadata was applied successfully. This was a transfer-path limitation, not a remaining deliverable blocker.
