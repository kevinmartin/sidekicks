# Neko UDP through a first-level Virtualization.framework VM

Builder and independent verification passed within the documented scope. Source and runtime are frozen for parent review. Actual WebRTC UDP media and decoded frames were observed before and after restart. This is a separate `neko` session at viewer7906/API7796 and UDP127.0.0.1:59120–59139. Existing noVNC VM A and accepted Docker services stay running.

The guest has no NIC, host filesystem share, host Docker socket or external egress. Host UDP datagrams are sent over a private0600 Unix socket through the runner to virtio-vsock8000. Each learned `(host media port, loopback client port)` gets a separate reliable stream and a separate connected guest UDP socket. The guest destination is fixed to127.0.0.1 and the20-port Neko media range; responses go only to the original host peer. Two-byte network-order length framing preserves empty/full datagrams and boundaries. Peer count64, per-direction queued stream bytes262144, idle expiry60s. No destination IP or arbitrary host endpoint is accepted from payloads.

This provides UDP at the browser and Neko edges. The VM boundary carries framed data over reliable ordered vsock streams, so head-of-line blocking and different loss/congestion behavior remain. This is not end-to-end native UDP networking or evidence of performance parity.

Pinned Neko3.2.0 excludes loopback ICE candidates by default. An isolated copied-source build enables Pion's loopback-candidate option; accepted Neko image/files are unchanged. Provenance/patch is in ../neko-source. No TCP WebRTC fallback is configured.

Preparation/start (only after isolated patched image is built):

```bash
bash pocs/microvm/prepare-neko.sh
python3 pocs/microvm/microvm.py create neko 7906 7796 neko
python3 pocs/microvm/microvm.py start neko
python3 pocs/microvm/microvm.py status neko
python3 pocs/microvm/microvm.py exec neko -- uname -a
python3 pocs/microvm/microvm.py capture neko /tmp/neko-vm.jpg
```

The media range is fixed; run only one Neko profile at a time. Exact POC cleanup:

```bash
python3 pocs/microvm/microvm.py stop neko
# Optional removal after stop; leaves all other sessions/containers alone.
python3 pocs/microvm/microvm.py destroy neko
```

`stop` verifies the relay PID/command, closes its UDP listeners, syncs/stops only this VM and removes its private sockets. State/logs and relay counters are under ../.state/neko. No autostart service, firewall, host routing, VPN, NAT or security setting is changed.

## Builder evidence

- `../artifacts/neko-udp/e2e.json`:9 PASS checks. Actual selected ICE pair is `udp`, remote127.0.0.1:59122, succeeded/nominated; real1280x720 decoding. Browser gatedHTTP and official nativeSDK data-channel each exercised typing/modifiers/click/right/double/drag/drop/scroll/held release. Normalized automation, human preemption, stale/forged/session/Origin/Host denial and bounded capture were separately exercised. Generic injection cutoff is the guarantee; already delivered application input may render later.
- `transport.json`:8 PASS checks. Distinct kernels, non-root exec and no NIC/share/socket; empty/binary/8192-byte UDP plus two-peer separation through actual VM boundary; outside-range destination rejected; actual20loopback UDP listeners; stop freed all20ports; restart new boot+healthy fixture/relay; accepted containers and A boot unchanged.
- `post-restart-stream.json`:2 PASS checks. New browser selected UDP127.0.0.1:59137/DTLSconnected and decoded frames4→64 over~2s; fresh bounded capture. Raw RTC stats retain packetsLost0 and framesDropped17 in this short observation; no causal or comparative claim.
- `relay-unit.txt`:4 PASS unit checks, covering fragmented/coalesced/empty/max-size framing, invalid lengths/destination and real two-socket guest-path separation. Unit framing capacity is not host datagram capacity.

Guest OS is Debian13(trixie), exported from accepted Neko source imagea7120e…fec85 with only the isolated loopback-capable binary added (new imagec27c3a…46146). Kernel6.12.28 is the same cached Kata release boot asset as the original VZ POC, not a Kata runtime. Guest runtime rootfs/init/config adaptations are copied-only. Sources, hashes and exact patch are in ../neko-source and ../artifacts/neko-udp/source-hashes.json. Build is cached/local; first prepared start observed agent1.30s/health4.03s/fixture5.57s, a reboot0.36/1.95/3.59s. These stop at host RPC/health/fixture polling, exclude image build/export/disk preparation and are not a benchmark.

## Preserved limits/failure

The initial65507-byte real-UDP test failed with macOS EMSGSIZE. Read-only `net.inet.udp.maxdgram` is9216; host sends8192/9216 pass,9217/65507 fail, with no sysctl change. Evidence: transport-attempt1-host-max-datagram.json/log and host-datagram-limit.json. End-to-end VM test passes up to8192bytes;9216 was tested only host-local. Neko media packets are smaller; this does not prove arbitrary UDP payload support. Oversized return packets are dropped/counted rather than closing a peer.

Reliable vsock adds head-of-line/retransmission behavior and cannot reproduce native UDP loss/reordering semantics. No WAN/loss/jitter/congestion/multi-user performance parity test, audio waveform/physical listening, physical Mac gestures, hostile escape/resource exhaustion or production hardening was performed. Loopback-only peers, one configured Neko media range and only one active Neko profile are supported. Already-injected application backlog is not rolled back. No additional desktop/runtime feature is implied by these tests. Screenshots stay repo-local; written report only is eligible for Library update after independent QA.

Independent functional takeover observation: controller input frames stayed315→315 after acknowledgement while already delivered fixture text grew27→38 during650ms follow-up. The generic guarantee is new-injection cutoff and future-command cancellation, not application queue flush or stable text immediately on handoff. This caveat remains even when the visible owner changes to human.

## Independent closeout and review delivery

Independent preflight, initial media4/4, functional9/9, lifecycle5/5 and final state PASS. The post-restart observer has5 subchecks within lifecycle; do not double-count them. The full independent report is artifacts/independent-qa/microvm/neko-udp/report.md in the repo root, SHA2563414735c38a4ce3b2b7414e9cd668c1eac20708007115e7af005304a20483b72. The guest remains running, unowned, with all actual X11 keycodes/buttons empty; A and accepted containers unchanged. No further runtime work is planned.

The combined written report is saved to the same microVM Library identity, version1; original report history and original POC identities remain preserved. Screenshots were not uploaded. Local native-terminal.png was personally inspected by integration/QA, showing the actual guest xterm and exact unique output; path is in artifacts/neko-udp/handoff-summary.json. Final acceptance remains parent review.
