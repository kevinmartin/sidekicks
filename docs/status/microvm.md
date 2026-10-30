# Additive microVM POC status

**Current: ready for parent review with documented limits. Builder21/21 PASS; independent6 viewer +12 boundary/lifecycle +5 targeted-discovery PASS. All live windows released.** SessionA is a real noVNC desktop at7903, adapter7793. README and separate review report are complete under pocs/microvm/. No main-workbench integration was made.

## Prerequisites confirmed

- Actual host: macOS **15.3**, build **24D60**, ARM64 Apple M1 Max.
- Apple `container` CLI: not on PATH and absent from `/usr/local/bin/container` and `/opt/homebrew/bin/container`.
- Apple's current CLI supports macOS 26; current Containerization source requires macOS 26 / Xcode 26. This Mac is below that supported prerequisite. No unsupported CLI install or OS upgrade is being attempted. This is a **prerequisite blocker**, not an approval denial or pending approval.
- Direct **Virtualization.framework** probe succeeds on this Mac (`supported:true`), independently rechecked during takeover. It can support a first-level Linux VM without nested KVM.
- No nested Kata, cloud provisioning, privileged container, host mount, network-security setting change or persistent credential grant is planned.

Primary references: [Apple container requirements](https://github.com/apple/container#requirements), [Containerization requirements/design](https://github.com/apple/containerization#requirements). The direct-framework fallback is distinct from Apple container CLI and from a Kata runtime.

## Interrupted work recovered

The prior worker failed twice due to model capacity, but created substantial work under `pocs/microvm/`. Its first successful VM evidence includes two distinct Linux boot IDs, 2 vCPUs / 2 GiB each, separate ext4 disks, no NIC/shared directory, and a decoded noVNC viewer over a loopback TCP → virtio-vsock bridge. It exports the accepted ARM64 noVNC image without modifying the accepted image/container, using a Kata-distributed Linux kernel as a boot asset only.

A previous verification process was still running when takeover inspected the host. Its exact process/lifecycle state is being checked before resuming; no duplicate verifier was launched. Earlier boot failures and screenshots remain preserved. Existing workbench/noVNC/Neko are not being modified.

## Reserved additive endpoints

| Session | Viewer | Adapter |
|---|---|---|
| A | http://127.0.0.1:7903/ | http://127.0.0.1:7793 |
| B (isolation test) | http://127.0.0.1:7904/ | http://127.0.0.1:7794 |
| Disposable Docker comparison only | http://127.0.0.1:7905/ | http://127.0.0.1:7795 |

Current bridge is TCP-only and has no guest NIC or network egress. **Neko UDP/WebRTC, Apple CLI, Kata runtime, snapshots and GPU remain unsupported/not tested on this fallback.** Successful noVNC display must not be presented as Neko media success.

## Takeover checkpoint

Commands actually executed during takeover: inspected the failed worker turns and existing source/evidence; verified `sw_vers` 15.3/24D60, ARM64, absent Apple CLI, Xcode14.2/Swift5.7.2; ran the existing signed Virtualization.framework probe (`supported:true`); inspected exact task PIDs/listeners; preserved the partial rootfs panic log; stopped only the task-owned panicked VM A; repaired startup timeout/log handling and atomic disk preparation/clone checks; rebuilt only the task-local Swift runner; preserved the 207MiB truncated session; created and booted a fresh A from the completed3GiB image.

Execution is **not blocked by model capacity**. Apple container CLI remains blocked by the unsupported macOS prerequisite. Direct first-level Virtualization.framework fallback is progressing; full isolation/lifecycle/cleanup verification is still pending. Main workbench and accepted noVNC/Neko implementation/runtime were not modified.

## Builder complete and QA handoff

Final own run records21PASS: two live distinct kernel boot IDs/disks;2CPU/2GiB; nonroot exec/exit status; files/cross-session/path boundary; real RFB decode/guest keyboard/capture; actual xterm; explicit pre-crash sync, crash containment/new boot/synced persistence; reset; shared Docker-kernel comparison/internal capture; accepted-demo identities/health unchanged; loopback-only host listeners. Startup numbers and full exact21 list are in README/report, with cached conditions and different probe paths explicit.

A was handed over as fresh fixture, ownernull, no held keys, no viewers;7904/7794 were free for QA-owned qa-b. Builder performs documentation/artifact work only while QA owns input/lifecycle. Independent fast-shortcut behavior is under investigation, not declared accepted. Apple CLI remains prerequisite-blocked; NekoUDP unsupported on this fallback. Failed rebind and unsynced-write results are preserved.

## Review artifacts prepared

Separate user-facing report: pocs/microvm/sidekicks-microvm-report.md, with exact21 builder check names, official source URLs, cached/probe timing boundaries, safe start/stop/cleanup and all failures. Independent core results are6 viewer +12 boundary/lifecycle PASS, with fast immediate shortcut and alternate-session capability URL findings retained. Parent review/independent final report is still pending; no final acceptance claim is made. One actual QA native-terminal screenshot has been visually inspected by integration and will be saved with the separate report to new Library identities, leaving original POC Library files untouched.

## Parent-requested metadata follow-up

After QA release, fixed per-session endpoint discovery in source: runner boot parameters -> guest numeric environment -> copied adapter viewer URL and allowed Origin ports. Cached template refresh cost60.092s, not a new runtime performance sample; disposable A was recreated with bootafa49c8a-80fe-4ee5-9771-b032da5d5c12 and healthy fresh fixture. Accepted POCs/workbench untouched. QA now owns targeted A/qa-b discovery/read-only RFB/cleanup checks only. Original metadata FAIL and pre-fix report hash are preserved. Fast immediate shortcut failure remains visible, cause not established.

Parent corrected the artifact scope: only the written report will be uploaded to a separate new Library identity. No screenshot has been uploaded; native-terminal.png stays local and has been visually inspected. Original POC Library identities remain untouched.

## Targeted metadata revision complete; frozen

Independent targeted5PASS confirms correct per-session viewer URL against manifest, own/untrusted Origin behavior, fresh read-only RFB decoding at A and qa-b discovered URLs, qa-b cleanup and unchanged accepted POCs. A is unowned with no actual held X11 input and only loopback listeners. Original metadataFAIL/raw report and pre-revision state remain preserved. Fast immediate shortcut failure remains known; paced PASS does not establish its cause.

All live work is released/frozen. Only written-report Library transfer and metadata/links remain. No screenshot upload occurred or is planned; actual native-terminal image remains repo-local and has been visually inspected. Parent must inspect report/local image before declaring completion. No OS upgrade, Apple CLI install, nested Kata, Neko UDP workaround or host network/security change was performed.

## Written report saved for parent review; no image upload

Separate Library report created successfully: libfile_a8d31b9037f48191b7553f5bda4a35c8, version0, file_00000000dc0c8230807cca41f88d43f6, named sidekicks-microvm-report.md. Local Library identity/version persistence succeeded. The report includes a concise review summary, exact21 builder check list and the complete frozen independent QA report/addendum (SHA f8d7bfd26e0f273fff83af7117eb30917bebf3a4356a7633b0eefc8a8785ae25). Original POC Library identities were not changed.

No screenshot was uploaded. Actual guest/native-xterm image remains /Users/kevinmartin/Development/kevinmartin/sidekicks/artifacts/independent-qa/microvm/native-terminal.png; integration and QA personally viewed exact command output in a real guest desktop. Hash4922886ac6ca586556b9d3798d6da8f91a493446fc4a2a8790df0ebb2055cf7b. Transfer details are in pocs/microvm/artifacts/library-deliverables.json. All live work is frozen/released; parent should inspect the written report/local image before accepting completion.

## Additive Neko UDP variant ready for independent QA

Separate session `neko` is live at viewer http://127.0.0.1:7906/ and API http://127.0.0.1:7796, media UDP127.0.0.1:59120–59139. It has its own4GiB disk,2vCPU/2GiB and kernel, no NIC/host shares/egress. Accepted A/noVNC and Docker demos remain unchanged. Framed reliable vsock streams carry loopback UDP datagrams, one stream/connected guest UDP socket per learned host peer and media destination;64peers/256KiB buffer per direction/60s idle. No host network/firewall/security settings changed.

An isolated pinned Neko3.2.0 binary enables Pion loopback candidates; source/provenance is in pocs/microvm/neko-source. Actual headless Chrome selected ICEUDP127.0.0.1:59122 and decoded1280x720;9E2E checksPASS including both native data-channel and gatedHTTP inputs, takeover, rejects and capture.8transport/lifecycle checksPASS include datagrams/peer isolation, private destination denial, all20UDP bindings loopback, stop closes ports, new kernel restart and unchanged accepted identities.2fresh postrestart stream/capture checksPASS; frames4→64 in~2s, UDP59137.4unit checksPASS. Independent QA pending, exclusive handoff next.

Preserved oversized test failed with macOS EMSGSIZE. Read-only sysctl shows UDPmaxdgram9216;9216 sendsPASS,9217/65507FAIL; no setting change. Actual VM datagrams empty/binary/8192bytesPASS. This is bounded UDP-at-edges via reliable-vsock, not native VM UDP or proof of parity; head-of-line/loss/congestion limits explicit. Raw decoded-stream stats include frame drop counters and are retained without causal/performance claims. No image Library upload. Evidence: pocs/microvm/artifacts/neko-udp and pocs/microvm/neko/README.md.

## Neko independent closeout complete; written Library version1 saved

Independent Neko preflight, initialmedia4, functional9 and lifecycle5 PASS. Lifecycle includes5 postrestart media/capture subchecks, not a second full-suite count. Final09:05:40EDT state: ownernull/all actual X11 keycodes/buttons empty; new Neko boote93d0d8b-9021-41ea-aa93-7cf5299d5e59; protected A and accepted Docker identities/images/start/ports unchanged; services7790/7791/7792/7793/7796 HTTP200. The guest remains live at7906/7796 with UDPloopback59120–59139. All implementation/runtime/test work is frozen/released.

Takeover establishes controller injection cutoff:315→315 afterACK; fixture text snapshot lengths27→38 separated by650ms observation wait are preserved. This does not distinguish application processing from telemetry settling or establish delivery latency; no generic application-queue-flush guarantee. Reliable framed vsock transport/HOL limits and host9216byte UDP limit remain explicit.

Frozen QA report artifacts/independent-qa/microvm/neko-udp/report.md SHA2563414735c38a4ce3b2b7414e9cd668c1eac20708007115e7af005304a20483b72 was appended verbatim to the existing combined written report, preserving original noVNC history. Library replacement succeeded: same libfile_a8d31b9037f48191b7553f5bda4a35c8, now version1, file_00000000626481fdabd2de7966a8b015; local version persistence succeeded. No screenshot upload; all images stay local. Parent should inspect report/local image before final acceptance. No approval/execution blocker remains for this bounded variant; Apple CLI prerequisites and native UDP parity remain outside its tested scope.

## NAT profile functional; comparison pending

Separate session nat/profile neko-nat is live at viewer7907/API7797. Apple basic VZNAT start succeeded with existing virtualization entitlement and no new OS prompt. Guest udev/networkd provides DHCP/routes/DNS; assigned address is reported by private vsock info, currently192.168.64.4. Actual WebRTC selected UDP to192.168.64.4:59231 and decoded1280x720, bypassing relay.9builderE2E and5lifecycle checksPASS; public DNS/TLSHTTPS200 to example.com/www.iana.org. Protected A/relay VM boots and accepted containers unchanged. New profile is egress-enabled, not egress-isolated; no host firewall/routes/VPN/security settings edited and no private services scanned.

First guest DHCP timeout/PID1 panic preserved; fixed guest-only udev initialization, repeatable IPv4 config and setup-error reporting. Rootfs/binary/codec/resolution/resources match relay apart from required networking/init/config differences. No fresh comparative performance claim yet. Independent QA/methodology preparation requested; attempted readiness message returned Transport closed (delivery uncertain), checking status before retry. Source/runtime builder work paused for QA coordination.

Coordination connection recovered: supported shell read at2026-10-26T14:23:52Z succeeded, no login/unlock required; task-message delivery succeeded afterward. NAT builder remains IDLE/docs-only. Independent protocol at artifacts/independent-qa/microvm/nat/protocol.md reviewed: fresh native-input control/load windows, same software/codec/resources, exact counter ROI, raw outcomes, separate media/RSS/CPU attribution. Harness source is being prepared; no live QA handoff/measurement yet.

## NAT and matched comparison complete; Library version2 saved

Independent NAT functional9, lifecycle5 and final-state9 checksPASS. Public DNS/TLSHTTPS and actual Firefox public page rendered; direct ICEUDP to assigned guestIP decoded1280×720. Four fixed-order control/load windows completed120/120 valid samples. Builder recomputed standard median/p95/max, CPU deltas, interval jitter-buffer ratios and all counter/hash/poll checks from unchanged raws. Control median/p95 ms:relay92.00/94.60 vsNAT89.55/127.00. Load:relay141.00/271.80 vsNAT116.85/212.20. Typical polling16.6ms. Mixed control tails, short fixed order, differing VM ages/caches and incomplete first-block Chrome process total prevent causal or efficiency claims. Native freeze/loss deltas0 and no observer gaps≥250ms occurred only in short passive windows.

The helper restore failure `/fixture/` remains preserved; separate restart verified correct fixture recovery. NAT strict stable-text-atACK diagnostic remainsFAIL, while controller injection cutoffPASS (313→313, fixture snapshots33→37 after650ms wait; no generic queue flush). Initial DHCP/PID1 failure and all earlier reports/raws preserved.

Final QA leaves NAT/relay live, unowned and all actual keycodes/buttons empty; exact load workers and owned Chrome gone; comparison lock removed; server listenersloopback and all protected boots/container IDs/images/start/ports unchanged across measurements. No more runtime work planned. No host firewall/routing/VPN/security/persistent credential changes; NAT is explicitly egress-enabled with possible host/LAN reachability, not isolation.

Frozen independent NAT report SHA2569c25ad5b55950b838d4fc0749711492c191d0fb5b71a9362c14a39fb35faf7a1 appended verbatim to same combined written microVM Library item. Replacement succeeded: libfile_a8d31b9037f48191b7553f5bda4a35c8, version2, file_000000002aa881f5a6bb215bfcf0154e; local version persistence succeeded. Screenshots stay local, personally inspected, not uploaded. Details pocs/microvm/artifacts/library-deliverables.json. Source/runtime frozen for parent review; no current functional or approval blocker in tested scope.

## Shared desktop UX revision in progress

User requested cloud-like visual/behavioral UX across all variants. Shared original cyan wallpaper/icons/Home assets now in shared/desktop; Openbox/tint2+picom/PCManFM retained by builders, no heavy desktop/runtime swap. Current Docker builders test actual native WM/cursor/close/reopen/fullscreen behavior; oldimages/containers/evidence preserved. VM live disks still unchanged. New templates will use fixture-independent /desktop/state readiness and host-forwarded HTTP source127.0.0.2, keeping internal /home launch capability inaccessible via forged host headers. Workbench source has cyan palette and direct links for all five runtimes; not deployed yet. Existing performance records remain historical pre-UX, no new performance claims. No screenshots uploaded.

## Shared desktop UX deployed; independent acceptance underway

Integration applied final Docker noVNC01acf649... and Neko1aca12... desktop images to fresh VM templates, with the existing pinned loopback Neko binary retained for VM media. Preparation and both ext4 checks passed; one BuildKit image-reference failure is preserved separately. Old templates/runners are in .build/pre-cloud-ux and normally stopped old disks in .state/{a,neko,nat}-pre-ux. Current sessions retain2vCPU/2GiB and existing networking, no host mount/security/network changes.

All three /desktop/state replies are ready. Initial integration boots: a7423b922-4a37-4b12-a383-a6b24e93591f, neko1c27019e-3bdb-4d04-874b-ba22a314fc5b, nat25c28f28-5c13-46b2-9eea-27b99ee9fe6d; normal QA restarts may change them. A and relay remain no-NIC; NAT DHCP192.168.64.4 is egress-enabled. Read-only fresh host viewers decoded actual RFB A and increasing selected-UDP WebRTC relay/NAT streams; current workbench renders five direct links and historical-evidence labels. Smoke4PASS does not establish native controls or restart acceptance.

Initial local screenshots for all three VM desktops were personally inspected: real normal browser window/chrome/margins, shared cyan Home/wallpaper and floating dock visible. Screenshots remain local except the ONE Docker Neko screenshot expressly requested by Kevin (Librarylibfile_65d0def3be588191b4121a4a25f88068, timestamped12:53:27EDT preview). Current workbench imagec80c522... containerc1a4a1c2...; old container sidekicks-workbench-preserved-20261026T170425 stopped. Protected Docker noVNC/Neko IDs/images/starts/ports unchanged during integration deployment.

Independent QA owns all sequential interaction/boundary/restart windows now; integration stays docs-only. No current approval blocker. New UX results remain pending until docs/ux-verification.md is frozen; old latency reports stay pre-UX and raw evidence unchanged. Exact integration evidence pocs/microvm/artifacts/ux/{preparation,deployment-state,stream-smoke}.json; lifecycle commands pocs/microvm/ux/README.md.

## Final shared UX and live-content drag closeout

Independent scoped core acceptance completed on all five desktops/eight input paths:95 launcher-boundary PASS,27 desktop restart subchecks (Docker6each; VM5each),8workbench PASS,13finalaudit PASS. All historical13 report/raw hashes unchanged. Original UX report is frozen SHAfd7ff6a83336a007e66cd602b544a028000684248480040a6f6d2f99f1b66495; actual physicalMac reservedkeys/gestures andreal tabblur remainUNTESTED, nativeNekoAltTab SDKEMULATED. No post-refresh performance rerun or generic applicationqueueflush claim.

The later user-observed noVNC outline was independently reproduced. Both current noVNC targets now add only -nowireframe; image0bdd1d3baf9e27ab6e396cce1c7ab9353a441e5711ba7c0b8547dfa3c89698d7, entrypoint803129717c7467d54ddbdc1a8ccf17ddceef3e5d1523f37f6c2894386045ae8a. Focused held-drag content checks passed before and after normalrestart on Docker andVMA with zero outline matches.27/29tight intermediate matches; partial/asynchronous frames retained, no atomic/latency/independentresize claim. Focused report SHA28a0b1a4f87677be60e4d31f76e2d7ea88d214fd69ea31538ebf64be55a014bd. PreviousA state .state/a-pre-drag andtemplate .build/pre-live-drag preserved; Neko/workbench/runner/kernel unchanged. FinalA QAboot51e94113-7937-497f-93c0-4bd079c96962. All input/restart windows released clean.

Guarded Library replacements confirmed: written report SAME libfile_a8d31b9037f48191b7553f5bda4a35c8 nowversion3,file_000000006b6881f5a003c4011a154fe0; requested screenshot SAME libfile_65d0def3be588191b4121a4a25f88068 nowversion1,file_000000004af881fd9b933e5d33b20661. Both local metadata helpers exit0. Earlier report/version history and originalPOC identities retained. Screenshot is finalDockerNeko image saved13:38:11EDT, personallyinspected; no newimageidentity or otherQAimageupload. Fullreport local pocs/microvm/sidekicks-microvm-report.md. Parent may read current Library report before consolidatedusercompletion. No further tests/builds/runtime actions planned.
