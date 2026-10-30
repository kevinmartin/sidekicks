# Apple NAT versus relay: Sidekicks microVM POC

**Functional NAT and the independent local comparison are complete.** The first fixed-order run retained120/120 valid observations. Builder arithmetic/integrity review independently matched every median, nearest-rank p95, CPU delta and interval jitter-buffer calculation; no performance superiority is established from this single run.

The opt-in session `nat` uses basic Apple `VZNATNetworkDeviceAttachment`, one virtio NIC, guest DHCP/routes/DNS, and the same 2 vCPU / 2 GiB / 4 GiB Neko desktop software as the existing relay guest. Host viewer/signaling/control stay at http://127.0.0.1:7907/ and API7797 over the private vsock channel. Media goes directly from the task browser to the guest's assigned private IPv4, ports59220–59239; there is no host UDP relay for this guest. The assigned address is returned through vsock info and refreshed each boot, not assumed from a subnet or ARP table.

The builder observed actual selected ICE UDP192.168.64.4:59231 and decoded1280×720 video. Nine end-to-end checks passed, separately exercising native SDK data-channel and gated HTTP input, normalized automation, human takeover/rejection and fresh bounded capture. Five lifecycle checks passed, including NAT-only stop/restart, repeatable guest setup, fresh direct media/capture/internet and unchanged protected A/relay boots and Docker state. These are builder results, separate from independent QA below.

The guest resolved `example.com` and `www.iana.org` to public IPv4 addresses and returned HTTPS200 with normal certificate verification, bounded response bodies and an explicit benign redirect allowlist. No user accounts, cookies or private-service requests were involved. NAT enables outbound network connectivity; it is not an egress allowlist or proof of host/LAN isolation. Tests do not scan private networks. No host firewall, routing, VPN, security setting, persistent credential or paid resource was changed. Framework-managed NAT resources are distinct from manually editing host networking. No new OS permission prompt or restricted networking entitlement was needed on this Mac.

## Why the design changed

The earlier no-NIC/vsock choice was conservative scope, not a tested rejection of NAT or measured performance superiority. Reliable stream framing changes UDP ordering/loss semantics and can add head-of-line delay. Basic Apple NAT is available on this macOS15.3 host; it does not require the restricted bridged-network entitlement. [Apple NAT API](https://developer.apple.com/documentation/virtualization/vznatnetworkdeviceattachment), [Apple DTS entitlement clarification](https://developer.apple.com/forums/thread/710763). This is direct Virtualization.framework, not Apple container CLI, Containerization, Kata or cloud validation.

## Preserved implementation failure

The first NAT VM started successfully, but guest networkd waited for udev initialization omitted by the custom PID1. DHCP timed out and PID1 exited, causing a guest panic. The stopped attempt and console are preserved under `.state/nat-attempt1` and `artifacts/nat/attempt1-network-console.log`. Guest-only udev net initialization repairs the startup; subsequent setup errors are reported through the agent instead of killing PID1. Per-boot config validates and replaces the previous IPv4 entry, rather than requiring the original loopback value. Reboot succeeded; the observed DHCP address stayed the same, so changed-address behavior is supported by the validated generation path rather than claimed from an observed address change.

## Matched comparison protocol

Fresh relay and NAT observations use the same pinned Neko binary, Firefox/fixture, VP8 interactive encoder, desktop1280×720@30, resources and task-owned Chrome native SDK input route. The required network/init/candidate differences are recorded. Existing VMs remain up idle as a shared background; viewers and guest load run sequentially. The order is relay-control, NAT-control, NAT-load, relay-load. There are at least30 measured attempts per block, declared warmup, an8-second passive media/resource window, and one bounded guest-only CPU worker for the load condition. No WAN impairments are manufactured.

Latency begins at native-overlay mousedown and ends at the first observed decoded change in the same synchronous click-counter region, sampled using the same browser performance.now clock and rAF. Counter increments are checked separately. All samples, failures/timeouts, ROIs and sampling intervals are retained. Standard median averages the middle pair; p95 uses nearest rank. This is local input-to-observed-frame latency, not ping, screenshot HTTP time, physical human display latency or isolated transport causality.

RTP jitter, interval jitter-buffer delay, packet loss, native freeze counters, rVFC gaps and observer rAF gaps remain separate. Missing fields are unavailable; negative loss deltas are retained. Host CPU uses cumulative time deltas for the exact VM XPC process identified through its open disk, plus separate runner/relay/task-browser components. RSS is a process observation, not unique physical RAM or configured guest capacity. Short windows and fixed ordering limit inference.

## Reproduce / stop only this POC

```bash
bash pocs/microvm/prepare-nat.sh
python3 pocs/microvm/microvm.py create nat 7907 7797 neko-nat # if absent
python3 pocs/microvm/microvm.py start nat # if stopped
python3 pocs/microvm/microvm.py rpc nat '{"op":"info"}'
node pocs/microvm/nat/e2e.mjs
python3 pocs/microvm/nat/internet.py
python3 pocs/microvm/nat/lifecycle.py # exclusive window, NAT-only restart
python3 pocs/microvm/microvm.py stop nat
python3 pocs/microvm/microvm.py destroy nat # optional, stopped only
```

NAT direct UDP avoids the relay's stream framing, but no superiority is inferred in advance. Application input queues are not generically flushed by takeover. Browser/PID1/concurrency/output-buffer hardening, physical trackpad/audio listening, WAN behavior and production network policy remain outside the demonstrated scope. Screenshots stay repo-local; only the existing written microVM Library report is updated after verification.


## Reviewed local observations

One run, fixed order relay-control → NAT-control → NAT-load → relay-load,30 attempts each, all valid; zero timeouts/invalid attempts. Each active load block kept its exact bounded guest CPU worker alive through the final sample and stopped it afterward. Standard median averages the middle pair and p95 uses nearest rank. Each observation separately confirmed exactly one fixture-counter increment. The timed endpoint equals the summed browser polling intervals.

| Window | Valid/attempts | Median ms | p95 ms | Max ms |
|---|---:|---:|---:|---:|
| relay-control |30/30|92.00|94.6|102.4|
| nat-control |30/30|89.55|127.0|127.8|
| nat-load |30/30|116.85|212.2|269.1|
| relay-load |30/30|141.00|271.8|340.7|

Typical latency observation interval was16.6ms for every block. Control medians differ by only2.45ms, smaller than that interval, and NAT's control p95 is higher. NAT's loaded median/p95 are lower in this run; fixed order, one run,30 observations/block, VM age, warm caches, host scheduling and whole-pipeline differences prevent a causal transport or general performance conclusion. No WAN/loss impairment was introduced. This does not measure the magnitude of head-of-line effects under packet loss.

Media/resource windows were about8seconds each, separate from timed clicks:

| Window | RTP jitter median/p95 ms | Interval jitter-buffer mean ms | Reported packet-loss delta | Native freeze delta | Max observer frame gap ms |
|---|---:|---:|---:|---:|---:|
| relay-control |3.0 / 16.0|20.45|0|0|83.4|
| nat-control |2.0 / 8.0|14.25|0|0|83.4|
| nat-load |8.0 / 27.0|21.42|0|0|183.3|
| relay-load |18.0 / 24.0|38.76|0|0|166.6|

All windows reported zero additional decoder dropped frames and no observer video gaps≥250ms. Short-window zero counts are observations, not a guarantee of no rare freezes/loss. RTP jitter, browser buffering, native freezes and observer gaps have different meanings; the longest observer gap was actually larger for NAT-load than relay-load. All raw counters, timestamps, SSRC and fields are retained. [W3C metric definitions](https://www.w3.org/TR/webrtc-stats/).

| Window | VM XPC core-% | Runner core-% | Relay core-% | Available Chrome component core-% sum | VM XPC median RSS MiB |
|---|---:|---:|---:|---:|---:|
| relay-control |54.26|0.38|1.00|18.80 (incomplete)|1098.4|
| nat-control |41.74|0.00|not applicable|11.50|645.8|
| nat-load |139.64|0.00|not applicable|11.09|531.5|
| relay-load |150.83|0.25|0.87|10.49|550.5|

100 core-% means one CPU core. The first relay-control Chrome initial-component sum is incomplete because a late Unzipper utility process was not in its initial set; the other three observed sets stayed stable. These values must not be treated as whole-browser totals when the process set changes. The actual VM XPC helpers were identified by exact open guest-disk paths; CPU is cumulative-time difference divided by measured elapsed time. Chrome components use task-owned process counters, separately retained. RSS can double-count shared pages and is not unique physical RAM or configured guest capacity. Relay-control RSS ranged~718–1272MiB and changed substantially during the window; older relay VM age and first-window browser warmup/cache state confound apparent efficiency differences. The relay Python process itself consumed only~1 core-% here; the larger whole-VM differences must not be attributed solely to it. This is a scoped observation, not proof that NAT saves memory or CPU.

Raw directory: `artifacts/independent-qa/microvm/nat/comparison-2026-10-26T14-44-44-063Z`. Builder recalculation: `pocs/microvm/artifacts/nat/statistics-review.json`; source/fact/counter integrity: `raw-integrity-review.json`. Raw JSON was not edited.

## Independent functional closeout and preserved helper failure

Independent functional9/9, NAT lifecycle5/5 and final-state9/9 checks PASS. Public DNS/TLSHTTPS and actual public-page rendering in guest Firefox were verified. The browser helper then attempted `/fixture/` rather than the actual `/fixture` path and timed out restoring the fixture; that failed attempt remains visible. A separate NAT-only restart successfully restored a fresh fixture, direct media/capture and internet. A passing restart does not erase the helper failure.

Both NAT and relay are left running, ownernull with all actual X11 keycodes/buttons empty. Exact CPU-worker PIDs are gone, the owned headless Chrome is closed, and the comparison lock is removed. Accepted container identities/images/start/ports and A/relay/NAT boot IDs stayed unchanged across measurement. No further runtime work is planned. Integration personally inspected the actual public webpage and native guest terminal images; screenshots remain local and are not uploaded. Parent review remains pending.

The independent NAT takeover diagnostic also retains text snapshot growth33→37 after a650ms observation wait while controller sends stayed313→313. Queued/stale suffixes did not execute. This is injection cutoff with a failed strict stable-text diagnostic, not generic application draining or a measured final delivery time. All known limitations remain visible. RSS values below are resident-process observations; compression is an interpretation confound, not an independently measured memory pool.
