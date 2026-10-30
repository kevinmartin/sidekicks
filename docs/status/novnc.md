# noVNC status

Updated2026-10-28UTC. Owns `pocs/novnc/`, this file, `artifacts/novnc/` and delegated canonical `shared/desktop/polish/xfce/`.

## Current accepted Xfce production

Accepted whole-desktop image `sha256:f8dee4d59f9bb8b26676928b80f784cd4f982a591812c0f1fe501d0b564a1b38`, `sidekicks-novnc:polish-xfce-final`, is live on Docker7901 and VM A7903: standard Xfce session, sole xfwm compositor, GTK3 Thunar/terminal, Chromium embedded controls and managed Home, supported named-user panel lock, confirmed logout and fresh process readiness. Image1,018,096,080bytes,41,146,373bytes smaller than preserved0bdd by a fresh clean recipe; historical images remain, hostdisk reclaimed0. No new comparative runtime benchmark.

Own bounded final checks PASS for Cancel app/session identity preservation, confirmed logout503, normal restart/persisted confirmation, guest/window/viewer fullscreen and actual held cleanup, and browser-close-only readiness. Earlier broader clean-r2 app checks passed individually; combined drag/resize harness failures and CSD fixture-offset limitation remain preserved, so no full-suite-green claim. Actual held resize probe is separately recorded. Final stage Home contained a reported Chromium restore-pages bubble after deliberate logout/restart. Worker released stage one Home/unowned/heldempty and closed its viewer.

Integration promoted Docker7901 at13:44:52America/New_York on2026-10-28 (17:44:52UTC): new container `98d236dbc3321d82417ec2de98ee39aa02c0beda15fff3c191cd59f2cdf433b8`, exact frozenf8 image. Oldd590 is stopped/preserved as `sidekicks-novnc-pre-xfce-20261028`;0bdd remains untouched. Independent production Docker acceptance is8groups (3new PASS+5referenced PASS), VM A8(2new PASS+6referenced PASS), with exactly one supported normal stop/start each. Actual Cancel/content preservation, Home/New Tab, GTK3, input, held cleanup and existing LAN routes passed. Integration verified recursive raw-hash provenance; original reports remain unchanged.

The final passive audit at14:31America/New_York (18:31UTC) passed for all five production POCs: ready, one Home each, sole Xfce components, no owner/actual held input and private bindings preserved. Chromium Restore pages bubbles remain a documented restart limitation on both noVNC variants; VM A's overlay-related marker failure and successful continuation remain. Browser-close stalls/executor-session loss interrupted QA continuations; exact cleanup, partial hashes and referenced earlier PASS groups remain, with no extra guest restart. This worker performed no production runtime/input/build action. Integration stopped only released stage90c7, the Neko stage and its proxy; production remained running/healthy.

Authoritative report: [desktop-polish.md](../desktop-polish.md). Independent evidence: [production-summary.json](../../artifacts/independent-qa/desktop-polish/production-summary.json), [final-production-state.json](../../artifacts/independent-qa/desktop-polish/final-production-state.json). These are bounded functional results, not new exhaustive context-menu, physical-keyboard/mobile, audio or performance acceptance.

Current host start/rebuild scripts pin f8, reject mismatched containers without mutation, explicitly build the clean recipe as a candidate and never remove/restart/promote automatically. Eight builder mocked launcher tests PASS; supported actual production lifecycle proof is separately recorded by independent QA. Full facts, dependency footprint, commands, exact evidence and limitations: `pocs/novnc/POLISH.md`, `artifacts/novnc/polish/final-freeze.json`. Earlier sections below retain historical baseline acceptance and measurements rather than describing the current desktop stack.

## Historical transport and first-desktop variant

- Viewer <http://127.0.0.1:7901/>, adapter <http://127.0.0.1:7791/health>. `pocs/novnc/start.sh`, `stop.sh`, `status.sh`, `rebuild.sh`.
- Native Linux ARM64 under existing Docker Desktop. Xvfb1280×720, Openbox/tint2, Chromium, themed xterm, noVNC1.6.0 and active official x11vnc0.9.17 compiled natively. Source archive hashes/base digest pinned; exact packages/image/isolation recorded in artifacts.
- 2CPU/2GiB,512MiB shared memory, no host mounts, no capabilities, no-new-privileges; both published ports actually bind127.0.0.1. Raw VNC remains container-loopback-only.
- One server-enforced human/agent lease gates normalized X11 input and actual RFB viewer input. Human takeover revokes agent injection, cancels future queued commands and releases held keys/buttons. Agent text is paced4chars/chunk at20ms/char; agent preemption has250ms bounded generic settling. UI shows pending→input ready. `/control` exposes cutoff/settling and `applicationQueueFlushed:false`. No synthetic fixture ACK/F24 is required or deployed.

## Executed evidence

-5framing unit tests PASS. Final0.9.17 own E2E24checks PASS: real decoded stream, separate viewer/API input, each wheel axis, actual X11 held release, queued/stale rejection, no observed post-ACK text/intermix during650ms, resize mapping,2viewers, reconnect/blur/expiry/rejection cleanup and PNG capture.
-6additional malformed-event/Unicode-token checks PASS after the final defensive patch; malformed current-owner batches release actual Shift/button, foreign rejection preserves legitimate state.
-5restart/theme/terminal/browser-launcher checks PASS on0.9.17. Native xterm takeover3checks PASS without fixture focus; screenshot visibly contains the immediate human comment.
-Independent QA final window starts03:19UTC, with single fresh fixture/no owner. Live caps retain partial labels until that independent regression accepts the repair. No input/restart/runtime changes from this worker during their window.
-Physical keyboard/Mac trackpad/mobile tests remain UNTESTED. Native multitouch and audio transport UNSUPPORTED. Zoom emulation and separate normalized-drag assertion remain untested by own suite.

## Honest baseline and measurements

The packaged x11vnc0.9.16 baseline had two real independent defects: MAX_BUTTONS5 ignored horizontal RFB wheel buttons, and32char/zero-delay typing produced516ms of already-injected application delivery after takeover ACK. Future command cancellation/stale409/held release passed but did not imply downstream queue flush. Baseline evidence is preserved under `artifacts/novnc/baseline-0.9.16/` and `artifacts/independent-qa/`. Earlier broad own scroll assertion only checked y and was insufficient for horizontal.

Final own0.9.17 timings:35same-clock trusted mousedown→first changed decoded badge samples/preset. Text median65.3/p9580.8/max120.6ms; Motion same fixture click workload median64.1/p9587.5/max315.6ms;0failures. Polling median16.6/p9518.1/max19.0ms. Screenshot path30samples; full methods/raw results in README/e2e.json. Scrot time includes its initial PNG encoding/file write. No physical action-to-photon, WAN, video-motion or fair cross-backend benchmark claim.

`pocs/novnc/README.md` contains run/test commands, capability matrix, source citations, exact hashes, limitations and evidence links. No current dependency/install/runtime blocker remains. Independent final acceptance is pending.

## Stress completion03:28UTC

Independent final basic14/14PASS and native appearance5/5PASS. Own4 additional takeover stress rounds PASS, including2with one busy CPU inside the capped desktop; viewer and normalized human input both tested immediately afterACK.0postACK bot events/intermix observed; queued/stale409. Acquire275–296ms, including bounded settling. Evidence `artifacts/novnc/stress.json`, sampled `interaction-evidence.mp4`, and final preset-negotiation `x11vnc-e2e.log`. Live caps accepted with `applicationQueueFlushed:false`. Desktop restored fresh/idle; all input/runtime activity stopped for QA sequential measurement.

## Worker completion

NoVNC POC implementation and own/independent functional acceptance are complete. Independent basic14PASS and appearance5PASS; provisionalstandard-row30/30samples98.3/132.1/151.7ms(median/p95/max), explicitlynotcontrolled/comparable. FinalcontrolledsequentialcomparisonownedbyQAawaitsNekoidle; noVNC remainsidleandready. No install/runtime/input safety blocker remains. FinalREADME/status metadata are updated on disk; livecapabilitytested details were alreadyupdatedafterbasicacceptance.

## Sidekicks sky UX update

Image/source frozen and applied live:01acf649de1dcb169ae4dcc4ddd42d70f1b044fb0fada48246496260117d3001; Home45899f0abc84afd59fec79aa7d3b149202965f32a1052f87dcad33002b1cc14a. Normal real browser/Home, cyan gradient, actual transparent floating dock, native Browser/Terminal/Files, restore/reopen/AltTab/drag/resize/focus and reversible browser/viewer fullscreen. Own20UX checks+actual fullscreen held-state cleanup+live read-only smoke PASS. Old container/image preserved stopped, stage stopped. Live ownerNone/actual heldmask0 at handoff; runtime13files match workspace. QA received explicit exclusive live7901/7791 handoff and is queued behind Neko. No further runtime/input changes from this worker. Full report: `pocs/novnc/UX.md`; evidence `artifacts/novnc/ux/`.

## Live-content drag fix accepted

Kevin's outline report was reproduced in actual decoded RFB frames while the button stayed held: native X11 contents moved, but x11vnc's default wireframe optimization kept streamed contents at the old position. The sole functional change adds `-nowireframe` to startup. Current image is `sha256:0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7`; entrypoint SHA256 `803129717c7467d54ddbdc1a8ccf17ddceef3e5d1523f37f6c2894386045ae8a`. It adds one entrypoint-only layer over the preserved01acf image. Old current container is stopped as `sidekicks-novnc-pre-live-drag`; staging is stopped. Ports remain loopback7901/7791.

Builder held-drag and held-resize decoded/native comparisons passed. Independent focused Docker held-drag checks accepted7/7 tight live matches before normal restart and6/7 afterward, with0 old-position outline matches;6 normal-restart checks passed and neighboring runtimes remained unchanged. A non-tight/partial-repaint intermediate is preserved, so this is not an atomic-frame, physical-input or latency claim. Independent resize content was not part of this focused addendum. QA restored the original client/PID/position, owner null and actual held input empty, closed its viewer and released both targets. No further runtime action is required.

Frozen independent addendum: `artifacts/independent-qa/ux-live-drag/report.md`, verified SHA256 `28a0b1a4f87677be60e4d31f76e2d7ea88d214fd69ea31538ebf64be55a014bd`. Exact image/source identities, own before/after images and completed QA status are in `artifacts/novnc/drag-check/`. The original broader UX report remains unchanged. Only status/evidence files were updated after this final acceptance.
