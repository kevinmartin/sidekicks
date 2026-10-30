# Real desktop viewer acceptance

This suite creates only new `sidekicks-compute-e2e-*` Docker workloads. It never accepts an existing POC ID. Confirmed task-owned containers are stopped and destroyed in cleanup; all pre-existing Docker containers—including stopped resources and other compute instances—are compared before and after each route, without prefix exclusions. No personal browser profile is opened. The Chrome BrowserServer and contexts belong solely to this test.

Prerequisites: built Go CLI, local `sidekicks-compute/desktop-novnc:clean` and `sidekicks-compute/desktop-neko:clean` images, running Docker, `npm ci --prefix web`, and installed Google Chrome. The coordinator must complete and announce the clean-image handoff before execution. The script never builds/pulls images or changes host security/network settings.

```sh
SIDEKICKS_E2E=1 node test/e2e/desktop.mjs --execute
# Narrow one route when diagnosing an existing preserved failure:
SIDEKICKS_E2E=1 node test/e2e/desktop.mjs --execute --adapter desktop-neko
```

The optional `SIDEKICKS_DOCKER_SOCKET` environment variable is passed to the existing Go CLI. The local command uses Make-built `.run/compute-bin/sidekicks`. Explicit `egress` networking is used only for published viewer/media endpoints; this is not a claim of egress isolation. Published ports must be loopback. There is no phone, WAN, performance, physical-keyboard or isolation-escape test.

Each route verifies:

- Go runtime creation and current guest readiness, actual 1280×720 decoded stream, and consumed/removed one-use ticket.
- For Neko, the selected transport's actual candidate pair is UDP, its remote peer is an allocated loopback-mapped media port, and actual VP8 frames decode. TCP fallback cannot pass.
- A valid agent lease actually injects Shift-down through the private API; actual X11 keymap and Shift modifier state must show it held. Human takeover must show all actual input clear on the first observed owned state after acknowledgment (no polling-until-clear), and the old agent input must return 409. Both viewers use the owned Go X11 input endpoint, RFB remains receive-only, and Neko's upstream client never obtains native control. Native acquisition must be rejected as unsupported.
- Actual viewer mouse/keyboard navigation to the private synthetic fixture, exact fresh lowercase marker typing, and a real click-counter increment.
- Direct native PNG versus the independently decoded stream, using fresh white marker glyph pixels inside the textarea. Blank/stale frames fail despite an unchanged background. Encoded frames/PNGs are saved locally.
- Actual server input is empty before setup, then left-button hold is observed before Shift, then actual nonempty X11 key state plus fixture Shift is observed. The viewer release function must clear all actual keys/buttons before local Playwright release events are sent. This is a UI-function invocation with ordinary transport-held input, not a physical click on the release button.
- Viewer fullscreen entry/Escape exit. Guest browser fullscreen and physical reserved keys are outside this bounded check.
- Exactly one normal supported Go CLI stop/start, fresh authentication/decoded stream, fresh remote input/capture, and final unowned actual held-empty state. Restart does not replace the instance image or identifier.

A new `artifacts/compute/e2e/<timestamp>-<adapter>/` directory preserves every result, including failures, interruption/cleanup errors and local screenshots. `protected-before.json`/`protected-after.json` inventory all containers (`docker ps -a`) with immutable IDs, image, running/stopped/restarting/paused/dead state and restart/exit timestamps. Any addition, removal or change keeps the strict gate non-PASS. This observation cannot establish causation or exclude indirect effects such as resource pressure. `lifecycleTrace` records only operation, generated target ID, timestamps and success/failure; it contains no request payload or credential.

The harness cleans up by name only after a verified successful creation acknowledgment. A name collision, failed CLI create or malformed/unconfirmed acknowledgment never triggers harness stop/destroy. Runtime provisioning owns cleanup by the newly returned immutable Engine ID; its ProvisionError cleanup outcome remains authoritative. Any remaining orphan uncertainty is retained explicitly for coordinated inspection, rather than risking an existing colliding resource. Tokens, agent credentials and credential-bearing viewer/driver URLs remain in memory and are redacted from diagnostic errors. Do not upload images unless separately requested.

Startup Home screenshots require the coordinator's actual pixel review; decoded geometry alone does not identify Home. The resulting JSON states that limitation explicitly. Fixture DOM telemetry is private synthetic diagnostics and does not by itself prove remote delivery; the independent decoded marker/native-capture comparison is required. The startup barrier checks actual decoded bright Home-panel, cyan launcher and dark-glyph pixels before capturing the Home screenshot; semantic Home identity still requires coordinator review. The white-text thresholds accommodate RFB/VP8 encoding differences and do not claim pixel-perfect video or latency parity.

Before the clean-image handoff, only source and syntax/format checks have been performed. A written suite is not a PASS result.

## Immutable Neko audio comparison

`--image` accepts only a local immutable `sha256:` image ID and requires one explicit adapter. It never retags the baseline. `--audio` is opt-in and requires `--adapter desktop-neko`. After the coordinator hands over both frozen images, one exact image can be tested independently:

```sh
SIDEKICKS_E2E=1 node test/e2e/desktop.mjs --execute \
  --adapter desktop-neko --image sha256:<64-lowercase-hex-digits> --audio
```

The comparison runner executes the same bounded viewer suite sequentially for two **distinct** IDs and records both resolved IDs:

```sh
SIDEKICKS_E2E=1 node test/e2e/neko-audio.mjs --execute \
  --baseline sha256:<baseline-id> --candidate sha256:<candidate-id>
```

Do not copy placeholder IDs literally. Neither command builds, pulls, promotes, or retags an image. Each child owns a newly generated disposable instance and task-owned Chrome. `artifacts/compute/audio/<timestamp>/result.json` links to immutable per-run evidence. A failed protection audit stays FAIL even if functional media checks passed. Confirmed owned cleanup is tracked separately; the runner refuses a second workload when cleanup is unconfirmed. The usual strict audit includes stopped containers and other compute workloads, without exclusions.

The audio proof runs both **before and after exactly one normal stop/start**, alongside the existing VP8/selected-UDP/input/takeover/release checks. It releases human ownership before every guest command and checks the desktop is unowned and actually held-empty; it does not preempt a human. It verifies `/usr/bin/paplay`, records installed Pulse/GStreamer/graphics package versions, writes and reads back a deterministic 16,044-byte PCM WAV through the bounded workspace file RPC, and invokes fixed guest `paplay` arguments targeting only the guest `audio_output` null sink through `/tmp/pulseaudio.socket`. The owned source file is removed before reacquiring human control.

The received live audio track must negotiate Opus/48 kHz on the selected allocated loopback UDP pair. A task-owned receiver `AudioContext` reads actual decoded samples from that remote track through an analyser and a **zero-gain** destination; the media element stays muted. A quiet negative-control window precedes three synthetic 500 ms/440 Hz guest-tone trials per boot. Each trial requires fresh Opus packet/byte increments, nonzero decoded RMS well above baseline, a dominant 440±15 Hz component, and sustained energetic readings. Raw PCM, sampling windows, source WAV/hash, selected transport/codec and analysis are saved locally. Silence, unrelated-frequency sound, noise and a nonquiet baseline have executable negative tests. Host audio devices/settings and personal browser profiles are untouched.

This establishes a synthetic **guest Pulse → GStreamer → Opus → UDP WebRTC → decoded PCM** path. It does not establish Firefox-generated media audio, physical listening, audio fidelity/latency, arbitrary codecs, WAN behavior or performance parity. Removing GStreamer Bad removes optional upstream H.265/AV1 encoder and RTMP/AAC broadcasting capabilities; keeping Ugly does not make those supported. VP8/Opus does not exercise retained H.264 or other optional pipelines. Actual Firefox/Terminal/Files launch/restore/close is a **separate real-guest test of a one-layer test-only derivative of the exact selected image** and remains a promotion prerequisite; this media harness does not substitute for it.

For the scoped comparison, set `SIDEKICKS_NEKO_BASELINE_IMAGE` and `SIDEKICKS_NEKO_CANDIDATE_IMAGE` to distinct immutable IDs and run `make verify-neko-candidate` after an explicit runtime handoff. It runs deterministic checks, freshly builds and runs the isolated application lifecycle against both images, then runs the baseline/candidate media comparison. This is an additional acceptance gate; full `make verify`, the independent release vulnerability gate, and review of removed optional capabilities remain required for a release decision. The comparison gives each owned child ten minutes, requests cooperative cleanup on timeout and waits sixty seconds before force termination. A timeout or forced termination is a failure with cleanup unconfirmed; inspect the saved result and owned-resource state before another run.

For Chrome shutdown the harness first disconnects its client, then requests Playwright's kill on the disposable owned BrowserServer. If the leader has exited and its Unix process group is absent, it closes only that child's remaining local pipe endpoints so Node can deliver the process `close` event; it still requires Playwright's cleanup promise to complete. Failure or timeout at any step remains unconfirmed. This check does not prove that a hypothetical descendant which escaped that group was removed.
