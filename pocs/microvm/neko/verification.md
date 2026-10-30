# Sidekicks Neko microVM UDP — builder verification

The previously unimplemented media path now runs in a separate no-NIC Virtualization.framework guest. Actual Chrome ICE selected UDP to 127.0.0.1:59122 and decoded 1280×720 video; after an explicit guest/relay stop and new-kernel start, a fresh browser selected UDP port 59137 and decoded increasing frames. This is UDP at the edges transported inside reliable ordered vsock streams. It is not native VM UDP networking or demonstrated parity.

The live demo is http://127.0.0.1:7906/ (gated input), http://127.0.0.1:7906/?mode=native (official Neko SDK data-channel input), adapter http://127.0.0.1:7796. Host media binds only to 127.0.0.1:59120–59139. No host firewall, routing, VPN, NAT, security permissions, persistent credentials or cloud resources were changed. Original Docker demos and noVNC VM A remain unchanged/live. Independent raw artifacts now record initial media 4/4, functional 9/9 and lifecycle 5/5 checks passing, with fresh media/capture included in lifecycle. Preflight and final state also pass. The independent report is frozen and its live window released; parent review remains pending.

## Implementation and origin

Neko 3.2.0's pinned Pion defaults exclude loopback ICE gathering. A single isolated source insertion enables `SetIncludeLoopbackCandidate(true)` in the EPR path; no UDP mux/TCP mux fallback is enabled. Accepted ARM64 base image a7120e…fec85 is unchanged; separate image c27c3a…46146 adds only the rebuilt binary (SHA256 ccbf8d7f2c10b584629349ddc8d882fe90a6471932d5b5c3d91d9cd75fcd8512). Official pinned source archive, original/patched source, exact diff, Go dependencies/compiler, ELF verification, base layers and build logs are retained in pocs/microvm/neko-source. Primary upstream references: [Pion loopback filtering](https://github.com/pion/ice/blob/v4.2.2/net.go), [Pion SettingEngine](https://github.com/pion/webrtc/blob/v4.2.11/settingengine.go), [Neko WebRTC configuration](https://neko.m1k1o.net/docs/v3/configuration/webrtc).

Rootfs is Debian 13 (trixie), copied from the accepted Neko source-built image. The 4 GiB ext4 guest disk adds copied init/config/endpoint adaptations; 2 vCPU / 2 GiB and Linux 6.12.28 boot asset match the first-level VZ design. There is no NIC, guest external egress, shared host directory, host home/engine socket, USB or GPU. Guest desktop and exec use uid 1000; PID1/root supervisor agent remains a POC boundary. No Docker daemon runs in the guest. This remains direct Virtualization.framework, not Apple container CLI, Containerization, Kata, nested KVM, GKE or EKS. Host macOS 15.3 and Apple CLI prerequisites remain unchanged.

## Relay semantics and bounds

One hostUDPmedia-port/client-loopback-port pair receives one private stream, one two-byte network-order datagram framer and one connected guest UDP socket. Port and peer association is stable for that stream; empty datagrams are preserved. Host replies use the same bound listening port and only the endpoint which first sent a packet. No payload selects an address. Guest destination is fixed to 127.0.0.1 within 59120–59139; outside-range requests are rejected. Private host UDS is mode 0600 under mode 0700 state. Host caps 64 peers, 262144 queued bytes per direction/peer and 60-second idle expiry. Stream backpressure drops excess UDP rather than allowing unbounded queues. Only one Neko media range/session is supported at a time.

A reliable ordered stream changes UDP loss/reordering/congestion and introduces head-of-line behavior. No WAN/loss/jitter/congestion or matched media-latency/performance comparison was run. Native physical Mac gestures, audio waveform/physical listening, hostile escape/exhaustion, production multi-tenant hardening and GPU/snapshots remain untested. Generic takeover revokes new bot injection and cancels future queued events; already delivered application events may still render later.

## Builder tests and preserved failure

- **9 E2E checks passed:** actual selected ICE UDP/decoded frames; gated browser and native SDK keyboard/text/modifier/click/right/double/drag/drop/scroll/release; unleased native host request denial; normalized automation; copied-session/forged token/Origin/Host denial; human preemption; simultaneous human/agent ownership; fresh bounded 640×360 JPEG capture. Raw E2E evidence includes transport and fixture state.
- **8 transport and lifecycle checks passed:** separate kernel/resources; non-root exec/noNIC/share/socket; empty, binary and 8192-byte real-VM datagrams and two-peer separation; outside-range denial; actual 20 loopback host UDP listeners; stop closes all 20; new-kernel restart restores health/fixture/relay; original accepted containers and A boot unchanged and endpoints healthy.
- **2 checks after restart passed:** fresh browser selects UDP and decodes frames 4→64 over~2s, DTLS connected, plus fresh bounded capture. Short raw RTC observation reports packetsLost=0 and framesDropped=17; retained without causal/comparative inference.
- **4 unit checks passed:** fragmentation/coalescing/empty/max framing, malformed/oversized frame and invalid destination rejection, real local UDP peer separation. Unit frame capacity is not host payload support.

The first 65507-byte end-to-end datagram attempt failed on macOS send with EMSGSIZE. The failed report/log remain transport-attempt1-host-max-datagram.json/log. Read-only `net.inet.udp.maxdgram` is 9216: host-local 8192/9216 PASS, 9217/65507 FAIL. No sysctl/security setting was changed. Actual guest-bound roundtrip is proven through 8192 bytes; 9216 has only host-local proof. Oversized host return packets are dropped/counted.

Cached prepared first startup observed agent 1.30 s / health 4.03 s / fixture 5.57 s; reboot 0.36 / 1.95 / 3.59 s. These are host polling boundaries excluding build/export/disk preparation, not first decoded-frame time or a comparative benchmark.

## Reproduce and clean up only this guest

```bash
bash pocs/microvm/neko-source/build.sh
bash pocs/microvm/prepare-neko.sh
python3 pocs/microvm/microvm.py create neko 7906 7796 neko # only if absent
python3 pocs/microvm/microvm.py start neko # only if stopped
node pocs/microvm/neko/e2e.mjs
python3 pocs/microvm/neko/verify-transport.py # includes Neko-only restart
node pocs/microvm/neko/smoke.mjs
python3 pocs/microvm/microvm.py capture neko /tmp/neko-vm.jpg
python3 pocs/microvm/microvm.py stop neko
python3 pocs/microvm/microvm.py destroy neko # optional, only when stopped
```

Preparation uses named unprivileged/no-network disposable export/disk helpers with no host mounts; successful preparation removes them. The task-local signed runner/relay use detached processes/logs, without autostart service. Stop validates exact runner/relay PID commands, syncs only this guest and releases UDP/private endpoints; no broad kills/prunes. Read README for asset caches and safe retry/diagnostics. Screenshots remain local QA evidence and are not uploaded to Library.

## Source/runtime provenance note

The prepared guest module hash is57e673fb86692fdc8098118f40a28962f28d801bfbbd1f4ee001789e253ef54f. After preparation, the host-only UDP reply error handling was broadened from BlockingIOError to OSError to count/drop an oversized Mac UDP send. The current host module hash is0b08cb4509fd7c67762cbc38ef43e2c5f52ad7659e726670e97f9717c0159a00 and was loaded on the builder restart. The guest_udp function AST is identical in both files, independently recorded in relay-source-provenance.json. No guest runtime was silently replaced during QA.

## Independent control observation

The independent functional run records 9 PASS checks. In its takeover observation, the controller injection count remained 315→315 after acknowledgement, while fixture snapshots showed text length 27→38, separated by a 650 ms observation wait. This does not distinguish application processing from fixture-telemetry settling or establish delivery latency. Injection cutoff passed; application-queue flush was not established and is not promised. Human readiness is an ownership acknowledgement, not proof every prior application event has rendered. This is retained as an explicit limitation. Independent lifecycle and final-state artifacts also pass. This remains a reviewable POC with documented limits, rather than production or generic application-drain acceptance.
