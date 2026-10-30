# Neko Xfce desktop: frozen v7

October 28, 2026. The **exact v7 image is production-accepted** on Docker 7902 and supplies the canonical Neko relay/NAT VM desktops on 7906/7907 with the retained loopback Neko binary. Independent production QA passed **10 checks per Neko target**, including exactly one supported normal restart each, current GTK3 apps, logout Cancel/content, Home, native input/held cleanup and existing LAN viewing. [Root desktop refresh documentation](../../docs/desktop-polish.md) is authoritative for deployment, identities, network access and operation.

| Item | Accepted production value |
| --- | --- |
| Image | `sidekicks-neko:polish-xfce-stage-v7` |
| Exact image ID | `sha256:36e11c585ece469a01f5bf705d5e1357afe767ba8e858eafba548d38786457a1` |
| Architecture | Native `linux/arm64`, software VP8; no GPU device |
| Docker container | `sidekicks-neko` |
| Container ID | `127b2d00b590d3a32f3d1563503a1269558fcaa37df0bace04b26c6134454ae3` |
| Docker connections | Viewer <http://127.0.0.1:7902/>; native `?mode=native`; API <http://127.0.0.1:7792/health>; UDP 59020–59039 |
| Limits | 2 CPUs, 2 GiB RAM, 512 MiB shm, 512 PIDs; no mounts or privilege |
| Home SHA256 | `45899f0abc84afd59fec79aa7d3b149202965f32a1052f87dcad33002b1cc14a` |

[Frozen image/guest-source manifest and historical stage clean-state proof](test/artifacts/xfce-stage/final-freeze-v7/handoff.json), [clean desktop image](test/artifacts/xfce-stage/final-freeze-v7/clean-desktop.jpg). Image contents were frozen before QA; subsequent launcher/docs changes are host files only.

v7 changes only the Firefox Homepage policy enum `home` → `homepage` and installs the corrected JSON into Firefox's actual policy path as well as the source copy. [Mozilla's schema](https://firefox-admin-docs.mozilla.org/reference/policies/homepage/) excludes `home`. Independent v6 QA found the real Alt+Home failure even though startup's explicit URL and the separate Ctrl+T override worked. That failure is preserved; **v7 focused verification passed 7/7**, including actual gated Alt+Home, native new tabs/explicit Linux Home chord, Thunar HTML association and one normal restart. Independent focused v7 acceptance passed **7/7**: [QA report](../../artifacts/independent-qa/desktop-polish/stage-neko-2026-10-28T17-41-34-416Z/result.json), SHA256 `61bd63413f2399cc02c4413c9a118980271c9d65c3f7c757cc9d66b16229558f`. Unaffected v6 evidence is explicitly referenced; no full v7 app-suite rerun is claimed.

## Production acceptance and retained stage

| Neko target | Local viewer | Independent result |
| --- | --- | --- |
| Docker | <http://127.0.0.1:7902/> | **PASS 10/10**, one normal Docker stop + supported pinned start: [report](../../artifacts/independent-qa/desktop-polish/docker-neko-2026-10-28T17-58-54-842Z/result.json) |
| Relay VM, no NIC / vsock | <http://127.0.0.1:7906/> | **PASS 10/10**, one normal VM stop(sync) + start: [report](../../artifacts/independent-qa/desktop-polish/vm-neko-2026-10-28T18-19-00-386Z/result.json) |
| NAT VM, direct guest UDP | <http://127.0.0.1:7907/> | **PASS 10/10**, one normal VM stop(sync) + start: [report](../../artifacts/independent-qa/desktop-polish/vm-nat-2026-10-28T18-20-48-344Z/result.json) |

[Production summary](../../artifacts/independent-qa/desktop-polish/production-summary.json) records the exact reports, hashes, restart methods and identities. The [final passive production audit](../../artifacts/independent-qa/desktop-polish/final-production-state.json), October 28 at 2:31 p.m. EDT (18:31 UTC), passed: all five POCs ready, one Home each, sole Xfce components, no owner and no actual held keys/buttons. Eleven private TCP listeners remained loopback-only; five existing intentional LAN listeners retained their authorized bindings. NAT guest 192.168.64.4 matched the unchanged bridge PID 69880 target. No physical/mobile or performance rerun is inferred from this bounded production acceptance.

The disposable `sidekicks-neko-xfce-stage` container `c42a3c527b80f494a7e2cee5780c57e348dcf4f3e97d311fcd198fb1cb546528` and its proxy were **stopped after explicit release**, preserving container/image/evidence. Former stage 17802 is not a current viewer. [Stage cleanup evidence](../../artifacts/desktop-polish/production/stage-cleanup.json) records unchanged production identities/StartedAt and health 200. The versioned stage records below remain historical; v6's original Home FAIL is preserved.

## Desktop behavior

This replaces the guest desktop stack with a standard **Xfce4-session, Xfwm4, xfdesktop4, xfce4-panel and xfsettingsd** session on the existing Neko Xorg display. Xfwm is the sole window manager/compositor. The fixed unprivileged D-Bus session is shared with adapter-launched GUI apps. Firefox ESR remains the browser, with ordinary titlebar controls and an initial window exposing cyan desktop margins. Startup, Home and actual Ctrl+T lead to Sidekicks Home. Existing HTML/files and HTTP links use that same browser profile.

The dock contains browser, GTK3/VTE terminal and Thunar. Left-click restores the same managed client/PID and geometry without creating extra tabs. Closing an app leaves it closed until explicitly reopened. Thunar has light Adwaita GTK3 styling with original Sidekicks cyan accents/folder artwork and DejaVu fonts. No unlicensed cloud-reference artwork was copied. Files and command proofs remain inside the disposable guest.

The primary terminal is **xfce4-terminal 1.1.4**, using the shared navy palette, 11pt DejaVu Sans Mono, UTF-8 and an 88×24 layout. The managed launcher has class `SidekicksTerminal`; Thunar's supported Open Terminal Here creates the standard `Xfce4-terminal` class and correct working directory. XTerm and UXTerm are no longer installed in this pruned image. Historically, UXTerm was an xterm wrapper selecting UTF-8/resources, rather than another emulator. Their shared Xresources remain as compatibility configuration only.

Named-user Xfconf panel locking removes customization entries for the ordinary guest user. Stock **Log Out, Help and About remain**. About displays the installed version; Help opens the supported online-manual confirmation dialog (online/no-NIC documentation loading is untested). Log Out now opens the supported confirmation dialog; **Cancel preserves open apps and session**. This is menu curation, not a security sandbox. Desktop background settings menus are disabled; useful application context menus remain.

Canonical visual/session templates come from `shared/desktop/polish/xfce/`, owned by the noVNC worker. Neko substitutions are limited to guest paths, fixed helpers, icons/wallpaper and XRandR output `DUMMY0`. Shared GTK assets remain integration-owned.

## Actual verification

| Scope and revision | Result and evidence |
| --- | --- |
| v5 gated browser route | **PASS 14/14**: [report](test/artifacts/xfce-stage/gated-2026-10-28T16-24-50-761Z/report.json) |
| v5 native SDK data-channel route | **PASS 14/14**: [report](test/artifacts/xfce-stage/native-2026-10-28T16-30-02-886Z/report.json) |
| v5 About/Help/logout, session recovery and normal restart | **PASS 5/5** under the prior immediate-logout configuration: [historical report](test/artifacts/xfce-stage/final-lifecycle-2026-10-28T16-31-49-913Z/report.json) |
| v6 supported confirmation + Cancel | **PASS**: identical browser/terminal/Thunar client IDs/PIDs and Xfce session PID before/after actual Cancel |
| v6 one normal restart | **PASS**: `PromptOnLogout=true` persists, actual Cancel preserves the new session; gated and native streams decode fresh 1280×720 VP8; actual XQuery keys/buttons empty |
| v6 confirmation aggregate | **PASS 3/3**: [report](test/artifacts/xfce-stage/confirmation-2026-10-28T16-50-22-723Z/report.json) |
| Local unit + mocked launcher lifecycle | **PASS 19/19**, including 7 new fake-Docker lifecycle cases; no live Docker call in those tests |
| v6 independent unaffected checks | **12 PASS + 4 prior PASS references, 1 Home FAIL**: [historical report](../../artifacts/independent-qa/desktop-polish/stage-neko-2026-10-28T17-08-21-413Z/result.json); aggregate remains FAIL |
| v7 Home policy, association and restart | **PASS 7/7**: [focused report](test/artifacts/xfce-stage/home-2026-10-28T17-20-24-568Z/report.json) |
| Independent focused v7 acceptance | **PASS 7/7**: [report](../../artifacts/independent-qa/desktop-polish/stage-neko-2026-10-28T17-41-34-416Z/result.json); unaffected v6 evidence explicitly referenced, no full v7 app-suite rerun claimed |

The v5 route suites exercised actual decoded video and mouse control: minimize/dock restore, maximize/restore, moving, border resizing, guest F11 separately from viewer fullscreen, Ctrl+T Home, default links, terminal exact-case/punctuation/UTF-8 command output, Thunar folder/context actions and Open Terminal Here. They also exercised bot rejection during human ownership, fullscreen Escape with held-state release, exact panel locks, and release cleanup. v6 changes only the supported logout property and stale-cache hygiene; unaffected app suites were not relabeled as newly rerun v6 tests.

The official native SDK maps macOS Command to Linux Control and Option to AltGr. Explicit Linux Alt/Super keysym chords are **EMULATED diagnostic input**, not proof of ordinary physical Mac shortcuts. The actual native mouse/typing path is tested independently. The viewer toolbar sends explicit Linux terminal/minimize chords, preserving a usable Mac path. Normalized HTTP text still preserves printable ASCII, LF and Tab and rejects unsupported Unicode with 422. UTF-8 terminal rendering/output does not broaden that input contract.

The prior [16:45 failed test](test/artifacts/xfce-stage/confirmation-2026-10-28T16-45-20-373Z/report.json) remains unchanged: confirmation was visible, but the observer searched only managed windows. The actual dialog is override-redirect, `WM_CLASS=Xfce4-session`, title `xfce4-session`; the repaired observer includes XQueryTree popup windows. No image rebuild was needed for this locator correction.

Physical Mac/mobile gestures, new Xfce performance/resource comparisons, new Xfce audio measurements, WAN behavior and online Help remain **UNTESTED**. Earlier baseline timing/audio evidence in README belongs to its historical image and is not assigned to this desktop revision. Mobile was explicitly deferred.

## Size and clean-runtime method

| Measured image | Uncompressed Docker image bytes | Meaning |
| --- | ---: | --- |
| Original Openbox UX1aca | 1,467,548,481 | Preserved baseline |
| Additive Xfce core | 1,545,899,853 | +78,351,372; old stack still present |
| Additive core + session | 1,586,581,295 | Another 40,681,442; still additive |
| Pruned flattened rootfs prototype | 1,558,521,695 | Fresh rootfs repack, before final config |
| Final v7 | **1,558,730,439** | **Net+91,181,958** versus 1aca, approximately 6.2%; +1,246 B versus v6 |

The seven explicitly purged packages were `openbox`, `tint2`, `picom`, `pcmanfm`, `xterm`, `feh` and `librsvg2-bin`:14,720 KiB of dpkg Installed-Size metadata. No broad autoremove was applied. Xdotool/wmctrl/X11 tools support the fixed app helpers and capture; Xorg/GStreamer/PulseAudio/codecs/fonts remain for media/runtime compatibility; shared libraries were retained conservatively. Udev and systemd-networkd binaries remain for integration's NAT VM layer.

A later-layer apt purge was measured separately and **did not reclaim lower image bytes**. The clean prototype instead exported a fresh, never-started candidate container, imported that verified rootfs with `FROM scratch`, and restored/asserted image ENV, user, entrypoint/CMD, healthcheck, labels, ports and other metadata. Final v7 reuses one large rootfs data layer, the standard empty WORKDIR layer and small configuration/bundle layers. The exact manifests, hash and metadata checks are in [pruned footprint](test/artifacts/xfce-pruned/2026-10-28T16-13-23Z/footprint.json); additive package changes are in [dependency footprint](test/artifacts/xfce-eval/2026-10-28T15-30-24Z/footprint.json).

After successful verification, only our reproducible 1,576,463,360-byte export tar was deleted with authorization. [Cleanup evidence](test/artifacts/xfce-stage/final-freeze/capacity-cleanup.json) retains its hash and recipe. All old images, containers and evidence remain, so host Docker disk allocation is not claimed to shrink by the image-size difference.

The failed passive panel guard was removed from startup/readiness. v5's historical source manifest still lists an unexecuted stale `.pyc`; that manifest was preserved. v6 excludes future caches in the build context and its merged filesystem was checked for absent guard source/cache/autostart. Historical raw layers are not claimed to contain no old bytes. No global input grab, GTK interception or patched panel was shipped.

## Start, stop and rollback

These commands target the accepted production-name Docker container. Coordinate an exclusive window for stopping, replacement or rollback. The retired stage is separate and remains stopped; use the authoritative root documentation for VM/LAN operation.

```sh
# Existing Docker Desktop must already be running.
bash pocs/neko/start.sh
bash pocs/neko/status.sh
bash pocs/neko/stop.sh

# Explicit coordinated cutover if the existing container has another image:
bash pocs/neko/start.sh --replace
```

Default start reads `current-image.json`, requires the exact local v7 tag/ID and native ARM64, and performs **no download or build**. An unchanged stopped container starts directly. A differing container is rejected until `--replace` is supplied; replacement stops and timestamp-renames the task-owned old container before creating a new one. It never removes the old container. Non-Neko labels are rejected. Ports remain loopback 7792/7902 and UDP59020–59039, with the same resource limits and no mounts. Start prints endpoints; `/desktop/state` establishes actual desktop readiness separately from `/health` API readiness.

For rollback, use the printed preserved container name. During a coordinated exclusive window, stop and rename the new `sidekicks-neko`, rename the selected preserved container back to `sidekicks-neko`, and start it. This restores that container's previous disposable filesystem. Alternatively, selecting the preserved baseline image explicitly with `SIDEKICKS_NEKO_IMAGE=sidekicks-neko:3.2.0-ux1` and `start.sh --replace` creates a fresh baseline container, preserving the current container first. No rollback command is executed by this documentation.

## Explicit rebuild path

```sh
# Configuration/client rebuild on the exact verified local, already-pruned v7 base:
bash pocs/neko/build-current.sh
# build-ux.sh delegates to that current Xfce path.
```

This uses the existing tested `Dockerfile.xfce-runtime` recipe and a new `polish-xfce-stage-rebuild-*` tag, without starting a service. It never overwrites v7. It needs the local frozen base, preserved 1aca/dependency manifests required by the guarded stage builder, official npm/apt access when uncached, and current read-only shared assets. Its resulting ID is a **new candidate requiring actual stage verification**, not automatically the accepted image. This host-only wrapper was syntax-checked; no new image was built during QA.

If the frozen image is missing, start fails with these instructions instead of downloading or falling back to old Openbox. Restore the exact tested image from a trusted retained image/rootfs, or deliberately replay the recorded chain under separate candidate tags and verify it: `Dockerfile.xfce-eval`/`build-xfce-eval.sh` → `Dockerfile.xfce-runtime`/`build-xfce-stage.sh` → `Dockerfile.xfce-prune`/`build-pruned-xfce.py` fresh export/repack → final runtime/config layer → `Dockerfile.xfce-confirmation` → `Dockerfile.xfce-home`. The original build logs/manifests show the inputs and hashes. Existing historical helpers have fixed historical default tags and exact base guards; do not overwrite those preserved tags when replaying. Floating official OS packages mean this is not a byte-identical cold OS rebuild guarantee. A new clean-source recipe has not been invented or accepted without GUI verification.

The default `Dockerfile` now only reuses frozen v7. The old full-source Openbox recipe, launcher and build script are preserved as `*.openbox-historical`, for history rather than the current build path. They are not advertised as reconstructing their old image against today's changed source tree.

Historical/reusable verification commands are shown below; no new tests ran during this metadata closeout. Unit/mock tests do not touch a live guest. The active stage scripts require a separately coordinated, running disposable stage; the accepted production desktops must not be substituted for it:

```sh
cd pocs/neko
npm test
bash -n start.sh build-current.sh build-ux.sh
# Exclusive STAGE window only; these actively control/restart the stage:
node test/xfce-stage.mjs
node test/xfce-stage.mjs --native
node test/xfce-confirmation.mjs --inspect
# Inspect its saved dialog image, then provide the measured Cancel coordinates.
node test/xfce-confirmation.mjs --x=768 --y=406
# Focused v7 test, also requires an exclusive stage window and restarts it once:
SIDEKICKS_NEKO_EXPECTED_IMAGE=sha256:36e11c585ece469a01f5bf705d5e1357afe767ba8e858eafba548d38786457a1 node test/xfce-home.mjs
```

## VM and transport integration

No Neko/Xorg/GStreamer binary changed during desktop packaging. The accepted no-NIC VM uses integration's separately verified loopback-candidate Neko binary and bounded vsock transport; the accepted NAT VM also retains that known binary over source image v7. Docker runs no VM integration service. The guest Supervisor owns all new environment requirements: `DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/sidekicks-session-bus`, `XDG_CURRENT_DESKTOP=XFCE`, `DESKTOP_SESSION=xfce`, `XDG_SESSION_TYPE=x11`, `LANG=C.UTF-8`, `LC_ALL=C.UTF-8`. These apply to the adapter and every GUI child as well as Xfce. Existing DISPLAY/USER/HOME/runtime/Pulse/Neko variables remain required; guest init must create root1777 `/tmp/.ICE-unix` and X11 runtime directories.

The lease enforcement, native Neko host revocation, cursor/hotspot, fullscreen cleanup and exact internal Host/Origin/source-address rules remain intact. Host-forwarded VM traffic must remain source 127.0.0.2; only exact guest 127.0.0.1 can obtain/use the desktop-page capability. Current pending/ready semantics cancel future bot injection and release held state; they do not promise rollback or a generic application event-queue flush.
