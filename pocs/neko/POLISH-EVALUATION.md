# GTK3/Xfce desktop evaluation

**Historical source/build evaluation snapshot.** The statements below describe the initial dependency evaluation, before runtime work. The current worker-tested, pruned Xfce v6 image, actual verification and reserved stage ports are documented in [XFCE.md](XFCE.md). In particular, **17902 is live LAN and must never be used for staging**; actual stage viewer17802/raw17782 and API17792 were assigned later.

October 28, 2026, initial evaluation. At that point no candidate desktop had been started.

The current live/accepted image remains `sidekicks-neko:3.2.0-ux1`, exact ID `sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53`. No live container, browser, VM, host app, LAN proxy or input session was queried or changed by this evaluation. Docker image inspection and isolated image builds were the only Docker operations.

The scope initially covered a small Thunar upgrade, then expanded to a coherent Xfce desktop. The narrow candidate and its evidence remain preserved; it is not the final direction. Automatic approval review rejected a pending second narrow footprint refinement after the newer scope arrived. That command did not execute and was not retried.

## Recommendation

Integrate a coherent X11 Xfce session using Xfwm4, xfce4-panel, xfdesktop4, xfsettingsd, Thunar and xfce4-terminal. Include xfce4-session for standard component lifecycle, with an explicit Sidekicks session configuration. Keep Firefox and the existing Xorg/Neko/GStreamer/PulseAudio transport. Keep original Sidekicks/Adwaita GTK3 assets and DejaVu fonts; do not copy unlicensed cloud-reference assets.

Run Xfwm4 as the sole window manager/compositor. Openbox, tint2 and picom remain in the evaluation base for recovery/size comparison but must not run alongside the new session. This is a desktop replacement within the disposable guest, not a distribution upgrade or a claim that all installed software uses GTK3.

## Measured package layers

| Layer | Image tag | Uncompressed image size | Image increase over previous layer | dpkg Installed-Size increase |
| --- | --- | ---: | ---: | ---: |
| Accepted base | `sidekicks-neko:3.2.0-ux1` | 1,467,548,481 B | — | — |
| Xfce core | `sidekicks-neko:polish-xfce-core-deps` | 1,545,899,853 B | 78,351,372 B | 98,087 KiB |
| Core + session | `sidekicks-neko:polish-xfce-session-deps` | 1,586,581,295 B | 40,681,442 B | 38,333 KiB |

Core image: `sha256:1a5de6c2d6bac9e538adaecd68fd9fab375c64b9622745e9a8ca83c3941037bf`.

Core + session image: `sha256:c4cfcdf56e95654b64a1841ff43797a1b6a21202f27a5740c55edee9409071c1`.

Both are native `linux/arm64`. Core explicitly installs official Debian Trixie `xfwm4`, `xfce4-panel`, `xfdesktop4`, `xfce4-settings`, `xfce4-terminal`, `thunar` and existing `dbus-x11`, without recommendations. It adds 45 packages, downloads 15.3 MB of archives, and upgrades/removes zero existing packages. The optional session adds 10 packages and downloads 11.1 MB. It adds no display manager, screensaver, Polkit agent, UDisks service, network manager or full `xfce4` task/metapackage.

The core includes approximately 51 MiB of Perl package metadata, largely from the settings/helper dependency chain. The optional layer includes 35,962 KiB for `x11-xserver-utils`/`xrdb`, its required C preprocessor and numerical libraries. `xfce4-session` plus the Polkit **client library** account for the remaining 2,371 KiB. Thus the raw optional-layer difference overstates the marginal session cost if the shared legacy XTerm/UXTerm resources will already require `xrdb`.

These are uncompressed image-layer measurements and dpkg package metadata, not compressed pull size, unique disk allocation, RSS or performance. Original Openbox/tint2/picom/PCManFM/xterm/Firefox files remain in the base. A clean final build may omit unused packages, but deleting them from a new layer does not recover their bytes from the existing base layers.

Machine-readable [footprint and complete package changes](test/artifacts/xfce-eval/2026-10-28T15-30-24Z/footprint.json), [core apt plan](test/artifacts/xfce-eval/2026-10-28T15-30-24Z/packages/core-plan.txt), [optional session plan](test/artifacts/xfce-eval/2026-10-28T15-30-24Z/packages/session-plan.txt), manifests and build logs are preserved together.

## Verified during build

- All requested packages installed from official Debian repositories on ARM64. Versions: Xfwm4 4.20.0-1; panel 4.20.4-1; xfdesktop4 4.20.1-1; settings 4.20.1-1; terminal 1.1.4-1; Thunar 4.20.2-1+deb13u1; session 4.20.2-2.
- No existing package version changed. Every checked component's dynamic libraries resolved; GTK3 linkage is recorded in the exported `ldd` output.
- Exact SHA256 of `/usr/bin/neko`, `/usr/lib/xorg/Xorg` and `/usr/lib/aarch64-linux-gnu/libgstreamer-1.0.so.0` remained unchanged in both layers. This establishes file identity, not a live stream test.
- Accepted image tag/ID remained unchanged before and after the build.
- Both evaluation images deliberately retain the accepted **Openbox** entrypoint/config. They are dependency layers, not configured Xfce sessions. Do not start them expecting the proposed desktop.

Reproduce the isolated dependency builds:

```sh
bash pocs/neko/build-xfce-eval.sh
```

The helper inspects/builds images and exports build evidence only. It contains no container run/start/stop/exec, viewer, socket-input or lifecycle operation. Its required base ID is guarded.

## Integration implications and next implementation

1. **Session and processes.** Replace the desktop startup body and readiness checks; the current checks explicitly expect Openbox/tint2/picom and must not claim readiness from an unrelated desktop. Use a single `neko`-owned D-Bus session shared by the Xfce components and adapter-launched apps. A bus launched as root is not equivalent: its authentication/ownership can exclude the unprivileged desktop. Avoid separate accidental auto-launched buses. Configure the session's exact startup clients, no saved-session surprises, and no duplicate browser/process launch.
2. **Desktop/menu coherence.** Xfdesktop should own the background; do not leave competing `feh`/desktop-manager roots. Configure xfsettingsd's GTK theme, icons, fonts, pointer size and explicit DPI from the original shared assets. GTK theme and Xfwm frame theme are separate settings. Supply only the supported browser/terminal/files panel launchers and either no desktop menu or a curated menu. Preserve useful normal window controls and app context menus. Hiding settings/menu entries is product polish, not a security sandbox.
3. **Terminal.** Make GTK3/VTE `xfce4-terminal` the supported launcher, with stable class/window enumeration, UTF-8, the shared navy palette and 88×24/11pt font. Verify the actual CLI/class/server behavior before relying on it. Preserve XTerm/UXTerm shared resources for legacy/manual apps. `uxterm` is an xterm wrapper selecting a different resource class/UTF-8 locale, not a separate emulator. A UTF-8 terminal does not broaden the normalized Neko input API's existing printable-ASCII/LF/Tab contract or unsupported-Unicode rejection.
4. **Apps and ownership.** Retain the per-app launch lock, normal-client filtering, same-window/PID/geometry restoration, and close-stays-closed semantics. All Home app launches continue through the fixed allowlist and current lease gate. Default web links should use the Sidekicks Firefox profile; startup/Home/actual new tabs remain guest-local `/home`. No browser swap or blanket undecorate rule is required.
5. **Capture/ABI.** Neko still captures the same Xorg screen. Xfwm composition and xfdesktop/panel redraws can change damage/capture workload, cursor presentation, font sizing and startup order even though transport files are identical. Xfsettingsd must not unexpectedly resize the dummy display. Force the intended software compositor path; do not claim GPU acceleration from installed libraries. Check real VP8 decode, actual cursor hotspot/observer behavior and capture freshness in both input routes.
6. **Networking/VMs.** No backend Host/Origin/socket-source guard, lease/native-host transport, ICE configuration, port publication, vsock relay or parent-owned LAN proxy changed. The no-NIC VM still needs the separately verified loopback-candidate Neko binary layered over any final rootfs; this dependency image contains the Docker baseline binary. No Docker daemon is needed inside a VM. Preserve supervisor/init requirements, user ownership, runtime directories, D-Bus socket setup and GTK3 module/icon dependencies when exporting a rootfs.
7. **Resources.** Keep the same 2 CPU/2 GiB baseline limits initially. Measure process RSS/PSS and CPU at idle and during the same synthetic motion workload, startup/readiness time, screenshot latency and decoded-frame response after integration. Additional installed bytes do not prove additional resident memory or latency. Run comparisons sequentially and record concurrent activity.

## Safe apply/QA plan

The following plan was written before the granted stage runtime window; see XFCE.md for completed evidence. Keep all live targets and accepted tags frozen until the integration lead grants a runtime window and confirms preservation. Kevin's Docker noVNC 7901 windows/jobs remain outside this worker's ownership and must never be disrupted by a Neko test.

Build a separately named **configured** Xfce candidate from the measured dependency base after source review. Preserve the accepted image, container and any current guest user state before replacing anything. Prefer an independent staging container, matching resource limits, no host mounts/privilege, and loopback-only ports checked/reserved by integration. The subsequently assigned stage uses API17792, raw viewer17782, frontend17802 and UDP59420–59439. Port17902 is live LAN and is excluded from staging.

Do not widen the backend's fixed Host allowlist for staging. A parent-owned, stage-only loopback proxy can preserve the accepted upstream Host/Origin contract while forwarding only the specific evaluation listener; the WebSocket and ICE port mapping require the same deliberate stage configuration. Internal guest Home remains 127.0.0.1:7792 and the capability route still requires actual guest source 127.0.0.1. Forwarded VM traffic remains source127.0.0.2. An alternative is a coordinated swap on the accepted ports after preservation. No staging service/proxy has been started.

QA must exercise actual stream decode, both gated and native routes, desktop/panel right-click behavior, Thunar rendering and local operations, Firefox Home/new tab/default links, GTK3 terminal UTF-8/command output and same-client restoration, real window controls, blur/disconnect/fullscreen held-state cleanup, bot rejection during human ownership, screenshot freshness, normal restart and LAN proxy boundaries. Mobile remains deferred. All these new Xfce runtime checks are **UNTESTED**.

## Preserved narrow experiment

`sidekicks-neko:polish-thunar-eval` image `sha256:a98bc0921f5e38541f790640ab9812ba6cabef029d0372be350a558e2ed313be` was built before the expanded scope. It adds 44,273,995 image bytes and 50,462 KiB package metadata to the accepted base, with the shared original GTK3 assets, Thunar app swap, terminal resources and MIME associations. It was never started. Evidence: [narrow footprint](test/artifacts/polish/2026-10-28T15-26-19Z/footprint.json).

The staged `Dockerfile.polish`/`build-polish.sh` subsequently describe an **unbuilt** second refinement; do not confuse that source with the first image's frozen evidence. Narrow source changes remain paused for reference, not a production handoff. Existing 11 lease/desktop-boundary unit tests and Python/XML/shell syntax checks passed during that source work; these do not validate the new Xfce UI.

Primary references: [Debian Xfce packages](https://packages.debian.org/stable/xfce/), [Xfce session](https://docs.xfce.org/xfce/xfce4-session/start), [Thunar preferences](https://docs.xfce.org/xfce/thunar/preferences), [xfce4-terminal package](https://packages.debian.org/trixie/xfce4-terminal), [uxterm wrapper](https://manpages.debian.org/trixie/xterm/uxterm.1.en.html).
