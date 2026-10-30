# Isolated Neko microVM loopback build

This additive image replaces only `/usr/bin/neko` in the accepted ARM64 runtime image. It does not start any service or modify `pocs/neko/`, the accepted image/tag, or running containers. This is a source/build handoff; decoded microVM media remains unverified here and belongs to the microVM integration tests.

## Provenance and build

Pinned public Neko v3.2.0 archive: `https://codeload.github.com/m1k1o/neko/tar.gz/refs/tags/v3.2.0`.
SHA256 `3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d`.
Exact accepted base: `sha256:a7120e76048242bacf22b36e3c916339f71ad82858e0ce69c53d2fe8611fec85`.

`loopback-candidate.patch` adds one call, `settings.SetIncludeLoopbackCandidate(true)`, in the EPR path's shared Pion setting engine. There is no mux change. The Dockerfile uses the accepted POC's official `golang:1.25-trixie` build stage and dependency list. `build.sh` verifies the archive, patched file, accepted base ID before/after, all inherited filesystem layers, ARM64/Linux platform, and unchanged image runtime configuration. The accepted local base tag is used only after verifying its exact ID; the new tag is `sidekicks-neko:microvm-loopback`.

```sh
bash pocs/microvm/neko-source/build.sh
```

Source/file hashes are in `artifacts/source-provenance.json`; build output and image metadata are under `artifacts/`. Both original and patched `manager.go` are retained. The archive is cached under `.cache/` for the build.

## Verified build result

Image `sidekicks-neko:microvm-loopback` is `sha256:c27c3aab3181aa24dad92efb70a707d3c24f019d63c60279e42e1654c5346146`.
The extracted binary is ELF64 little-endian Linux AArch64, dynamically linked, 37,488,024 bytes, SHA256 `ccbf8d7f2c10b584629349ddc8d882fe90a6471932d5b5c3d91d9cd75fcd8512`.
`go version -m` independently reports GOOS=linux / GOARCH=arm64 / CGO_ENABLED=1 and compiler go1.25.14. Official Go build-stage tag resolved to digest `sha256:2c4c60ef415fbfa5e90300722293bef36c5e63fae17570ce18f580af933dbd73` (recorded in build.log). No emulation was requested.

Build, exact-base/layer/config checks and binary architecture/hash verification **PASS**. The disposable extraction container was created, never started, and removed. No media/service test was performed by this build worker. Integration must verify ICE, decoding and input in the actual no-NIC guest before claiming a working microVM Neko viewer.

## Why the binary override is required

Neko's pinned `server/internal/config/webrtc.go` exposes ICE Lite, EPR, NAT1to1 and UDP mux, but no loopback-candidate setting. `server/internal/webrtc/manager.go` calls `SetNAT1To1IPs` but never enables loopback gathering. Its pinned dependencies are Pion ICE v4.2.2 / WebRTC v4.2.11. Pion excludes loopback interfaces by default, so an unpatched loopback-only guest has no EPR host candidates even with NAT1to1=127.0.0.1.

Primary source: [Pion interface filtering](https://github.com/pion/ice/blob/v4.2.2/net.go#L74-L80), [loopback setter](https://github.com/pion/webrtc/blob/v4.2.11/settingengine.go#L365-L368). UDP mux independently excludes loopback in `NewMultiUDPMuxFromPort`; it would require `ice.UDPMuxFromPortWithLoopback()`. This build intentionally uses EPR and does not enable mux.

## Exact v3 configuration for the copied guest

Preserve the copied base configuration and replace its WebRTC section with the settings below; do not replace desktop, capture, member or ownership settings. Use the same numeric UDP ports in the guest and host relay so no SDP/candidate port rewriting is needed. Proposed/reserved microVM range is 59120–59139, separate from baseline 59020–59039.

```yaml
webrtc:
  icelite: true
  icetrickle: true
  epr: 59120-59139
  udpmux: 0
  tcpmux: 0
  nat1to1: [127.0.0.1]
  ip_retrieval_url: ""
  iceservers:
    frontend: [{urls: ["stun:127.0.0.1:9"]}]
    backend: []
```

The nonempty local frontend STUN list suppresses Neko's default external Google STUN fallback; this dummy endpoint supplies no STUN service. ICE Lite needs no backend STUN. Host signaling and media relays must bind explicitly to 127.0.0.1. The guest has no NIC, only enabled `lo`.

The corresponding supported flags/environment names are:

| Flag | Environment | Value |
|---|---|---|
| `--webrtc.icelite` | `NEKO_WEBRTC_ICELITE` | `true` |
| `--webrtc.icetrickle` | `NEKO_WEBRTC_ICETRICKLE` | `true` |
| `--webrtc.epr` | `NEKO_WEBRTC_EPR` | `59120-59139` |
| `--webrtc.udpmux` | `NEKO_WEBRTC_UDPMUX` | `0` |
| `--webrtc.tcpmux` | `NEKO_WEBRTC_TCPMUX` | `0` |
| `--webrtc.nat1to1` | `NEKO_WEBRTC_NAT1TO1` | `127.0.0.1` |
| `--webrtc.ip_retrieval_url` | `NEKO_WEBRTC_IP_RETRIEVAL_URL` | empty in YAML or explicit empty flag |
| `--webrtc.iceservers.frontend` | `NEKO_WEBRTC_ICESERVERS_FRONTEND` | JSON `[{"urls":["stun:127.0.0.1:9"]}]` |
| `--webrtc.iceservers.backend` | `NEKO_WEBRTC_ICESERVERS_BACKEND` | JSON `[]` |
| `--server.bind` | `NEKO_SERVER_BIND` | `127.0.0.1:8080` |
| `--server.cors` | `NEKO_SERVER_CORS` | comma-separated exact allowed origins |
| `--config` | `NEKO_CONFIG` | `/etc/sidekicks/neko.yaml` |

Viper uppercases names and replaces dots with underscores. Empty environment values are not a dependable override; use the copied YAML or an explicit empty flag. A populated NAT1to1 also prevents IP retrieval. Neko's actual CORS YAML key is a list `server.cors: ["http://127.0.0.1:EXTERNAL_VIEWER_PORT"]`; nested `server.cors.allowed_origins` is not read by the v3 source.

## Starting the exported rootfs without Docker in the guest

Docker `export` does not preserve image environment metadata. The guest init must inherit/recreate:

```text
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
DISPLAY=:99.0
USER=neko
HOME=/home/neko
XDG_RUNTIME_DIR=/tmp/runtime-neko
PULSE_SERVER=unix:/tmp/pulseaudio.socket
NEKO_CONFIG=/etc/sidekicks/neko.yaml
NEKO_SERVER_BIND=127.0.0.1:8080
NEKO_WEBRTC_EPR=59120-59139
NEKO_WEBRTC_NAT1TO1=127.0.0.1
NEKO_WEBRTC_ICELITE=true
NEKO_WEBRTC_UDPMUX=0
NEKO_WEBRTC_TCPMUX=0
```

After mounting `/proc`, `/sys`, `/dev` (devtmpfs), `/run`, `/tmp`, `/dev/pts` and `/dev/shm` (512 MiB tmpfs), recreate `/tmp/.X11-unix` mode1777 and `/tmp/runtime-neko` owner1000:1000 mode0700. The `/tmp` tmpfs hides these image directories. Preserve `/var/log/neko` ownership and the `neko` user UID/GID1000. Bring up guest `lo`; do not add a NIC.

Start `/usr/bin/supervisord -c /etc/sidekicks/supervisord.conf` as root from the guest init. The five supervised programs run as user `neko`: Xorg :99, PulseAudio, desktop, Neko and Node adapter. Root PID1 must reap adopted children. The rootfs already contains these dependencies; no container engine or package installation is needed in the guest. The synthetic desktop waits for adapter health and creates its own disposable browser profile. Existing supervisor paths remain valid.

## Host, Origin and port adaptation in copied rootfs only

Keep internal adapter/viewer ports7792/7902 and private upstream8080 if desired. A vsock TCP tunnel can expose separate host API/viewer ports, but ordinary browser requests retain the external Host header. Update only the copied `/app/server.mjs`:

- HTTP Host allowlist (line57) must include actual external API/viewer ports as well as internal ports used by the fixture.
- WebSocket upgrade Host allowlist (line106) separately needs the external viewer/API ports; HTTP-only adaptation will make signaling fail.
- `/capabilities` viewerUrl and nativeViewerUrl must advertise the external loopback viewer endpoint.
- Preserve Origin checking, reader-token checks, signaling-event filters and the lease enforcement paths. The current Origin predicate accepts loopback HTTP origins; tighter explicit per-session origin sets may be used in the copied version.
- Viewer JavaScript uses `location.origin + "/stream"` and relative API calls, so this endpoint adaptation does not need a viewer rebuild.
- The existing CSP permits loopback WebSockets on any port and self resources. Its frame-ancestors allows only self and workbench7790; change only if a different embedding origin is intentionally supported.
- Raw Neko8080 must remain guest-only; do not export it or the admin token through the host bridge.

## Bounded UDP-to-vsock relay requirements

This is transport feasibility guidance, not a tested relay implementation. Neko media, DTLS, STUN, RTCP and native SCTP input share the ICE UDP path; relay datagrams opaquely without parsing media or decrypting.

- Bind host UDP sockets exclusively to127.0.0.1:59120–59139; check conflicts first. NAT1to1 changes advertised IP, not port. Keep host/guest destination port identical; different ports require rewriting both SDP candidates and trickle candidate messages.
- Frame each datagram over vsock with a bounded length, guest destination port and flow ID. Reject unknown ports and oversized/malformed frames. Stream reads are not datagram boundaries: read complete headers/payloads and handle partial writes.
- Maintain a stable bounded per-flow mapping keyed by host listening port plus browser source address/port. In the guest create a connected UDP socket bound to127.0.0.1:0 and send only to127.0.0.1:the-approved-Neko-port. Return packets through that flow to the original host browser tuple, using the original host listening socket/source port.
- Pion sees the guest relay socket's source tuple; ICE Lite can learn it as a peer-reflexive remote address. This NAT-like behavior needs actual ICE-selected-pair/media verification. Do not collapse all browser peers into one guest source tuple: it can mix return paths and invalidate ownership assumptions.
- Bound flow count, buffers and idle expiry; discard stale-generation frames on session restart. The relay must not become arbitrary UDP forwarding or provide guest/host egress. A stream tunnel can introduce head-of-line delay; measure rather than extrapolate baseline UDP timing.
- Acceptance needs fresh browser decoded VP8 frames, an open native data channel, real input/capture/reconnect, selected candidate addresses/ports, and native host revoke/lease tests. HTTP health alone does not establish media success.
