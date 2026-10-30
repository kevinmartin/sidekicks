# Integration contract v1

Publish this early; workers own only their POC directories. Integration owns root scaffolding, `harness/workbench`, `shared`, `scripts`, and architecture/status docs. POC workers may read/copy `shared/desktop-fixture.html` into their image. Do not edit other workers' files.

## Entrypoints

Each POC supplies `pocs/<id>/start.sh`, `stop.sh`, `status.sh`, `README.md`, and an adapter HTTP server on its reserved port. IDs: `novnc`, `neko`. Scripts must resolve the repository root, use task-owned names, and leave unrelated resources alone. Integration's orchestration calls these scripts. API must bind 127.0.0.1 on host; container services may bind internally. Browser fixture is `/fixture/` at workbench port 7790, or copy the shared file into container and open locally.

## Adapter HTTP routes

- `GET /health` -> `{ "ok":true, "id":"novnc", "status":"ready", "runtime":{ "platform":"linux/arm64", "emulated":false } }`. Return a non-200 status when not ready. Add details freely.
- `GET /capabilities` -> `{ "id":"novnc", "viewerUrl":"http://127.0.0.1:7901/...", "capabilities": {"keyboard": {"status":"tested|implemented|unsupported|untested", "detail":"..."}, ...} }`. Standard keys: keyboard, click, rightClick, doubleClick, drag, scroll, modifiers, pinchNative, swipeNative, zoomEmulated, screenshot, audio, theme, humanTakeover. Truthful status only; protocol support alone is not tested.
- `POST /control` JSON `{ "action":"acquire|release|stop", "owner":"human|agent", "session":"opaque-client-id", "leaseMs":30000 }`. Acquire -> `{ "ok":true, "token":"opaque-lease-token", "owner":"human", "expiresAt":"ISO8601" }`. Human acquire preempts agent, cancels queued actions, releases held keys/buttons. Agent must not preempt human. Token/session release only releases its owner. Stop always releases held inputs and cancels automation. Max lease 60 seconds, default 30 seconds; expire safely. Return 409 on conflict. Include `token` in release/stop requests when required. Human viewer must participate in ownership; if raw viewer cannot be gated, clearly report takeover unsupported/untested and disable automation while viewer is in use.
- `GET /control` -> current owner, session and expiration (never return token).
- `POST /input` JSON `{ "token":"...", "session":"...", "events":[...] }`. All input must validate active owner/token/session. Accept at most 100 events. No arbitrary shell. Return 409/403 for stale/missing lease, 400 for invalid events. Release held keys/buttons on rejection/disconnect/stop.
- `GET /screenshot?maxWidth=1280&maxHeight=720` -> PNG or JPEG binary with content type; bound dimensions to <=1920x1080, payload <=4 MiB. Report source dimensions/format via response headers or metadata. If not ready return an explicit error, never a fake image. A separate JSON/base64 route is optional.

Normalized events (workers may document a subset and return 422 for unsupported):

```
{"type":"key","key":"a|Enter|Control|Shift|Alt|Meta|ArrowUp|...","action":"down|up"}
{"type":"text","text":"synthetic text"}
{"type":"pointer","x":400,"y":300,"action":"move|down|up","button":"left|middle|right"}
{"type":"click","x":400,"y":300,"button":"left|right","count":1}
{"type":"scroll","x":400,"y":300,"deltaX":0,"deltaY":120}
{"type":"releaseAll"}
```

Coordinates are desktop pixels. Double click uses `count:2`; drag uses pointer down/moves/up. Key naming mapping belongs in adapter. Native pinch/swipe must never be claimed from wheel/keyboard emulation. Validation and control lock are mandatory for automation; unsupported routes must fail explicitly.

## Status artifacts

Each POC writes `pocs/<id>/status.json` with id, summary, tested capabilities, evidence paths, platform/emulation, current blockers. Include exact executed tests and URLs in README. No benchmark claims before measurement. Integration polls `/health` + `/capabilities` live and distinguishes route availability from actual end-to-end proof.

## Browser and request safety

Reject non-loopback browser Origins on mutating adapter APIs. Do not enable wildcard CORS. Store no credentials. Capture synthetic desktop content only. A local demo lease is not production authentication. Direct viewer access needs its own input gating and must not bypass lease enforcement.

## Takeover acknowledgment precision (scope correction)

**Default takeover is generic desktop control.** It must work in terminals, browser windows and arbitrary native applications without a synthetic fixture or F24 acknowledgment.

Separate these guarantees:

1. Revoke old ownership, reject stale input, cancel future queued commands and stop new bot injection at the dispatch boundary. Release held keys/buttons generically.
2. An application may still process events injected before revocation. Generic takeover cannot promise rollback or flushing every downstream application queue.

Use small paced cancellable chunks and disclose bounded settling behavior. Expose takeover-pending and ownership-ready states. Ready means the new owner can inject input; it must not imply generic downstream application quiescence. A practical generic path is mandatory. Record injection cutoff timestamp/count and settling separately from decoded-stream input latency; explicitly state `applicationQueueFlushed:false` where appropriate. Workbench's current adapter HTTP timeout is five seconds.

Regression evidence must cover native Linux terminal/browser takeover, actual held-input release, future-command cancellation, stale-token rejection and immediate post-ACK human typing. Preserve and report any already-injected application-event backlog separately, including the original measured 516 ms observation.

### Optional synthetic diagnostic fence

A fixture-specific delivery fence can be an optional instrumented diagnostic, never a prerequisite for normal human leases or a substitute for arbitrary native-app control. If implemented, bind its ACK to the pending challenge, ownership generation and page instance; serialize overlapping diagnostic fences and reject stale/late ACKs. A diagnostic timeout may fail that diagnostic closed, while the ordinary generic control path remains usable with its documented guarantees. No F24 dependency is required or deployed in the default POC.

### Literal text versus key-event semantics

Normalized `text` must preserve submitted literal case/punctuation for supported characters; unsupported characters should be rejected explicitly rather than silently changed. A native key-event route sends actual key transitions and modifier state. A browser SDK typing helper that emits an uppercase printable value without Shift is not proof of physical Shift behavior; test explicit modifier chords and literal-text API semantics separately.
