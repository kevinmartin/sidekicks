# Neko VM with Apple NAT networking

A separate, opt-in `neko-nat` profile runs as session `nat`, preserving the noVNC VM A, relay-backed Neko VM and accepted Docker demos. Host viewer/signaling/control remain loopback/vsock at http://127.0.0.1:7907/ and API7797; WebRTC media is actual UDP directly to the guest DHCP address, ports59220–59239. Guest IP is reported by the vsock agent; it is not hardcoded.

**NAT enables egress. It is not an egress allowlist or host/LAN isolation boundary.** Tests use only the task's guest IP plus known benign public DNS/HTTPS destinations. No private network scanning, personal services, arbitrary browsing, host firewall/routing/VPN/security settings or persistent credentials are involved.

The same cached Debian13/Neko3.2.0 rootfs, patched Neko binary, Firefox, VP8 interactive codec,1280×720@30 and2vCPU/2GiB/4GiB disk as the relay are used. The NAT profile adds one `VZVirtioNetworkDeviceConfiguration`/`VZNATNetworkDeviceAttachment`, guest udev/networkd DHCP, guest route/DNS setup and per-boot candidate address configuration. No UDP relay exists for this guest. Exec/files/capture and the viewer API still use the existing private vsock transport. Basic NAT uses the existing virtualization entitlement; no restricted bridging entitlement was added.

```bash
bash pocs/microvm/prepare-nat.sh
python3 pocs/microvm/microvm.py create nat 7907 7797 neko-nat # if absent
python3 pocs/microvm/microvm.py start nat # if stopped
python3 pocs/microvm/microvm.py rpc nat '{"op":"info"}'
node pocs/microvm/nat/e2e.mjs
python3 pocs/microvm/nat/internet.py
python3 pocs/microvm/nat/lifecycle.py # NAT-only restart; needs exclusive window
python3 pocs/microvm/microvm.py stop nat
python3 pocs/microvm/microvm.py destroy nat # optional, stopped only
```

Builder checks:9 E2E PASS (actual direct ICEUDP/decoded video, native/gated input, takeover/rejects/capture),5 lifecycle PASS (normal stop, repeatable guest DHCP/config, fresh media/capture/internet after restart and protected baseline unchanged). Public `example.com` and `www.iana.org` resolved to public IPv4 and returned HTTPS200 using normal TLS trust. No unexpected permission prompt occurred on start.

Initial DHCP timeout/PID1 panic is preserved in artifacts/nat/attempt1-network-console.log and stopped `.state/nat-attempt1`. The custom PID1 initially omitted udev initialization required by networkd. Guest-only udev net processing repairs this; subsequent setup failures are reported via the agent instead of killing PID1. Per-boot NAT address replacement validates an IPv4 value and supports later DHCP addresses; successful restart verified. No existing VM or host networking setting was used as a repair.

Independent functional9/9, lifecycle5/5, final-state9/9 checks and a fresh120/120 matched local comparison are complete. Old Docker/relay timing numbers were not reused. Host resource attribution must use each actual Virtualization XPC service identified by its exact open task disk, with runner/relay/browser components separated. `artifacts/nat/host-vm-process-map.json` records the builder mapping; refresh after restart. A short idle/control window and bounded guest-local CPU load are permitted; no synthetic WAN impairment.

Guest root PID1, browser sandbox/output-buffer/concurrency limitations remain POC constraints. Physical gestures/listening, arbitrary network policy, production hardening and cloud compatibility are not implied. Screenshots stay local; only the existing written Library report will be updated after verification.

## Reviewed results and frozen delivery

The first fixed-order comparison has30/30 valid samples per window, no discarded outliers/timeouts. Standard median/p95/max ms: relay-control92.00/94.60/102.40; NAT-control89.55/127.00/127.80; NAT-load116.85/212.20/269.10; relay-load141.00/271.80/340.70. Typical observation interval16.6ms. Control medians are close and NAT has a higher control p95; loaded NAT is lower in this run. This is a local whole-route observation with order/warm-age/cache confounds, not proof of transport causality or universal superiority.

Short passive media windows show reported loss/freeze deltas0 and no observer gap≥250ms. CPU/RSS/media details and incomplete first-block Chrome process accounting are in verification.md and the frozen independent report artifacts/independent-qa/microvm/nat/report.md (repo root). Builder recomputation and sample/counter/source checks passed; raw data are unchanged.

The browser internet helper used `/fixture/` and failed restoration after successfully rendering the public page; preserved FAIL. A separate normal restart restored fresh fixture/media. Generic application-queue flush is not promised (NAT snapshots33→37 afterACK while controller313→313). Full limitations/unrun cases remain explicit.

NAT and relay remain live, unowned, all actual X11 keys/buttons empty; exact load workers and owned Chrome are gone, lock removed, protected baselines unchanged. Source/runtime/test work is frozen. Existing written microVM Library report is version2, same identity; screenshots were not uploaded. Parent review remains pending.

Recompute the recorded raw run without changing it:

```bash
python3 pocs/microvm/nat/review-results.py artifacts/independent-qa/microvm/nat/comparison-2026-10-26T14-44-44-063Z
```

Future active reruns require an exclusive window. Review the QA protocol and inspect any existing process/lock/partial manifest after interruption; do not blindly repeat. Cleanup remains `python3 pocs/microvm/microvm.py stop nat`, only this guest.
