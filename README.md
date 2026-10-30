# Sidekicks · Computer Lab

A local workbench for **noVNC** and **Neko** computer-use POCs across Docker and Apple Virtualization.framework guests. Compare actual human streams, automation input, takeover arbitration, bounded screenshots and desktop behavior. Model-backed bots and a production harness remain future work.

**Desktop polish complete (Oct28):** All five desktops now run the independently verified Xfce/GTK3 refresh with Thunar and VTE terminal; noVNC uses Chromium embedded controls and Neko retains Firefox. Production interaction, logout Cancel, one normal restart per desktop, native Neko input and all five existing LAN viewers passed bounded acceptance (35 new +11 referenced PASS groups). The final passive audit passed and disposable stages are stopped. See [current rollout status](docs/status/desktop-polish.md) and [desktop refresh report](docs/desktop-polish.md). Chromium's postrestart restore prompt remains documented; mobile and performance retesting are deferred. Historical verification below retains its original scope.

Open the workbench at **http://127.0.0.1:7790**. It polls live adapters and labels their reported capabilities. Inspect the POC READMEs and independent QA evidence before interpreting a feature as verified.

**Temporary LAN access:** Kevin authorized unauthenticated wildcard viewer exposure for this POC. The five phone-facing viewers are listed at **http://192.168.0.97:17901/lan/index**; see [LAN setup, mobile controls and rollback](pocs/lan/README.md). Existing loopback viewers/APIs are unchanged. `bash scripts/lan.sh stop` removes only the new LAN listeners. Mac-side actual stream/input checks passed; physical iPhone/Safari and inbound Wi-Fi connectivity remain untested.

## Desktop layout and earlier UX acceptance

All five live desktops use the shared cyan wallpaper, light Sidekicks Home, floating Browser/Terminal/Files dock and normal browser windows. The earlier Openbox desktop's completed scoped acceptance is preserved in [UX verification](docs/ux-verification.md): all five desktops/eight input paths,95 boundary checks,27 restart subchecks,8 workbench checks and13 final-audit checks. The new Xfce/GTK3 revision has separate [desktop-polish verification](docs/desktop-polish-verification.md); its current phase is stated above. Native Neko Alt+Tab uses SDK emulation; physical Mac reserved keys/gestures and real headless tab-blur behavior remain untested. The timestamped Neko preview screenshot is a single-variant preview, not acceptance of every variant.

| Desktop | Viewer | API | Network |
|---|---|---|---|
| noVNC Docker | http://127.0.0.1:7901/ | 7791 | Disposable Docker bridge |
| Neko Docker | http://127.0.0.1:7902/ | 7792 | Disposable Docker bridge; UDP59020–59039 |
| noVNC VM | http://127.0.0.1:7903/ | 7793 | No NIC; TCP over vsock |
| Neko relay VM | http://127.0.0.1:7906/ | 7796 | No NIC; framed reliable-vsock UDP59120–59139 |
| Neko NAT VM | http://127.0.0.1:7907/ | 7797 | Egress-enabled NAT; direct guest-IP UDP59220–59239 |

**Live-content dragging:** both current noVNC builds now disable x11vnc wireframing. Focused independent held-drag checks passed before and after normal restart on Docker and VM A; no outline samples remained. This follow-up changes one server startup flag, with original images/state retained. See [focused evidence](artifacts/independent-qa/ux-live-drag/report.md).

Open a viewer and take human control there. Native titlebar buttons minimize, maximize and close apps; the dock restores an existing minimized window or reopens a closed app. Browser F11 and the viewer's fullscreen control are separate. `/home` is the default guest page; deliberately open `/fixture` for synthetic input diagnostics. Home's launch controls are restricted to the internal guest page, fixed apps and active ownership. Chromium and Firefox retain their own browser chrome.

See [VM desktop preparation/lifecycle](pocs/microvm/ux/README.md). Old images, stopped containers, guest disks and verification reports are preserved. Historical performance below predates the windowed desktop/compositor revision and is not a current-UX benchmark.

## Historical comparison (before desktop UX revision)

Independent final runs used the same task-owned host Chrome viewer, fresh copies of the shared synthetic fixture, and native ARM64 desktops at 1280×720 with 2 CPUs / 2 GiB each. Execution was sequential with builders idle; each route has 30 successful samples and zero misses. The guest applications differ: noVNC uses Chromium 156.0.8078.47 with a 1280×595 page viewport; Neko uses Firefox ESR 153.5.1 in kiosk mode at 1280×720. These are whole-POC route observations, not an isolated protocol benchmark.

| Input → first decoded counter change | noVNC RFB (text preset) | Neko native SDK / WebRTC |
|---|---:|---:|
| Median (middle-pair average) | 98.05 ms | 62.05 ms |
| p95 | 115.8 ms | 96.2 ms |
| Maximum | 117.7 ms | 96.9 ms |
| Typical observation interval | 16.4 ms | 16.6 ms |

The clock is the viewer browser's `performance.now()`, from mousedown to a decoded change in the standard synchronous click counter. Medians average sorted samples 15 and 16 (one-based) for each n=30 run; [correction provenance](artifacts/independent-qa/statistics-correction.json) preserves the raw-file hashes and method; approximate medians are 98 ms and 62 ms. These are local synthetic samples; they do not establish physical display latency, WAN performance, image-quality equivalence or Neko gated-HTTP route latency. Backend encoding paths differ. Earlier 98.3 ms noVNC results remain preserved as provisional.

Independent active-contract checks: **16/16 per backend**; workbench **7/7**. Both stacks have real decoded-stream, native-terminal takeover, held-input release, restart/theme and fresh bounded image evidence. Neko also decoded a synthetic 250 ms / 440 Hz tone (peak PCM RMS 0.04253) with speaker output suppressed; physical listening is untested. noVNC has no audio channel in this configuration.

Takeover stops future bot injection and queued commands. Already-injected events may settle in an application after acknowledgment: original noVNC 516 ms and a Neko 36 ms strict diagnostic are retained. No generic application-queue flush is promised. The final noVNC paced/settling regression observed no later bot text; Neko's terminal controller-send count stayed unchanged after handoff.

See [independent verification](docs/verification.md), [current integration evidence](docs/status/integration.md), and each POC README for exact tested routes, preserved failed attempts and untested variants. The workbench displays a **recorded verification snapshot** separately from live API readiness. Refresh that snapshot with `bash scripts/workbench.sh rebuild` after changing status evidence.

## Review artifacts

The [independent report](docs/verification.md), [final workbench screenshot](artifacts/independent-qa/workbench.png), [noVNC screenshot](artifacts/independent-qa/novnc-viewer-measure-changed.png) and [Neko screenshot](artifacts/independent-qa/neko-native-measure-changed.png) are saved locally and in Library. The final workbench Library screenshot is version 2 and the corrected report is version 1. Raw [noVNC](artifacts/independent-qa/novnc-viewer-measure.json) and [Neko native](artifacts/independent-qa/neko-native-measure.json) measurements are also attached there. Exact Library IDs, versions and transfer outcomes are in [library-deliverables.json](artifacts/independent-qa/library-deliverables.json).

Those screenshots and passive checks describe the original pre-UX revision. Current desktop acceptance is recorded separately in [UX verification](docs/ux-verification.md); historical raw files are retained unchanged.

## Run

Prerequisites: existing Docker Desktop (running), Go ≥1.23, Node (syntax/QA tooling), Python 3 for POC helpers. On the current Mac this is native ARM64 Docker Desktop; see [runtime details](docs/status/runtime.md). No model credentials are needed.

```bash
bash scripts/start.sh           # workbench, noVNC, Neko
bash scripts/start.sh workbench # workbench only
bash scripts/start.sh novnc     # one POC
bash scripts/start.sh neko
bash scripts/status.sh
bash scripts/stop.sh            # Docker demos + temporary keep-awake job only
bash scripts/workbench.sh test  # race-enabled Go boundary tests + JS syntax
```

The shared scripts manage the Docker demos. The VM desktops have separate preserved sessions; inspect them with `python3 pocs/microvm/microvm.py status SESSION`. Start a stopped current session with `start a`, `start neko`, or `start nat`. To stop only those three VMs:

```bash
python3 pocs/microvm/microvm.py stop nat
python3 pocs/microvm/microvm.py stop neko
python3 pocs/microvm/microvm.py stop a
```

Docker/socket access and local listening can require executor approval in restricted coding sessions. This is distinct from runtime failure. The ordinary terminal commands above do not change Docker permissions. For a foreground host-only fallback (`bash scripts/workbench.sh start-host`), stop the Docker workbench first. A managed executor may reap detached host children; Docker is the durable default:

```bash
GOCACHE="$PWD/.run/go-cache" GOPATH="$PWD/.run/go" go build -o .run/sidekicks-workbench ./harness/workbench
./.run/sidekicks-workbench -root "$PWD"
```

The workbench runs as `sidekicks-workbench` with Docker restart policy `unless-stopped`, 0.5 CPU / 128 MiB, a read-only filesystem, non-root user and no host mounts. It survives coding terminal exit while Docker Desktop is running. `docker logs sidekicks-workbench` shows its logs. Local build cache lives in `.run/`; POC-specific logs and exact cleanup commands live in their READMEs. No persistent system daemon is installed. Existing unrelated Docker resources are left alone. Stop commands do not prune Docker or touch other projects.

For this overnight run, `bash scripts/keep-awake.sh start` creates a temporary user launch job running `caffeinate -i -t 28800` for eight hours. It prevents idle sleep without changing system settings or installing a persistent plist. Closing the lid, explicit sleep, logout, shutdown or loss of Docker Desktop can still stop access. Inspect with `bash scripts/keep-awake.sh status`; `scripts/stop.sh` removes it.

## Demo route

1. Open the workbench and check each adapter's live readiness and recorded capability status.
2. Use **Human viewer** to acquire a human lease and open that stack's viewer. The POC must gate viewer input at the final dispatch point. Neko also exposes a separate **Native diagnostic** link: connect there, then use its own takeover button; bot input is disabled during that native lease.
3. Use Home's Input Lab link to deliberately open `/fixture`, then type, click/right/double click, drag/drop, scroll and try modifiers. Return to `/home` afterward. The visible event log and counters provide proof of delivery.
4. Release control before acquiring an agent lease in the control desk. Try normalized input, then **Capture fresh image**. An HTTP capture duration is labeled as such and is separate from video latency.
5. Use **Stop & release inputs** to cancel control. The adapter must reject stale tokens after human takeover or expiry. Takeover cancels future bot injection and releases held inputs; applications may still process previously injected events. Ready means input ownership, not a generic application queue flush. Default takeover works in arbitrary native windows without a fixture acknowledgment.

The shared fixture is also available at http://127.0.0.1:7790/fixture/ and [shared/desktop-fixture.html](shared/desktop-fixture.html). Local fixture behavior alone does not prove a remote POC input route.

## Project map

| Path | Purpose |
|---|---|
| [harness/workbench](harness/workbench) | Go server, operator UI, TypeScript wire contracts and tests |
| [pocs/novnc](pocs/novnc) | noVNC builder-owned baseline, scripts and evidence |
| [pocs/neko](pocs/neko) | Neko guest/viewer, scripts and evidence |
| [pocs/microvm](pocs/microvm) | Direct Virtualization.framework noVNC, relay and NAT profiles |
| [shared/desktop](shared/desktop) | Original shared wallpaper, app icons and Home |
| [docs/status/contracts.md](docs/status/contracts.md) | ports, ownership and adapter route contract |
| [docs/architecture/overview.md](docs/architecture/overview.md) | boundaries, safety lifecycle and measurement rules |
| [docs/status/integration.md](docs/status/integration.md) | executed integration tests and limitations |
| [docs/status/qa.md](docs/status/qa.md) | independent verifier progress |
| [docs/verification.md](docs/verification.md) | independent acceptance results, when available |
| artifacts/ | local test evidence; not credentials |

## Scope and limits

Original viewers and host shell/admin APIs bind to127.0.0.1; Kevin's separately authorized temporary LAN bridge exposes only the documented viewer/media routes on wildcard listeners. Desktop containers must have no privileged mode, home mount, raw host VNC exposure, or broad host access. Images use synthetic content only. Each POC begins at2 CPUs /2 GiB with a512MiB shared-memory allocation where needed.

Native Mac trackpad pinch/swipe, wheel/keyboard zoom translation, direct X11 automation and human browser input are separate capabilities. Protocol support is not test evidence. Audio generation is distinct from a decoded receiver track or physical listening. Screenshot images are suitable for a future LLM boundary, but this lab does not invoke a model. Neko normalized text currently supports printable ASCII, LF and Tab; other Unicode is rejected with 422 before writing. Native keyboard layout/IME coverage is not exhaustive.

Scout, Builder and Reviewer are Sidekicks sample personas for demo intent records, with no autonomous agents or model execution. Terminal and browser adapter contracts are present; they do not launch host shells or reuse private browser sessions.
