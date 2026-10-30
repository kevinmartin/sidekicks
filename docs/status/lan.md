# LAN POC status

2026-10-27: Kevin explicitly chose unauthenticated0.0.0.0 viewer exposure after the HTTP/all-interface risks were disclosed. A separate task-owned bridge adds TCP17901/17902/17903/17906/17907 and UDP59040–59059/59140–59159/59240–59259. The original loopback services, private APIs, VM RPC, images and guest startup configuration remain unchanged. No firewall/router/VPN/security/credential changes.

Current LAN IPv4 is192.168.0.97 on default en9; the NAT guest is192.168.64.4. The NAT phone path uses host UDP forwarding, not direct phone-to-guest routing. See [exact URLs and commands](../../pocs/lan/README.md).

Builder verification on the final bridge revision:

- Five default viewers PASS actual decoded1280×720 through the LAN address over plain HTTP/WebSocket, exact guest typing from the new Keyboard panel at390px width, no horizontal overflow, bounded fresh capture, Home restoration and lease release.
- All three Neko native SDK routes PASS actual LAN WebRTC UDP, host-transfer/human ownership, Keyboard focus to the official SDK textarea, exact lowercase guest text and extra Ctrl+A replacement.
- All six Neko sessions selected actual UDP pairs at192.168.0.97 in their separate public forwarding ranges. This does not prove physical phone or inbound firewall traversal.
- Four offline unit tests PASS for private-route denial, malformed upgrade handling and constrained ICE rewriting.

Raw reports/screenshots and preserved initial failed helper attempts: `artifacts/lan/`. Those failures included an insufficient address-bar settling interval and an incorrect assumption that Neko fixture telemetry supplied its own window offset; the final verifier uses explicit settling and actual read-only X11 geometry.

Independent review found and preserved a malformed WebSocket URL crash and a stale-log readiness bug. Both were fixed. Independent offline regression12 applicable boundary checks PASS (2 no-UDP cases N/A), and2 mocked readiness cases PASS; source/proof files are under `artifacts/independent-qa/lan/`. Independent narrow live boundary confirmation completed17/17 PASS at2026-10-27T03:51:36Z (October26,11:51p.m.EDT): all5TCP/60UDP public bindings belonged to the same bridgePID69880, original11web/API and40mediaUDP bindings stayed loopback, private-route/agent denial and malformed-upgrade process-survival checks passed. No guest input, lease, browser, restart or screenshot action was performed by this boundary audit. Source hashes match the current files. Raw evidence: `artifacts/independent-qa/lan/readonly-2026-10-27T03-51-34-875Z/result.json`. QA released its window; no further runtime actions are planned.

Physical iPhone/Safari, mobile keyboard/gestures/IME and iOS fullscreen are UNTESTED. Default text entry intentionally supports printable ASCII plus Tab/Enter. Upstream noVNC is1.6.0; Neko SDK is3.2.0 with touch-driver-disabled first-finger mouse fallback in native mode. No performance claim, recording or screenshot upload accompanies this task.

Independent QA is frozen: [report](../../artifacts/independent-qa/lan/report.md), SHA256 `0df1c1a7a53c6f7e076da3eb5c9cd001ded777e8d525bec164d56a255e3685ad`. Live QA is released and no further runtime actions are planned. The independent boundary audit does not claim media/freshness or physical-phone verification; builder evidence remains separately attributed.
