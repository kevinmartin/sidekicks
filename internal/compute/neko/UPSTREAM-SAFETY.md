# Pinned Neko input safety decision

The compute layer uses Neko v3.2.0 for WebRTC media, cursor transport and signaling. Human and agent input both use the shared synchronous Go X11 driver under the control manager. Native SDK input and the asynchronous administrator control WebSocket are unsupported. There is no experimental runtime or HTTP opt-in. Earlier POC source is preserved on the local `reference/poc` branch; its ignored runtime resources and evidence remain separate and are not included in that branch.

## Source-backed finding

The locally preserved v3.2.0 source archive SHA256 is `3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d`.

* [Native handler](https://github.com/m1k1o/neko/blob/v3.2.0/server/internal/webrtc/handler.go): line21 snapshots `session.IsHost()`; line95 later checks that cached result; lines132/154 eventually call desktop KeyDown/ButtonDown. The source file SHA256 is `cff726da7b73357cc421f239d23c97ead10a484969d02024cb6554744ca4c6ef`.
* [Control reset API](https://github.com/m1k1o/neko/blob/v3.2.0/server/internal/api/room/control.go): lines104–105 reset keys and clear host. The source file SHA256 is `30ad7e7561c4e7d9c4cc7e41cbd6bb3047ed12430dd6cd28c6794d118ab2ef2f`.
* [Controller WebSocket handler](https://github.com/m1k1o/neko/blob/v3.2.0/server/internal/websocket/handler/control.go): authorization/controlRequest precedes eventual ButtonDown/KeyDown at lines119/155; no shared ownership generation/reset barrier surrounds those operations. File SHA256 is `ac7a0aa88a3d8051b4e786205e70f7628d3fb9fad694d929fc54c8d2ff11a20c`.

A native callback can snapshot ownership, pause, then resume after a concurrent reset and inject a key. The administrator WebSocket introduces a second asynchronous dispatch queue with the same absence of a cross-connection generation barrier. Gateway token checks, a read-only SDK overlay, or closing signaling do not prove that an already-running upstream callback has joined. No adversarial timing reproduction against the actual binary is claimed; the finding follows from the inspected source ordering. Earlier successful functional checks do not rule out this race.

## Fail-closed implementation

The default server configuration grants SDK viewer accounts watch/connect access with `can_host:false`, clipboard/media sharing disabled, locked controls and implicit hosting off. The Go client validates those returned viewer permissions. It never transfers host ownership: `Client.Give` returns `ErrNativeInputUnsupported`. `Client.Execute` and any controller `control/*` send return `ErrUpstreamInputUnsupported` without writing a frame. The private Neko-only `/desktop/input` compatibility route returns501. Handler construction rejects `EnableNative:true`.

The actual compute private `/desktop/input` and public cookie-authenticated `/input` routes use the existing synchronized Go X11 driver and control-manager gated dispatch. The Neko viewer config has neither a native ownership resolver nor an input override. WebRTC signaling, UDP candidate mapping, video, read-only cursor/ping data and capture remain available. Generic application-queue flushing is still not promised.

Tests cover rejected native/controller APIs with zero upstream input frames, media signaling continuing after rejection, default server RBAC, public factory wiring to Go X11, exact lease binding/disconnect cleanup, and direct private agent revocation on controller loss. These are isolated protocol/unit tests; actual rendered media and Go X11 interaction are independently verified by the root integration suite.
