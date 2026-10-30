# Private Chromium capability

`New("http://127.0.0.1:9222")` connects to a **dedicated workload's** private CDP service. The constructor accepts literal loopback HTTP authorities with an explicit port. It must never be pointed at a personal browser. Discovery redirects, foreign WebSocket authorities, encoded/foreign target paths, credentials and non-page targets are rejected. The client creates and closes only its own targets.

The client exposes navigation, DOM readiness, title, one CSS selector's text, complete primary-button clicks, Unicode text insertion and bounded viewport PNG capture. There is no arbitrary JavaScript-evaluation or arbitrary target-attachment API. `Navigate` acknowledges the CDP navigation; `WaitReady` checks DOM interactive/complete, which does not imply arbitrary application work has settled. This is a Chromium CDP implementation; another browser adapter may use another protocol.

`NewHandler(client)` provides the exact machine endpoint `POST /browser` and **must be wrapped in the guest service's authentication**. It rejects requests with a browser Origin, requires JSON, and rejects unknown fields, multiple JSON values and bodies over 128 KiB. It does not provide CORS or a public CDP proxy. Its opaque `pageId` values differ from raw CDP target IDs.

| action | additional fields | result |
| --- | --- | --- |
| `new` | none | `pageId`, `claimWithinMs` |
| `navigate` | `pageId`, `url` | `ok` after DOM readiness |
| `title` | `pageId` | `title` |
| `text` | `pageId`, `selector` | `text` |
| `click` | `pageId`, `x`, `y` | `ok` |
| `type` | `pageId`, `text` | `ok` |
| `screenshot` | `pageId`, optional `width`, `height` | `mime`, base64 `data` |
| `close` | `pageId` | `ok` |

There are at most eight owned targets, 15-second operations, 65,536-byte text input, 1 MiB returned text, a 12 MiB CDP-message limit and an 8 MiB standalone PNG limit. The private handler further caps raw PNGs at 1 MiB (at most about 1.34 MiB after JSON/base64), returning HTTP 413 before emitting an oversized payload so the guest and Docker invocation envelopes remain bounded. The complete JSON response is independently capped at 1400 KiB, including escaping and base64; oversized text envelopes also return HTTP 413 before any payload is written. Screenshots default to 1280×720 and cannot exceed 1920×1080; viewport capture and full PNG validation prevent an unbounded full-document capture. Multi-command operations are serialized per page. Click errors attempt a release; a failed release retires the owned target and reports errors rather than making that page available for continued input. Failure to contact CDP can still prevent confirmed target cleanup. Both the client ownership registry and handler page ID are retained until a close acknowledgment (or target-already-gone 404), so Page.Close, the handler close action, or Client.Close can retry rather than silently forgetting the target. Failed commands attempt bounded cleanup before retiring known handles; unconfirmed creation requires workload teardown as described below. Persistent cleanup failure is reported and may require workload teardown. The library does not control arbitrary native input or promise to flush an application's already-delivered events.

The handler's new-page result must be used in another page operation within the advertised `claimWithinMs` (currently two minutes). A page never claimed by a later request is closed, preventing lost responses from permanently consuming the page limit; explicit response write failure triggers immediate cleanup. A claimed page is not expired this way. Cleanup failure retains the page in the client's ownership registry until client shutdown retries it. If a CDP creation response is lost or malformed, Chromium may already have created a target whose ID is unknown. The client refuses further page creation and `Close` reports `ErrCreationUncertain`; it cannot safely infer which target to close. Tear down the dedicated browser workload to reconcile that state. Up to eight already concurrent creation requests may have been in flight when uncertainty was detected.

HTTP(S) and `about:blank` navigation are allowed; file/data/javascript URLs and URL credentials are rejected. This is application curation, **not network isolation**. A web page can make private-network requests unless an independent runtime/network policy prevents them. A loopback endpoint does not by itself identify an owned browser; configuration ownership remains the caller's responsibility.

Tests use ephemeral fake-CDP HTTP/WebSocket servers and cover protocol framing, cancellation, malicious discovery, no redirects, target ownership, resource limits, command serialization, selector quoting, PNG validation and handler boundaries. They do not substitute for actual isolated Chromium end-to-end tests.
