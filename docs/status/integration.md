# Integration status

## Current acceptance snapshot

| Area | Latest evidence | Outstanding |
|---|---|---|
| Workbench | Go race + final 7/7 independent checks PASS; fresh-marker half-scale captures PASS; recorded snapshot and live readiness verified | No implementation or transfer blocker; final Library screenshot v2 and corrected report v1 saved |
| noVNC | Independent contract 16/16, basic 14/14, appearance 5/5, restart 2/2 PASS; FINAL sequential n=30, standard median 98.05 ms / p95 115.8 | Physical gestures, quality/resource studies and wider compatibility remain untested |
| Neko | Contract 16/16; generic native-terminal/restart/audio PASS; FINAL native n=30, standard median 62.05 ms / p95 96.2; literal ASCII on 3 routes PASS | Original app-delivery diagnostics and appearance-helper failures retained; physical input, quality/resource variants untested |
| Physical Mac gestures/listening | UNTESTED | Requires physical human validation |

The previous noVNC horizontal-wheel failure supersedes any broad scroll PASS inferred from the builder's earlier suite until the new server regression passes. The original 516 ms post-ACK application-event backlog remains evidence, while generic injection cutoff/held release are separate guarantees. Default takeover must work in arbitrary native apps; fixture fences are optional diagnostics. Independent acceptance and builder tests remain separate.


## Implemented

- Go workbench, TypeScript adapter boundaries, live stack health/control/capability polling.
- Same-origin + ephemeral bearer mutation boundary, fixed loopback backend allowlist, normalized event validation, adapter conflict preservation.
- Fresh bounded PNG/JPEG proxy: decode metadata, maximum requested 1920×1080 / 4 MiB; visible capture duration is explicitly HTTP duration.
- Control desk for leases, stop/release, text, modifiers, pointer/click/right/double click, drag, scroll.
- Human viewer launch acquires lease and carries token/session to adapter viewer.
- Shared synthetic fixture with event evidence, drag/drop, scroll, native gesture observations, optional synthetic tone.
- Start/stop/status scripts; local Go caches avoid writes outside workspace.
- In-memory named shared-thread intent demo, clearly labeled. No model calls or host shell API.

## Executed checks

| Check | Result | Evidence |
|---|---|---|
| Go build + race-enabled boundary tests | PASS | `GOCACHE=$PWD/.run/go-cache GOPATH=$PWD/.run/go go test -race ./harness/workbench` (2026-10-26 UTC) |
| Cross-origin and missing-bearer mutation rejection | PASS | TestMutationsRequireSessionAndSameOrigin |
| Invalid events, control owners, lease length | PASS | TestInputValidation / TestControlValidation |
| Image payload size, dimensions, format | PASS | TestScreenshotRejectsOversizeAndBounds |
| Adapter 409 ownership conflict preserved | PASS | TestProxyPreservesAdapterConflict |
| Frontend JS syntax | PASS | `node --check harness/workbench/web/app.js` |
| Workbench visible in task-owned Chrome tab | PASS | CUA tab 721256558, title Sidekicks · Computer Lab |
| Shared-thread UI posts intent | PASS | Visible Grok Bot intent at 2026-10-26T02:20:42Z; no execution implied |
| Local fixture browser typing | PASS | CUA tab 721256561, visible `INTEGRATION-20261026`, 2026-10-26T02:21:02Z |
| Local fixture right/double click | PASS | Visible counts right=1, double=1, clicks=2; event log trusted=true |
| Remote POC streams / input / restart / latency | UNTESTED by integration | Separate builder and independent QA ownership |
| Physical Mac pinch/swipe / physical listening | UNTESTED | Requires physical human interaction |

## Environment limitations and resolution

Normal Docker socket access was sandbox limited, not denied by automatic review. Supported `require_escalated` inspection and startup of existing Docker Desktop were approved. Engine is native ARM64 and ready. Local socket-based Go tests also required supported escalation and passed.

An initial detached workbench process was reaped by the managed executor after startup. It was replaced by a foreground managed terminal session (21794, refreshed at 2026-10-26T02:23:13Z). Ordinary terminal start scripts remain available. No launch daemon installed.

`git init` could not write `.git` under the workspace's read-only Git metadata rule. No Git initialization was needed for the runnable lab; no unrelated files or prior work were modified.

Shell loopback HTTP probes also require executor escalation in this environment; an un-escalated connection refusal is not sufficient to infer service failure. Escalated `curl /health` and `lsof` verified PID 93077 bound to 127.0.0.1:7790.

## Durable lifecycle and independent evidence

The foreground host server was replaced with Docker container `sidekicks-workbench`. It runs healthy on 127.0.0.1:7790, restart `unless-stopped`, nonroot/read-only/no host mounts, limited to 0.5 CPU / 128 MiB. `scripts/workbench.sh start|rebuild|stop|status` manages only the labeled task-owned container. Adapter Host validation stays enabled; the HTTP proxy preserves the public loopback Host while using Docker Desktop transport.

Temporary fixed-deadline idle-sleep inhibition is active until 2026-10-26T10:30:58Z, then self-removes. `scripts/keep-awake.sh status|stop` and root stop command expose cleanup. Forced sleep, lid closure, logout and shutdown remain outside that guarantee.

Independent verifier reports **7 workbench checks PASS**, including isolated browser rendering, screenshot, mutation bearer+Origin+Host, absent host-shell route, explicit intent-only thread, and shared synthetic fixture behavior. Evidence: `artifacts/independent-qa/workbench.json`, `workbench.png`, `shared-fixture-browser-events.json`, `shared-fixture-browser.png`. This evidence verifies the workbench and local fixture, separately from remote POC input.

Latest integration Go `-race` suite passed again after Docker routing/source-coordinate changes. `node --check` and `bash -n scripts/*.sh` passed. Current noVNC adapter health and capabilities pass through the container proxy; Neko image is still building at this milestone. Independent backend acceptance proceeds separately.

At 2026-10-26T02:34:59Z, CUA confirmed **both noVNC and Neko API ready cards** in the final containerized workbench, with actual linux/arm64 runtime reports, noVNC human link, Neko human link and separate native diagnostic link. This proves backend routing and UI readiness only; Neko decoded streaming/input remains pending builder/independent execution. The task-owned Chrome workbench tab is marked to remain open for review. The temporary direct fixture tab was closed; user tabs were not inspected or closed.

## Neko stream evidence reviewed — 22:38 New York

Neko builder reported its first actual decoded WebRTC stream and saved `artifacts/neko/smoke.json` and `viewer-smoke.png`. Integration personally inspected the saved synthetic desktop screenshot and the media stats: stream connected, inbound VP8 video 1280×720, 9 decoded frames, open RTC data channel, and a selected UDP candidate at 127.0.0.1:59023. This is actual decoded-media evidence from the builder's fresh task-owned headless Chrome run, reviewed by integration; it is not a new independent black-box run or a latency/quality benchmark. Audio playout sample count was zero in this snapshot, so audio remains unverified.

The screenshot visibly contains the shared input fixture and its event clock. Builder gated/native input and takeover tests are now running; independent acceptance follows its exclusive handoff. No integration input or capture request was sent to the desktop during that window. Neko baseline is native ARM64 source-built 3.2.0; first startup configuration issues were repaired by its owner. Viewer: http://127.0.0.1:7902/ ; native diagnostic: http://127.0.0.1:7902/?mode=native .

## noVNC baseline completed by builder — 22:44 New York

Integration reviewed `pocs/novnc/status.json`, `artifacts/novnc/e2e.json`, and `artifacts/novnc/restart.json`. The builder's saved E2E run contains **23 PASS checks**. Its independently exercised browser RFB path and normalized HTTP path are kept distinct. Evidence covers typing/modifiers, click/right/double click, viewer drag/drop, wheel translation, stale/unleased input rejection, cancellation after takeover, native XQueryKeymap/XQueryPointer confirmation of held-input release, two-viewer ownership, simulated blur/disconnect/reconnect/expiry/invalid-input cleanup, and coordinate mapping after viewer resize. A separate restart artifact lists five successful restart/theme/launcher checks; builder framing tests also report five PASS.

Limitations remain explicit: normalized pointer drag and zoom emulation were not separately asserted in the builder suite; native multitouch and audio are unsupported in this RFB/X11 baseline; physical Mac input remains untested. Independent QA has the exclusive noVNC input/restart window from approximately 22:44 to 22:59 New York; integration sends no desktop input or capture requests during it.

Builder same-clock synthetic click-to-decoded-marker measurements (not physical-human latency or a matched backend benchmark): text preset n=35, median 59.5 ms / p95 90.5 / max 96.9; motion preset on the same click workload n=35, median 60.9 / p95 87.7 / max 230.1. Both report zero failures. requestAnimationFrame sampling interval median 16.5 ms / p95 18.4 / max 19.7. Full samples and method are in the builder E2E artifact; no cross-stack winner is inferred.

The noVNC adapter now serves its tested capability labels from its status artifact, so the workbench can display them without converting API availability into test proof. Independent QA acceptance remains a separate result.

## Neko input paths completed by builder — 22:46 New York

Integration reviewed the builder's `artifacts/neko/e2e.json`, `gated-fixture.json`, and `native-fixture.json`: **8 aggregate E2E checks PASS**, separately exercising (1) browser input through HTTP lease → v3 control WebSocket and (2) official SDK overlay → WebRTC data channel → X11. Both fixture snapshots show typed/replaced text with uppercase A, click=3, double=1, right=1, drop=1, scroll y=737, and no held keys. Normalized automation was independently exercised in the same builder suite.

The saved run also passes unleased native host rejection, copied-session/forged-token/Origin/Host rejection, queued bot cancellation on human preemption, simultaneous human/agent acquisition ending in human ownership, and a fresh bounded 640×360 JPEG (source 1280×720, 47,997 bytes). Builder reports seven safety unit tests PASS. These are builder results reviewed by integration, with independent QA acceptance still pending.

Lifecycle cases (blur, disconnect, raw native input after revocation, expiry, resize/corners, theme/terminal and restart) are next. Neko decoded-frame timing remains unmeasured until the noVNC QA window ends at approximately 22:59 New York. The E2E `measurements` object is empty. Its ten screenshot request timings are a distinct capture path and must not be used as viewer-input latency.

Audio stats now show Opus packets/sample playout, but total audio energy is zero in this run; a received synthetic tone or physical listening is not established by that evidence.

## Open noVNC QA findings — 22:54 New York evidence

Independent `artifacts/independent-qa/novnc-viewer-basic.json` explicitly records **horizontal viewer wheel FAIL**: scroll x remains 0 when horizontal delta is sent separately. The earlier builder test labeled vertical/horizontal wheel had an x=0 snapshot and did not establish horizontal delivery. Vertical wheel, browser text/modifiers/click/right/double/drag, resized-corner mapping and raw unleased RFB rejection have independent PASS observations in that artifact.

The builder reports a diagnosis in x11vnc 0.9.16's five-button limit and is preparing an official native 0.9.17 source-build variant. That diagnosis/fix is a builder report awaiting a fresh regression; integration does not claim the replacement passed. The running desktop remains undisturbed during QA safety checks. Builder was asked to expose the partial/failing scroll status live until confirmed repaired.

The same saved independent run also has a **takeover cancellation FAIL** (`160 !== 16`) with a fatal assertion. Cleanup confirmed no actual held keys/buttons. QA and builder were both informed; they are checking whether the difference is queued execution after acknowledgment or asynchronous fixture-snapshot freshness. It remains unresolved acceptance evidence, with no assumption that the implementation or test is at fault until the timeline is reconciled. No integration desktop inputs/captures were sent.

## Confirmed noVNC post-takeover backlog — 22:56 New York

QA reconciled the wall-clock event timestamps and reran the full basic suite: **12 independent PASS + 2 FAIL**, with no fatal error. The latest `novnc-viewer-basic.json` has takeover acknowledgment at 2026-10-26T02:56:13.045Z and the last bot-generated `text-input` event at 02:56:13.561Z: about **516 ms after acknowledgment**. The first observed text length was 16; the settled length was 160. This is confirmed post-acknowledgment application-event delivery, not merely uncertainty from snapshot freshness. The earlier fatal attempt remains preserved as attempt 5.

The finer distinction matters: remaining HTTP commands cancel, stale input returns 409, and actual XQueryKeymap/XQueryPointer held-input release passes. Those checks do not prevent application processing of text already buffered downstream. Horizontal viewer wheel and this buffered delivery are the two outstanding independent failures. Builder has the exact timeline for repair; integration requested that live takeover capability status remain failing/partial until a fresh regression passes.

Independent PASS observations now also include the workbench screenshot proxy: a fresh unique marker agrees with the decoded source at half scale. Blur, invalid-input release, raw transport gate, screenshot freshness and disconnect cleanup pass; final cleanup reports no owner, held keys, or held buttons. This closes the earlier pending workbench proxy-capture check.

## noVNC repair in progress — live labels corrected

Builder reports live capability labels now expose partial implementation for horizontal wheel and takeover; the 0.9.16 scroll failure and 516 ms post-acknowledgment text backlog are stated explicitly. The official native 0.9.17 server image is building without replacing the QA desktop.

Proposed input repair uses smaller paced text chunks (four characters, 20 ms per character), cancellation of remaining injection and a minimum 250 ms settling period before acknowledgment. Integration review flagged that a fixed 250 ms period alone does not prove downstream application quiescence, especially against the observed 516 ms backlog. Builder was asked to retain closed input during transition, distinguish pending/draining from human-ready, bound any wait and fail closed when its criterion is not met, and precisely label measured settling versus a proven application fence. Fresh builder and independent regressions, including immediate post-ack human text and no bot intermix, remain required. No repaired result is claimed yet.

## noVNC opt-in application fence proposed

Builder accepted that 250 ms settling alone is insufficient and is implementing a synthetic-fixture fence: revoke owner, cancel remaining dispatch, release held input, pace text, inject a reserved F24 key-up after earlier X11 injections, then wait for the fixture DOM handler to acknowledge a pending nonce with its current snapshot. Human-ready token follows actual fixture ACK plus quiet settling. A two-second timeout fails closed/pending within the workbench's five-second HTTP timeout. Native applications without this opt-in fence cannot claim generic application drain.

Integration review requested nonce + ownership-generation + fixture-page binding, serialization of overlapping handoffs, stale/late-ACK rejection and protection against an old F24 event satisfying a newer pending challenge. Timeout/reload/stale-ACK/concurrent-handoff regressions were requested. This remains a proposed repair, with no passing post-acknowledgment regression claimed yet. The native x11vnc 0.9.17 build is still separate work in progress.

## Scope correction: generic native-app takeover remains default

Parent clarified that default human leases must never depend on a synthetic fixture acknowledgment. Integration forwarded the correction unchanged to both builders and QA and rewrote the contract accordingly. Earlier proposals for mandatory fail-closed fixture readiness are superseded. A synthetic fence may only be an optional diagnostic; normal terminal/browser/native-window control must remain practical.

noVNC owner confirms no fixture-fence code was deployed or required. Its generic repair uses four-character text chunks with 20 ms pacing, ownership revocation before cleanup and a bounded 250 ms agent settling interval before handing out the new token. It exposes pending/ready, transition metadata, injection-cutoff timestamp/count and `applicationQueueFlushed:false`. This enforces the generic injection boundary while explicitly declining a generic application-queue flush guarantee. The 0.9.17 native build completed; fresh regression is next.

QA acknowledged the scope correction and will independently exercise takeover with an actual Linux terminal active. The original 516 ms observed application-event backlog and its original FAIL criterion are preserved; cancellation of future commands, stale-token rejection and actual held-input release remain separate PASS observations. Neither a scope correction nor a fixed settling interval erases that historical measurement.

The workbench generic-control clarification and visible pending/ready UI were deployed through its task-owned Docker rebuild. Frontend JS syntax check passed. The UI blocks its own input dispatch while a control request is pending and labels readiness as input ownership. No fixture challenge is sent by the workbench, and no desktop was restarted or controlled by integration for this update.

## Neko final-image QA handoff — 23:10 New York

Neko builder handed the final stable native ARM64 3.2.0 image exclusively to independent QA at approximately 2026-10-26T03:10Z. It reports healthy with owner null and a fresh 1280×720 fixture. Integration inspected `handoff-state.json`: empty text, zero click/drop counters, no held keys, new fixture instance and the expected desktop geometry. No integration desktop input, build or benchmark will compete with QA's window.

Reviewed `artifacts/neko/lifecycle.json` has four PASS cases: raw native data-channel input rejected after host revoke; native modifier released on blur/disconnect with reconnect; expiry/invalid-lease held-input cleanup; both viewer routes mapping all four desktop corners after resize (eight observations). Its overall status remains FAIL because the subsequent terminal-title assertion failed in that saved run. Builder reports the final title/profile repair applied before handoff, but independent native-terminal/theme/restart acceptance is still required. The historical failure is preserved, including `lifecycle-before-terminal-title-fix.json`; this document does not relabel the whole lifecycle artifact PASS.

Neko exposes `GET /metrics` with `controllerInputFramesSent` for generic injection-cutoff observation. Default takeover has no fixture ACK dependency, and documents that an application may process already-delivered events after ownership transfer. The README includes both viewer modes, run/test commands and exclusive-measurement requirements. Builder's 8 E2E and 7 safety-unit PASS results remain distinct from final independent acceptance.

## noVNC final 0.9.17 builder regression reviewed

Integration reviewed the new `artifacts/novnc/e2e.json`: **24 builder E2E PASS**, including separate horizontal and vertical wheel assertions on both viewer and normalized routes. The paced takeover check observes 650 ms after ACK with no later bot text or human/bot intermix in that run. It records injection cutoff at 2026-10-26T03:09:10.385030Z, 270.339 ms measured settling and `applicationQueueFlushed:false`; this is bounded observed behavior, not a generic downstream-flush guarantee.

`native-takeover.json` has **3 PASS checks** with an actual xterm active: generic human acquisition without fixture acknowledgment, queued agent request returning 409, and immediate viewer keyboard input in the native terminal after readiness. Acquire duration was 282.543 ms. Integration personally inspected `native-takeover.png`; the active “Sidekicks - disposable terminal” visibly contains the unique `# native-human-mvooieif` marker. The same run reports a 252.698 ms bounded settle and explicitly no application queue flush guarantee.

`restart.json` has **5 PASS checks** for a fresh fixture/decoded stream after restart, persisted wallpaper/panel and working terminal/browser launchers. The previous 0.9.16 evidence is retained under `artifacts/novnc/baseline-0.9.16/`, including its wheel failure and the separately preserved independent 516 ms post-ACK backlog. The final 0.9.17 + pacing/settling image is live and handed back to QA with a fresh single fixture. Capability labels deliberately remain partial pending independent final-image regression. Integration sent no desktop input or capture requests during handoff.

## Final noVNC independent regression reviewed — 23:20 New York

Latest `novnc-viewer-basic.json` has **14 independent PASS, zero FAIL, no fatal**. Takeover acknowledgment took 323.758 ms; observed text length stayed 8 → 8 and `postAckBotEvents` is empty. Final cleanup reports no owner, actual held keys or actual held buttons. Both wheel axes now have passing final-image evidence. The original horizontal-wheel and 516 ms post-ACK failures remain preserved under baseline/original artifact names.

Independent `novnc-viewer-appearance.json` separately passes native-terminal takeover with exact synthetic printf output, queued agent request 409, no held input, and injection command count stable at 35 after readiness. Native handoff HTTP duration was 326.017 ms with `applicationQueueFlushed:false`. Terminal/desktop screenshots were saved for review. That same appearance-phase attempt later timed out waiting for a fresh fixture capture marker; its FAIL/fatal is retained and QA was asked to explain it in the final synthesis. It is not silently relabeled by later passing captures.

Additional artifacts already completed after the builder's milestone: `novnc-restart.json` has **2 PASS** (repeatable start; restart readiness/fresh marker/theme persistence) with restart-to-ready HTTP duration 2,592.343 ms. `novnc-viewer-measure.json` has **30/30 successful standardized decoded-counter samples**, zero failures: median 98.3 ms, p95 132.1, max 151.7. Sampling interval median 16.6 ms, p95 17.6, max 19.3. This uses the viewer browser's same performance.now clock, mousedown to first decoded change in the standard synchronous fixture click-counter row, at 1280×720. It is distinct from earlier builder marker/preset timings and physical-human latency. No cross-stack winner is inferred.

The final measurement run also independently verifies fresh direct 1280×720 PNG and workbench 640×360 proxy capture, the same unique marker and correct ×2 source-coordinate mapping. HTTP capture durations are separately labeled. Final independent report/synthesis is still being completed; integration sent no POC input or capture request during these runs.

## QA evidence qualification and latest native-route results

The noVNC 98.3 / 132.1 / 151.7 ms result is **PROVISIONAL**, saved as `novnc-viewer-measure-provisional.json`: Neko polish/build idleness was not confirmed for that run. Thirty samples succeeded with the standard counter detector, but a matched sequential repeat of both backends with confirmed idle owners is still required. Earlier entries describe the observed run, not a controlled cross-backend benchmark.

QA identified the noVNC appearance timeout as its window-restoration helper mapping a hidden Chromium utility window. That attempt and screenshots are preserved as `novnc-viewer-appearance-attempt1.json`. Integration inspected the corrected current appearance artifact: **5 PASS, zero FAIL**, following restoration through the actual Linux panel task button. Native-terminal takeover, exact output and stable injection-count proof remain separate passing evidence; post-restart capture is separately retained.

Reviewed Neko independent artifacts: `neko-native-basic.json` has **15 PASS**, no fatal, no post-ACK bot text events, text length 16 → 16. `neko-viewer-basic.json` has **13 PASS + 1 FAIL** under the strict application-delivery criterion: one text event timestamped 1 ms after acknowledgment, with first/settled length both 17. This observation is preserved and does not imply a generic downstream queue-flush promise; new-injection cutoff remains a separate boundary to verify.

The current Neko native appearance attempt fails literal uppercase output (`HUMAN_TERMINAL` requested through the browser typing helper, lowercase received). Explicit physical-style Shift+A passes; QA and owner are checking the route/API case semantics. Do not conflate that helper failure with a proven physical modifier failure or relabel the native-app test accepted yet. Its earlier title failure remains preserved separately.

The first optional decoded-audio test also records FAIL (RMS/peak energy zero) despite an Opus track/sample flow. QA is preparing a direct MediaStream check to distinguish the observer path from actual received tone energy. Audio remains unverified; physical listening and Mac trackpad gestures remain untested. No integration desktop activity was performed during these tests.

## Final idle handoff and supplemental noVNC stress reviewed

Integration reviewed `artifacts/novnc/stress.json`: **4 builder stress rounds PASS**, covering human input through viewer RFB and normalized API both unloaded and with one busy CPU for three seconds inside the two-CPU disposable desktop. Every round records queued/stale input 409, no post-ACK bot events and no human/bot intermix. Handoff request duration ranged 274.93–295.96 ms including bounded settling; `applicationQueueFlushed:false` remains explicit. Four runs are supplemental stress evidence, not a latency benchmark or generic application-flush proof.

The sampled viewer evidence clip `interaction-evidence.mp4` contains 51 screenshots encoded at nominal 5 fps. Its file codec does not describe the VNC transport. Preset negotiation evidence remains in `x11vnc-e2e.log`. The original baseline failures are preserved.

noVNC owner restored a fresh fixture and declared IDLE at approximately 23:28 New York, with no further activity before QA's final sequential measurements. QA is waiting for Neko final polish and IDLE confirmation, then plans matched runs and sanitized review screenshots. Integration remains documentation-only; no desktop input/capture/build/test activity competes with that window.

QA clarified Neko's earlier native terminal uppercase assertion: its SDK `keyboard.type` helper omitted physical Shift for uppercase values, while explicit Shift+A already passed. Actual native xterm command execution was confirmed; a lowercase marker avoids misrepresenting that helper as physical input. Literal HTTP text case/punctuation is a separate final regression (owner reports its driver path handled). Preserve the earlier failed attempt and do not treat helper semantics as either a physical-keyboard failure or proof that literal-text APIs preserve case without their own test. Neko optional audio remains under direct-track investigation.

## Neko text contract and both-owner idle confirmation — 23:30 New York

Neko owner declares IDLE at approximately 2026-10-26T03:30Z for independent native-app file proof, restart and final sequential measurements. Both desktop owners have now confirmed no further builds, input or benchmarks until QA releases the window. Integration remains documentation-only.

Integration reviewed `artifacts/neko/text-contract.json`: the exact multiline string `SideKicks QA! 123\nspaces_+[]{} @# =:;?` roundtrips unchanged on three independently exercised builder routes: gated viewer with explicit physical modifiers, native SDK data channel with explicit modifiers, and normalized HTTP text through v3 WebSocket. Unsupported Unicode receives 422 before writing and fixture text remains unchanged. Supported normalized text is printable ASCII plus LF and Tab; native layout/IME coverage is not exhaustive.

That artifact's overall status remains FAIL because a later terminal-title assertion expected a static title while xterm's live prompt changed it. The three string comparisons and Unicode rejection explicitly PASS; integration does not relabel the whole artifact. Actual xterm opened through the new Linux terminal button, but independent QA will use window class and actual command/file output rather than a changing prompt title. A minimize button also provides access to the Linux desktop without relying on a Mac-intercepted Super shortcut. No synthetic fixture ACK is required for ordinary takeover.

## noVNC worker completed; source manifest independently reconciled

The noVNC owner has completed its implementation and remains idle. Final builder counts are 24 E2E, 5 framing, 6 malformed-input cleanup, 5 restart/theme/launcher, 3 native-terminal takeover and 4 stress rounds PASS; independent basic 14/14 and corrected appearance 5/5 PASS are separately recorded above. Integration reviewed `malformed.json`: all six cases pass, including wrong field types releasing actual held X11 state and a foreign non-ASCII token preserving the legitimate owner.

Integration independently recomputed SHA256 for the nine workspace source/config files named in `source-manifest.json`; **9/9 match** the saved runtime hashes. The saved image inspection matches manifest ID `sha256:64c4e5e66e745ee817c0baecf37e8f9084f4ba30c3d988f5c5089fb351c87b0c`, architecture arm64. Supporting review: `artifacts/integration/novnc-source-review.json`. This is a read-only manifest reconciliation, not a new live-container inspection during QA's quiet window.

The final controlled comparison remains owned by QA. The earlier independent n=30, 98.3 / 132.1 / 151.7 ms run remains explicitly PROVISIONAL. Source manifests, final README and runtime limits are complete; no further noVNC builder or integration input/build activity is scheduled during measurement.

## FINAL matched sequential timing and decoded audio reviewed — 23:36 New York runs

QA completed idle-owner sequential measurements on the same task-owned Chrome and fresh F5-reset shared fixture, at 1280×720 and actual 2 CPU / 2 GiB limits per native ARM64 desktop. Integration inspected both final measurement artifacts and the sanitized runtime limits.

| Route | Successful samples / misses | Median | p95 | Maximum | Minimum | Median / p95 / max sampling interval |
|---|---|---|---|---|---|---|
| noVNC RFB WebSocket | 30 / 0 | 98.05 ms | 115.8 ms | 117.7 ms | 52.6 ms | 16.4 / 18.3 / 19.0 ms |
| Neko native SDK / WebRTC data channel | 30 / 0 | 62.05 ms | 96.2 ms | 96.9 ms | 43.4 ms | 16.6 / 16.8 / 23.4 ms |

Final raw files are `novnc-viewer-measure.json` and `neko-native-measure.json`. The viewer's same performance.now clock runs from mousedown to the first decoded change in the standard synchronous click-counter region (at least 12 pixels changing by >45 RGB-channel units), sampled with requestAnimationFrame. Supplemental marker timers are excluded. These results characterize this local synthetic setup and these two routes; they do not establish physical display/WAN latency, equal image quality or Neko gated-HTTP timing. Earlier 98.3 ms data remains preserved in the provisional file.

Neko corrected direct MediaStream audio check is now **PASS**: a generated 250 ms / 440 Hz synthetic tone produced received PCM peak RMS 0.042532. The test uses a zero-gain output sink, so speakers were suppressed and physical listening remains untested. The earlier RMS-zero observer attempt remains preserved. Fresh JPEG/proxy image captures pass independently of audio/video reception.

Final active contract checks are **16/16 per adapter**, workbench **7/7** before the last metadata display update. Neko's actual xterm generic takeover independently passed exact output, actual held-input release and a controller-send count stable at 147 → 147. Native basic's latest raw strict application-delivery diagnostic is 36 ms post-ACK (14 PASS + 1 strict diagnostic FAIL); the earlier 15-PASS attempt and gated 1 ms diagnostic are preserved. These application-processing observations are not new injection after cutoff and do not create a generic flush guarantee. NoVNC's original 516 ms failure remains preserved alongside the passing final paced regression.

Neko aggregate appearance restoration-helper failure remains visible in raw evidence, separate from passing native command/file proof and a later clean restart/fresh capture. The final independent report is being written with those distinctions.

## Final workbench verification snapshot integration

The final worker status files are copied into the workbench's read-only image (no mounts), so the UI can show recorded tested capabilities even when a live adapter intentionally keeps conservative pre-verification labels. Cards explicitly label the source as a recorded verification snapshot while polling live API readiness separately. Rebuild after changing status evidence. Root README now contains the final comparison and limits. JS syntax and shell syntax checks passed; the metadata-only workbench rebuild completed and QA is refreshing its final 7-check run and screenshot. Neither POC image, runtime nor input state was changed by integration.

Unmeasured scope remains explicit: Neko quality encoder profile, controlled idle-versus-motion CPU/memory study, WAN/network-loss/mobile coverage, general Unicode/IME/layout coverage and physical gestures/listening. One Docker CPU/memory point sample is not a resource profile. No model API calls, host-shell endpoint or full multi-bot implementation is implied by this lab.

The final workbench card/footer wording was corrected after browser inspection: route descriptions replace a stale worker summary that still said measurements were pending. Source status records are preserved unchanged. Capability rows remain explicitly recorded verification, with live API readiness separate. Final workbench is ready for QA's replacement screenshot and final seven-check rerun; no further code/service changes are planned by integration.

## Implementation frozen; final workbench accepted

No further implementation or service changes are planned. Integration inspected the post-freeze `artifacts/independent-qa/workbench.json` generated at 2026-10-26T03:52:28Z: **7/7 PASS**, zero failures. Its browser-card evidence shows both APIs ready, correct recorded-verification labels, Neko audio tested, no stale pending-measurement summary, and both owners none. QA created the independent report plus three sanitized Library screenshots and is replacing only the existing workbench screenshot with this final UI, preserving its Library ID.

The remaining setup gap was resolved through the supported approval route: `git init` succeeded locally after the earlier sandbox-only restriction. The repository is on main with no commits yet; no remote operation was performed. Prior entries describing unavailable Git initialization are historical, not a current blocker. Source, scripts and evidence remain intact.

Final independent synthesis is `docs/verification.md`. Root README contains the exact matched metrics, routes, limits and run/stop commands. Physical trackpad/listening, generic downstream app rollback, quality/resource/WAN studies and broader Unicode/layout coverage remain explicitly untested or unsupported; they do not block the local demos. Terminal/browser harness adapters and named multi-bot behavior remain contracts/intent prototypes.

## Final QA deliverables and morning readiness complete

QA completed the final passive check: both backend health responses 200, no control owner, no actual held keys/buttons, workbench 200 and both readiness flags true. Current task images are linux/arm64, published only to loopback, with no host mounts or privileged mode. No source/runtime/input work remains; implementation stays frozen.

Integration inspected `artifacts/independent-qa/library-deliverables.json`: independent report and three sanitized screenshots saved successfully; final workbench replacement succeeded at version 1 with the same Library ID. Both raw final measurement JSON files were also saved. Local identity metadata helpers all report exit 0. The unavailable prepare-transfer helper was detected before mutation; supported host-file creation/replacement succeeded. There is no artifact-transfer blocker.

Authoritative IDs are in that manifest. Report: `libfile_58d40d111d888191b7c70bf828ab7a74`; workbench v1: `libfile_4fa3387929f88191a3010e6546cb35b5`; noVNC: `libfile_dbc3750d92f08191b298043b55262348`; Neko: `libfile_8e956507b710819183696954cf4d90f4`. Raw measurements: `libfile_2b35a5d3af2c819191d06245fc9bc028` and `libfile_7c2dfa9a724081919579fbfc0fc93c38`.

Only final README/status reconciliation was performed after implementation freeze. README links local report, screenshots, raw measurements and manifest; it states exact noVNC text-preset versus Neko native route metrics and preserves scope/limitations. Services remain ready for morning with documented start/status/stop commands and the temporary idle-sleep assertion expiring at 06:30:58 New York.

## Final parent review corrections: persona names and median definition

Parent's review requested one substantive wording correction: the intent-only thread now uses Sidekicks sample personas **Scout, Builder and Reviewer**, with explicit “demo intent records, no autonomous agents or model execution.” The workbench UI, its minimal accepted-name allowlist, TypeScript wire type and existing test fixture use those names. No POC backend source or runtime was changed. External product inspirations are not represented as integrations.

Parent and QA recomputed standard even-n medians from the unchanged final raw sample arrays. Integration independently verified the middle pairs: noVNC 97.6999988556 and 98.3999996185 → 98.0499992371 ms; Neko 61.6999988556 and 62.3999996185 → 62.0499992371 ms. README now reports **98.05 ms / 62.05 ms** (approximately 98 / 62 ms), with p95 unchanged at 115.8 / 96.2 ms. Earlier 97.7 / 61.7 values were nearest-rank lower-middle q50 values, not conventional even-n medians. Earlier parent examples of 97.5 / 61.45 were themselves corrected and are not used. Raw observations are not rerun or altered.

Raw SHA256 verified by integration: noVNC `227f04670a4803a8b8f2e2e49c545a2974fcb08a8df8cb9b049de2f7b59718c5`; Neko `ca0c15f6aa381c438c824fb096c3ecd354aaaf32fea929fb0f8f87822ab81d4f`. QA owns helper/report provenance and replacement of the same Library report ID. The only planned service action is this requested workbench rebuild, followed by QA's final seven-check run and same-ID screenshot replacement. No optional tests, POC changes or further polish are planned.

## Final correction verified and frozen

QA's final post-persona workbench run generated at 2026-10-26T04:05:04Z has **7 PASS, zero FAIL**, explicitly asserting Scout / Builder / Reviewer and demo/no-autonomy wording. The same Library report ID is now version 1; the same workbench screenshot ID is now version 2. Integration read the final deliverables manifest and verified metadata helper exit 0 for both. No transfer blocker remains.

Integration read `statistics-correction.json`: standard median method averages the middle pair, nearest-rank p95 is unchanged, raw samples were not rerun or edited, and raw SHA256 values were rechecked after the Library correction. Correct medians remain **98.05 / 62.05 ms**, approximately 98 / 62 ms. Current README/table and final summaries use those values.

The final report also records important application differences: guest Chromium 156.0.8078.47 has a 1280×595 page viewport in noVNC; guest Firefox ESR 153.5.1 is kiosk 1280×720 in Neko. Both Linux desktops are 1280×720, but guest application/viewports and encoders differ. The results characterize the whole POC routes under the recorded setup, not isolated protocol performance. README now states this explicitly.

Only necessary README/status reconciliation followed QA's final return. No additional backend tests, builds, inputs, optional work or implementation changes are planned. Services and artifacts are frozen for parent review and morning use.
