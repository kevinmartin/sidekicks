# Independent microVM verification

Latest revision: **2026-10-26 05:39:46 EDT** — targeted discovery/readiness regression **5/5 PASS**. A now advertises 7903 and a fresh QA guest advertises 7904, each matching its outer manifest. The prior URL metadata failure is fixed for the tested revision. Fast-shortcut reliability remains unresolved; the passing paced result does not erase that failure.

The original 6-viewer/12-boundary checks below were performed on the **prior revision**. After the endpoint fix and template refresh, only the five targeted checks in the addendum were run; no full-suite, input, screenshot or benchmark rerun is claimed. The original report is preserved [unchanged](../artifacts/independent-qa/microvm/report-before-discovery-fix.md), SHA256 `f5d7190cea6a98937866ccf35c526a99aba3583f85d162a0e7b01e9af7754c99`. Original raw results and metadata FAIL remain untouched.

**Library scope: report only.** The native-terminal image remains local; no microVM screenshot is authorized for upload. Original accepted-POC Library identities are preserved.

Initial core verification completed **2026-10-26 05:26 EDT (09:26 UTC)**. The local direct-Virtualization.framework demonstration passed **6 independent viewer checks and 12 independent boundary/lifecycle checks**. Known failures and unsupported features remain explicit below. This is a local POC, not production multi-tenant hardening or an adversarial escape assessment.

Live demo: **http://127.0.0.1:7903/**. Adapter: **http://127.0.0.1:7793**. Session A is left running, owner `null`, with no held X11 keycodes/buttons. QA session `qa-b`, builder session B and Docker comparison helpers are removed. Accepted noVNC/Neko/workbench containers were unchanged and their services returned200 at final inspection.

## What actually runs

This is a first-level ARM64 Linux VM created directly with Apple Virtualization.framework. It has an exported Debian 12 noVNC desktop filesystem, Linux 6.12.28, Xvfb, Openbox/tint2, x11vnc, Chromium and xterm. The host bridges loopback TCP to guest virtio-vsock; the fixture runs in **guest Chromium**. It is not merely a host-served fixture or an image pretending to be a desktop.

Independent evidence includes an actual decoded 1280×720 RFB frame, typed fixture text and a real xterm receiving a bounded viewer-typed command. The command wrote a unique marker inside guest `/workspace`; a separate private RPC read returned that exact marker. Guest process inspection found Xvfb/Openbox/x11vnc/Chromium/xterm. I personally viewed both saved viewer and terminal screenshots.

Host inspection independently returned macOS 15.3 (24D60), arm64, absent standard Apple CLI paths, and the runner's virtualization entitlement. The current Apple `container` CLI supports macOS 26; current Containerization requires macOS 26 / Xcode 26. That path is prerequisite-blocked, not denied approval. No OS upgrade or unsupported install was attempted. This fallback is distinct from those products and from Kata runtime. [Apple requirements](https://github.com/apple/container#requirements), [Containerization requirements](https://github.com/apple/containerization#requirements).

## Independent result matrix

| Check | Result | Actual evidence / boundary |
|---|---|---|
| Live decoded viewer | PASS | Fresh RFB connection over TCP → vsock,1280×720 framebuffer; viewer.json. |
| Viewer keyboard/pointer | PASS with pacing | Exact replacement of pre-existing text, Control modifier and click counter; final marker `independent-microvm-1793006503074`. Fast-sequence failure below remains separate. |
| Fresh image capture | PASS | Real 1280×720 PNG, 216,917 bytes, source 1280×720; decoded/captured marker `SK-mvp1exy8` / probe 2 agreed at magenta pixel. No model API. |
| Native terminal | PASS | Viewer opened actual xterm and typed `printf`/`cat`; independent file read returned `native_microvm_qa_1793006505169`. File removed afterward. |
| Ownership/stale input | PASS | Human takeover released a real held Shift; stale agent request 409; direct unleased RFB key packets increased dropped counter 3 → 5. |
| Final held state | PASS | Independent XQueryKeymap found **no pressed keycodes**, XQueryPointer no held buttons; owner `null`, viewers 0. |
| Two concurrent VM kernels | PASS | A boot `03d1e24c-9a7e-4aa8-8a37-6571a404380c`; QA guest boot `ed029ee1-35b6-40bd-b3c8-58c37853f332`; distinct disk inodes. |
| Resources | PASS for configured/guest-visible resources | Both guests: 2 vCPUs, MemTotal 2,045,784 kB; each configured for 2 GiB, separate 3 GiB raw disk. This is not total host-memory accounting or dedicated physical cores. |
| Host-path/network exposure | PASS for inspected boundary | Exec uid 1000; host home/Docker/containerd sockets absent; no virtiofs/9p/host path mounts; only `lo`, route table empty. Config contains no NIC/shared directory. |
| RPC locality | PASS / source reviewed | Session directories 0700, Unix sockets 0600. Host TCP destination prefixes fixed to guest 6080/8080; private RPC executes guest commands, not host shell. |
| Guest files/exit code | PASS | Exact file roundtrip, marker absent in A, outside-workspace paths rejected, guest exit 17 preserved. |
| Crash containment/recovery | PASS | SIGKILL only QA guest; A retained same boot ID. QA restart got new boot `ca4a84fe-de7f-468d-b16c-07c0685747a7`. |
| Synced persistence | PASS | **Explicit guest sync before crash**; exact marker survived. Does not establish unsynced-write durability. |
| Stop/reset/cleanup | PASS | Sync/power-off removed runner/RPC/listeners; destroy/create removed marker; recreated guest boot `da43c457-4528-475d-bf84-b32fb908e871`; QA guest then stopped/destroyed. |
| Docker versus VZ kernel boundary | PASS | Existing Docker desktops share one kernel boot ID, distinct from all tested VZ boots. No accepted container mutation. |
| Actual host listeners | PASS | Only 127.0.0.1:7903/7793 remain; 7904/7794 closed. No NIC/host networking change was used. |
| Accepted POCs/workbench | PASS | IDs/images/start times/restart counts/ports unchanged during independent lifecycle; final 7790/7791/7792 HTTP 200. |
| Alternate-session URL metadata | **Historical FAIL; targeted fix PASS** | QA guest/capabilities advertises 7903 while its correct outer manifest says 7904. Use manifest/endpoints; no shared-disk/control conclusion follows from this metadata error. |
| Immediate click→Ctrl+A→type | **FAIL in one retained attempt** | Existing text was appended to despite logged `ctrl=true` events. Cause remains unestablished. Paced repeat replaced the complete previous string and passed; no implementation fix or full fast-input reliability claim. |
| Unsynced crash durability | **Observed FAIL in builder run** | Acknowledged recent/workspace marker lost after SIGKILL. Not independently repeated. File write acknowledgment is not an fsync fence; normal stop syncs. |
| Apple CLI / Neko UDP / egress | UNSUPPORTED here | Current Apple CLI OS prerequisite unmet. No guest NIC/egress; bridge TCP-only, so no Neko UDP/WebRTC success. |
| Snapshots/GPU/cloud/Kata runtime | UNSUPPORTED / NOT RUN | No snapshot restore, GPU, cloud deployment, nested KVM or Kata runtime validation. |
| Physical gestures/audio listening/security stress | NOT RUN | No native Mac cursor/user tabs, physical trackpad/listening, kernel escape, CPU/memory exhaustion or hostile-output tests. |
| Full gesture/input suite and input latency in VM | NOT RUN | No new 30-sample latency, wheel/drag/pinch/resize/quality benchmark; accepted adapter tests do not automatically prove those on this VM. |

The successful paced shortcut waited 150 ms after the pointer click, held Control 40 ms before a, used 40 ms key press, waited 40 ms before Control release and 100 ms before typing. It is a separate observation from the retained fast failure. The first independent attempt also hit a QA-only JSON-parser error on the expected **plain-text 409** rejection; that attempt is preserved, and the corrected parser rerun is separate evidence.

## Startup and resource method

Independent QA measured three individual starts of its own disposable guest using host monotonic time around the existing start function:

| Prepared/cached observation | Agent ready | DesktopHTTP ready | Fixture telemetry ready |
|---|---:|---:|---:|
| Fresh QA disk |0.713s|1.472s|2.711s|
| Recovery after forced crash |0.357s|0.959s|2.500s|
| Fresh disk after destroy/create |0.455s|1.194s|2.422s|

Each row is n=1. Download/export/ext4 preparation/kernel acquisition/Swift compile and disk cloning are excluded. Readiness is guest-agent/HTTP/fixture-telemetry readiness, **not** first decoded human frame, input latency, memory snapshot restore or a factory-cold boot. No aggregate percentile or causal speed ranking is justified by these three different cases.

Builder’s final 21-check run is supporting evidence, separately labeled in `builder-final-review.json`: fresh VZ fixture 2.451 s (n=1), persistent-disk warm restarts 2.458–2.507 s (n=3); network-disabled Docker fixture 2.083 s (n=1), warm 1.933–2.017 s (n=3). Docker was probed by `docker exec` into internal loopback, including exec/poll overhead; no external Docker viewer/input timing was measured. VZ uses host UDS/HTTP polling. Docker has a 2-CPU quota inside its existing shared VM; VZ has 2 configured virtual processors per guest. Different probes, kernels, caches and resource semantics preclude a fair speed ranking. Total host memory/CPU overhead and sustained load were not independently measured.

## Preserved failures and remaining limits

The builder's original partial-disk/rootfs panic, rebind failure and unsynced-write loss remain preserved. Independent static review identified the missing SO_REUSEADDR during crash recovery; current runner sets that socket option only, not SO_REUSEPORT, and publishes the compiled/signed binary atomically. Independent crash/restart passed after the repair. No OS network setting changed.

Guest Chromium uses `--no-sandbox`, and the private PID1 agent is guest root. VM isolation is an additional boundary; it is not a proof of hardened browser or multi-tenant isolation. Exec output is clipped to 1 MiB **after** `communicate` has buffered it; maximum 30 s timeout and response clipping do not bound transient buffering or concurrent workers. Kernel comes from the official Kata 3.17.0 release asset, used only as a boot asset. Local archive/kernel hashes are recorded, but no separately trusted maintainer signature/digest was verified. These are documented prototype limits, not new host-side privileges or a host shell fallback.

No unresolved execution/approval blocker remains for the demonstrated offline noVNC desktop. General guest internet browsing and Neko media need a future networking design. Fast shortcut reliability remains unresolved. The alternate-session URL finding was fixed and passed the targeted revision check below; the original failure remains preserved.

## Reproduce and inspect

Use only a coordinated exclusive additive window. The viewer script sends bounded synthetic input to A; the boundary script refuses an existing `qa-b` or occupied 7904/7794, creates only `qa-b`, and cleans it while preserving A. It explicitly syncs the marker before forced power-off.

```sh
node artifacts/independent-qa/microvm/independent-viewer.mjs --active
python3 artifacts/independent-qa/microvm/independent-boundary.py --active
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py endpoints a
```

Scoped executor loopback/Docker/Virtualization.framework access is required. No model secrets, user browser profile, native Mac input, cloud service or new runtime install is used. Optional stop of the additive demo only: `python3 pocs/microvm/microvm.py stop a`. Destroy only a named stopped disposable session; no broad cleanup command.

Evidence files:

- [Viewer6-check report](../artifacts/independent-qa/microvm/viewer.json), [actual decoded viewer](../artifacts/independent-qa/microvm/viewer-capture-agreed.png), [native terminal screenshot](../artifacts/independent-qa/microvm/native-terminal.png), [fresh PNG](../artifacts/independent-qa/microvm/capture.png), [fixture events](../artifacts/independent-qa/microvm/fixture-events.json).
- [Boundary/lifecycle12-check report](../artifacts/independent-qa/microvm/boundary.json), [final passive state/source hashes](../artifacts/independent-qa/microvm/final-state.json), [host static evidence](../artifacts/independent-qa/microvm/host-static.json), [initial static review](../artifacts/independent-qa/microvm/static-review-initial.json).
- [Retained fast-shortcut FAIL](../artifacts/independent-qa/microvm/viewer-attempt2-fast-shortcut/viewer.json), [its fixture events](../artifacts/independent-qa/microvm/viewer-attempt2-fast-shortcut/viewer-attempt2-fixture.json), [QA parser failure](../artifacts/independent-qa/microvm/viewer-attempt1-json-parser/viewer.json).
- [Builder final review](../artifacts/independent-qa/microvm/builder-final-review.json), [preserved rebind failure](../artifacts/independent-qa/microvm/builder-crash-recovery-first-failure.json), [preserved unsynced loss](../artifacts/independent-qa/microvm/builder-unsynced-crash-durability-first-failure.json).

The final passive listener command returned lsof exit 1 because the selected QA ports correctly had no listeners, while its output contained the two expected A loopback listeners. The initial QA exit-code-only assertion was corrected by inspecting actual output; the raw diagnostic remains saved. This was not a service failure.


## Targeted endpoint-discovery correction

Independent check completed **2026-10-26 05:39:46 EDT** (2026-10-26T09:39:46.199831+00:00). The builder made a narrow source change: the runner passes assigned viewer/API ports on the guest kernel command line; guest-init validates numeric unprivileged ports and sets environment values; the copied adapter derives viewerUrl and its assigned local Origin ports from those values. The builder refreshed the cached rootfs/runner and recreated A; the old A state and all failure artifacts were retained. Accepted noVNC/Neko/workbench implementation and images were unchanged.

| Targeted check | Result | Evidence |
|---|---|---|
| A discovery/own Origin/readiness | PASS | Manifest and capabilities both http://127.0.0.1:7903/; own Origin 200, untrusted Origin 403; HTTP health ready. |
| Fresh qa-b discovery/own Origin/readiness | PASS | Manifest and capabilities both http://127.0.0.1:7904/; own Origin 200, untrusted Origin 403; HTTP health ready. |
| Discovered viewer endpoints | PASS | Both fresh, task-owned headless RFB connections decoded 1280×720 nonzero frames, no lease held. No input or screenshots. |
| Cleanup and remaining demo | PASS | qa-b stopped/destroyed, 7904/7794 closed. A boot `afa49c8a-80fe-4ee5-9771-b032da5d5c12` unchanged through QA; owner `null`; all X11 keycodes/buttons empty. Actual remaining listeners 127.0.0.1:7903/7793 only. |
| Accepted POCs/workbench protection | PASS | Container IDs/images/start times/ports unchanged; accepted backend APIs healthy. |

The current A fixture marker is `SK-mvp2b7bg`; live viewer remains http://127.0.0.1:7903/. QA's fresh second guest boot `6d22c07a-80a1-432b-9a35-63e981df90d5` was removed after testing. This addendum resolves the advisory discovery URL issue only. Fast immediate Ctrl+A behavior, unsynced power-loss durability limits, unsupported networking/Neko UDP, and other original limitations are unchanged.

[Targeted raw report and revised source hashes](../artifacts/independent-qa/microvm/targeted-discovery.json). Exact targeted reproduction, only after an exclusive builder/user handoff:

```sh
python3 artifacts/independent-qa/microvm/targeted-discovery.py --active
```

The helper creates only `qa-b` on 7904/7794, opens discovered viewers read-only, and cleans its guest. It does not send keyboard/pointer input, acquire control, save screenshots, produce timing statistics or run the full acceptance suite. The selected native-terminal PNG remains a local **prior-revision** artifact and was not regenerated or uploaded.
