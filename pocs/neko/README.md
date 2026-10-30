# Sidekicks / Neko computer-use POC

**Current production Xfce desktop:** exact v7 is accepted on Docker 7902 plus canonical Neko relay/NAT VMs 7906/7907, each with 10 independent production checks and one supported normal restart. See [XFCE.md](XFCE.md), [xfce-status.json](xfce-status.json) and the authoritative [root desktop refresh](../../docs/desktop-polish.md). Docker viewer: <http://127.0.0.1:7902/>; native diagnostic adds `?mode=native`. The released stage 17802 is stopped with its rollback data retained. [UX.md](UX.md) and [ux-status.json](ux-status.json) describe the earlier Openbox revision; baseline benchmark/audio evidence below remains historical.

This is a real disposable Linux desktop streamed over Neko WebRTC, with a small Sidekicks viewer and an ownership-enforcing adapter. It does not call a model or implement a production bot platform.

## Run

From the repository root, with the existing Docker Desktop running:

```bash
bash pocs/neko/start.sh
bash pocs/neko/status.sh
bash pocs/neko/stop.sh
```

- Workbench input viewer: <http://127.0.0.1:7902/>
- Native Neko SDK diagnostic: <http://127.0.0.1:7902/?mode=native>
- Adapter: <http://127.0.0.1:7792/health>
- Capabilities: <http://127.0.0.1:7792/capabilities>
- Synthetic fixture telemetry: <http://127.0.0.1:7792/fixture-state>

Click **Take human control** before input. **Linux terminal** opens the disposable GTK3/VTE terminal; **Minimize Linux app** exposes the Xfce desktop panel using Linux Alt+F9. **Release**, **Stop all input**, tab blur/hide, viewer disconnect, and expiry revoke ownership and release native held input. The panel launches browser, terminal and Thunar; Linux Super+Enter opens the terminal. The official native overlay maps macOS Command to Linux Control (and Option to AltGr); physical Command+Enter therefore is not Linux Super+Enter. The viewer launcher buttons send explicit Linux chords over the active input route, giving Mac users a clear path to the terminal and panel.

Docker socket access on this Mac requires the executor's scoped escalation. The scripts never install or start the shared runtime. `start.sh` selects the exact local frozen v7 image without downloading/building; a missing or changed image fails clearly. A matching stopped container starts directly. A differing task-owned container requires explicit `start.sh --replace`, which stops and timestamp-renames it for rollback before creating a fresh container. It never removes the preserved container. The browser receives a fresh synthetic profile on each desktop boot; appearance and launcher configuration persist in the image. Coordinate an exclusive window for stop/replacement; use [XFCE.md](XFCE.md) for the explicit rebuild/rollback paths.

## Runtime and pinning

The host is Apple M1 Max / ARM64. The existing Docker Desktop Linux VM is ARM64. The official GHCR image pull returned registry denial, including an empty task-local Docker configuration. Docker Hub's documented upstream Firefox image was AMD64-only. This POC therefore builds **official Neko v3.2.0 public source natively for `linux/arm64`**, rather than using emulation. Source archive SHA-256:

```
3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d
```

The Dockerfile uses official Go 1.25 / Node 22 / Debian Trixie images and Debian Firefox ESR. The Neko SDK is exactly `@m1k1o/neko@3.2.0`; npm dependencies are locked. Debian package versions are recorded by the build log, not individually frozen. This is not a bit-for-bit reproducible OS snapshot. See `artifacts/neko/build.log` and `start.log`.

The container has 2 CPUs, 2 GiB RAM, 512 MiB shared memory, no privileged mode, no host mounts, and no host Docker socket. Host TCP 7792/7902 and UDP 59020–59039 bind explicitly to **127.0.0.1**. Raw Neko HTTP 8080 is internal loopback only. ICE uses loopback NAT mapping, ICE Lite and a loopback-only STUN placeholder to avoid upstream's external STUN default. No router, LAN, OS security or Docker security changes are needed. No GPU device is passed; the encoder is software VP8.

## Input and takeover boundaries

There are two deliberately distinct tested routes:

1. **Gated workbench mode:** browser input or normalized `/input` actions → adapter lease validation → authenticated Neko v3 `control/*` websocket messages → Neko X11 input. Media remains WebRTC. The viewer's direct Neko input is not host, and its websocket gateway denies `control/*` events.
2. **Native diagnostic mode:** official SDK `Overlay` → Neko binary WebRTC data channel → X11. A human-only native lease transfers Neko host ownership to that exact viewer. Bot HTTP input is rejected in this mode. Neko `locked_controls` prevents the viewer from acquiring host independently. Release/reset/take revoke the native host before a new agent lease can write.

The adapter is the only privileged controller exposed by the POC. There is no shell execution endpoint or direct X11 automation bypass. Read-only `xdotool` queries in lifecycle tests independently inspect the resulting virtual desktop state. All desktop input goes through the routes above.

Human acquisition preempts an agent, invalidates the queued generation, resets keys/buttons, then grants the human lease. An agent cannot preempt a human. Every event and every character in an executing text job rechecks its generation. Renewing a lease requires its current token; copying the session shown by `GET /control` does not recover the token. Non-loopback Hosts and browser Origins are rejected. This local demo lease is not production authentication.

Current v3.2.0 data-channel framing is big-endian: one opcode byte, two-byte payload length, then typed fields. A key packet is 7 bytes (32-bit X11 keysym), pointer movement 7 bytes, and scroll 8 bytes. This is different from Neko's older 11-byte little-endian legacy key framing; the POC disables legacy mode and uses the published SDK.

## Adapter examples

Acquire a lease:

```bash
curl -s http://127.0.0.1:7792/control -H 'Content-Type: application/json' \
  -d '{"action":"acquire","owner":"agent","session":"my-test","leaseMs":30000}'
```

Keep the returned ephemeral token in memory, pass it with `session` to `/input`, and release with `/control`. Supported normalized events are `key`, `text`, `pointer`, `click` (count 1/2), `scroll`, and `releaseAll`. Normalized `text` preserves printable ASCII, LF/newline and Tab, up to 2,000 characters per event. Other Unicode/control characters return 422 before the batch writes; arbitrary Unicode/IME is not claimed. Coordinates are **desktop pixels**, not viewer CSS pixels. Unsupported events return 422; invalid batches fail before any event in the batch is sent. Native diagnostic leases reject normalized HTTP input.

`GET /screenshot?maxWidth=640&maxHeight=360` returns a real bounded JPEG from Neko's X11 screenshot API, resized with sharp. Source/output dimensions and capture duration are response headers. Bounds are at most 1920×1080 and 4 MiB. Mapping is `desktopX = imageX × sourceWidth/imageWidth` and likewise Y. Screenshot capture is independent of the WebRTC viewing path; it is not labeled human action latency.

## Verification

The tests launch an isolated headless Chrome context with no user's profile or tabs and exercise only the container's synthetic fixture.

```bash
cd pocs/neko
npm ci
npm test
node test/smoke.mjs
node test/e2e.mjs
node test/lifecycle.mjs
node test/text-contract.mjs
# Run the latency scripts only during an exclusive measurement window:
node test/latency.mjs --native
node test/latency.mjs
node test/latency.mjs --native --quality
node test/latency.mjs --quality
```

`lifecycle.mjs` deliberately restarts only `sidekicks-neko`. End-to-end and latency tests must not compete with another owner of the same desktop. The shared fixture is copied read-only from `shared/desktop-fixture.html`; the POC's copy adds a changing visible marker, bounded synthetic event telemetry, and pointer coordinates. No private web content is used.

Recorded end-to-end evidence includes decoded VP8 media, both native and gated viewer keyboard/modifier/pointer routes, independently normalized automation, queued bot cancellation, simultaneous acquisition, forged/stale token rejection, and fresh bounded capture. Lifecycle and final measurement results are summarized in `status.json` and `docs/status/neko.md` after execution. Raw evidence lives in `artifacts/neko/`; independent QA owns `artifacts/independent-qa/`.

Final independent verification passed the native terminal command-output takeover, clean restart/fresh marker/capture, and synthetic audio decode. The earlier title assertion and appearance-restoration helper failures remain preserved; those aggregate reports are not relabeled PASS.

| Observed native WebRTC result | Measurement |
|---|---:|
| Successful input-to-decoded counter observations | 30 / 30; zero misses |
| Median | 61.7 ms |
| p95 | 96.2 ms |
| Maximum | 96.9 ms |
| Median / p95 sampling interval | 16.6 / 16.8 ms |

The timing uses one browser's `performance.now()` from native `mousedown` to the first decoded change in the standard fixture's synchronous click-counter region, sampled with `requestAnimationFrame`. It excludes the supplemental marker's timer. QA ran backends sequentially with builders idle, at 1280×720 and 2 CPUs/2 GiB. It is a local synthetic headless-browser result, not physical human action-to-display latency or a WAN benchmark. See `artifacts/independent-qa/neko-native-measure.json`. The quality encoder variant and a dedicated idle-versus-motion resource study remain untested. A single post-QA Docker snapshot was 32.72% of one core and 667.6 MiB; it is not labeled idle/motion.

Exact `SideKicks QA! 123` plus further punctuation and LF round-tripped through both viewers with explicit physical key modifiers and through normalized HTTP `text`. See `artifacts/neko/text-contract.json`; its earlier aggregate title assertion failed after the three text checks passed. Independent normalized-text and actual terminal output proofs are recorded separately. New tests identify XTerm by class/output rather than its mutable shell title.

## Limits

Native Mac trackpad pinch/swipe and real human/mobile hardware are **UNTESTED** overnight. The custom Xorg touch driver is disabled in this source-build configuration, so native touch/pinch/swipe are **UNSUPPORTED in this configuration**. Wheel/drag and Ctrl+wheel are emulations, not evidence of native gestures. Keyboard layout/IME beyond the exercised keys is not exhaustively validated. Native synthetic browser `.type()` calls that provide uppercase keysyms without a Shift modifier can produce lowercase; the native physical-key tests use explicit Shift, and the normalized ASCII `text` route handles case itself. No GPU performance claim is made.

Audio is configured as a synthetic PulseAudio output monitor through Opus. Independent QA received decoded PCM for the 250 ms, 440 Hz fixture tone: peak RMS 0.04253 against a silent baseline, using `MediaStreamAudioSourceNode` on the received stream. The video stayed muted and the analyser fed a zero-gain sink. An earlier `MediaElementAudioSourceNode` probe returned zero and is preserved as a failed test attempt. No microphone is requested, and physical speaker listening is untested. See `artifacts/independent-qa/neko-native-audio.json`.

Local loopback synthetic tests do not establish WAN performance or physical human action-to-display latency. The two encoder profiles are `interactive` (2.2 Mbps target, `cpu-used=8`) and `quality` (5 Mbps target, `cpu-used=4`), both VP8, 30 fps and two encoder threads. Targets are not observed bitrate or guaranteed quality. Actual timing and frame observations require the measured artifacts.

## Public upstream sources

- [Neko v3.2.0 release and reusable client core](https://github.com/m1k1o/neko/releases/tag/v3.2.0)
- [Official installation](https://neko.m1k1o.net/docs/v3/installation)
- [Image names, versions and architectures](https://neko.m1k1o.net/docs/v3/installation/docker-images)
- [Configuration reference](https://neko.m1k1o.net/docs/v3/configuration)
- [Pinned public source](https://github.com/m1k1o/neko/tree/v3.2.0)

Neko and its SDK are Apache-2.0; the bundled keyboard implementation carries its upstream Apache Guacamole notices. This POC's dependency lockfile and public source archive preserve the tested inputs.
