# Temporary LAN viewers

Kevin explicitly authorized unauthenticated `0.0.0.0` POC exposure on October27 after the LAN/HTTP risks were explained. These listeners bind **all IPv4 interfaces**, not only home Wi-Fi. Any client that can reach them can view and take control of the disposable guest desktops. HTTP signaling, input and noVNC pixels are unencrypted; Neko media retains WebRTC DTLS/SRTP protection. Do not enter sensitive data. No firewall, router, VPN, TLS trust, credential or host security settings are changed.

Current advertised address: **192.168.0.97** (Thunderbolt Ethernet `en9`). Wi-Fi is also active at192.168.0.169. The startup command reads the current default interface and NAT guest address; stop/start after an address change. IPv6 listeners are not created.

| POC | URL | Wildcard TCP | Wildcard UDP |
|---|---|---|---|
| noVNC Docker / RFB | http://192.168.0.97:17901/ |17901| none |
| Neko Docker / WebRTC | http://192.168.0.97:17902/ |17902|59040–59059|
| noVNC microVM / no NIC, vsock | http://192.168.0.97:17903/ |17903| none |
| Neko microVM / UDP over reliable vsock | http://192.168.0.97:17906/ |17906|59140–59159|
| Neko NAT microVM / host UDP forwarder | http://192.168.0.97:17907/ |17907|59240–59259|

A labeled list is available at http://192.168.0.97:17901/lan/index . Neko's `/?mode=native` selects the official SDK/data-channel diagnostic in the Sidekicks wrapper, not the complete upstream Neko application.

## Mobile use

1. Open a viewer on the same LAN; landscape generally gives the remote desktop more space.
2. Tap **Take control** and then a text field inside the guest desktop.
3. Tap **Keyboard** to open text entry and extra keys. Default routes use the existing human lease input API. Native Neko focuses the official SDK's transparent textarea and sends extra keys over its data channel.

The LAN-only script adds an HTTP-compatible UUID generator using `crypto.getRandomValues`, responsive Neko header wrapping and a software-keyboard UI. It supports printable ASCII, Enter and Tab in the default text route; Unicode is explicitly rejected. NoVNC1.6.0 supplies mouse-emulating touch gestures; native Neko3.2.0 uses first-finger mouse fallback because the guest touch input driver remains disabled. Gated Neko has ordinary pointer handlers, not a multi-touch gesture translator. Clipboard and fullscreen depend on browser/secure-context support; no browser security exception is installed.

**Physical iPhone/Safari, actual Wi-Fi inbound connectivity, software-keyboard behavior, gestures and iOS fullscreen remain UNTESTED.** The verification uses separate headless Chrome on the Mac at the Mac's LAN address, including a390px viewport. It proves actual decoding, selected UDP candidates, input and route boundaries, not physical-phone compatibility or inbound firewall traversal.

## What is exposed

The existing loopback ports7901/7902/7903/7906/7907 and APIs7790/7791/7792/7793/7796/7797 remain bound exactly as before. An additive task-owned bridge uses distinct wildcard ports to avoid disturbing them. It forwards only viewer assets, RFB/Neko signaling sockets, viewer-session creation, human lease actions, human-token input, health and bounded screenshots. Agent acquisition, workbench APIs, host/guest exec/RPC, internal Home capability and `/desktop/launch` are denied. Human desktop control can still open a **guest** terminal; no host shell endpoint is added.

After exact incoming Host/Origin checks, the bridge uses the backend's existing loopback authority. The original lease/input arbitration stays authoritative. Server ICE candidates alone are rewritten from the exact configured backend UDP range to the Mac address/public forwarding range; ICE credentials remain unchanged. UDP peers can reach only fixed destinations:

- Docker: public59040–59059 → existing127.0.0.1:59020–59039.
- no-NIC Neko VM: public59140–59159 → existing127.0.0.1:59120–59139 → framed reliable vsock.
- NAT Neko VM: public59240–59259 → current guest192.168.64.4:59220–59239.

The NAT phone route is **host-forwarded UDP**, not direct phone-to-guest routing. The relay variant retains reliable-stream head-of-line behavior. Each variant is limited to64 learned source tuples, fixed destinations,60s idle expiry and256KiB pending data per connecting peer. UDP datagrams above9216bytes are dropped to respect the observed Mac limit. WebSocket pending/buffered output is bounded. This POC is not production rate limiting or a tenant security boundary.

## Commands and rollback

```sh
bash scripts/lan.sh status
bash scripts/lan.sh stop    # removes only this bridge's wildcard TCP/UDP listeners
bash scripts/lan.sh start   # requires existing five baseline demos + NAT guest
node --test pocs/lan/bridge.test.mjs
node pocs/lan/smoke.mjs
node pocs/lan/native-smoke.mjs
```

`start` detaches one task-owned Node process, records its PID/config in `.run/lan/`, and waits for readiness emitted by that child. No persistent service or auto-start setting is installed. `stop` verifies the exact process command before signaling it. Stopping the bridge leaves all original loopback demos and guest state running. No Docker or VM image, startup file or template was changed for this task. Logs contain aggregate counters, not input text, session tokens, media payloads or traffic recordings.

## Evidence

- `artifacts/lan/*-smoke.json`: one final current result per default viewer, with actual decoded media, LAN ICE pairs, exact synthetic typing, layout/capture checks and Home restoration.
- `artifacts/lan/*-native-smoke.json`: additional native SDK route results where completed.
- `artifacts/lan/attempts/`: preserved earlier helper failures, including too-fast initial address-bar focus and missing Neko fixture offset assumptions. Subsequent tests use settling and actual read-only X11 geometry.
- `artifacts/independent-qa/lan/`: independent source review, original malformed-upgrade and stale-readiness findings, and narrow repaired-boundary checks. The repaired source passed12 applicable offline boundary checks (2 no-UDP cases N/A) and2 mocked readiness cases, followed by17/17 narrow live boundary checks; original findings remain preserved. This independent audit performed no browser/input/restart and does not independently certify media or phone behavior.

No performance or phone-latency benchmark was run. Earlier desktop/performance reports remain historical and unchanged. Local screenshots are synthetic QA evidence only; none is uploaded for this LAN task.
