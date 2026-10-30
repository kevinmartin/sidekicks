# Coherent guest desktop refresh

The sustainable candidate is an official Debian **Xfce X11 session**: Xfwm4 as sole window manager/compositor, xfce4-panel, xfdesktop, xfsettingsd/xfconf, Thunar and the GTK3/VTE xfce4-terminal. Keep the current Debian releases, Linux kernel, noVNC/Neko servers, X11 input gates, loopback/private APIs and LAN forwarding contracts. GTK is an application toolkit, not an OS or transport version. Chromium still draws much of its own UI; legacy xterm uses Xaw, while uxterm wraps xterm with UTF-8 and another resource class.

This replaces a mixed runtime of Openbox, tint2, picom, differently built PCManFM versions and per-launch xterm styling. A narrow Thunar swap is cheaper and useful as an isolated baseline, but leaves multiple desktop/settings owners and inconsistent generic launch paths. Standard session/settings ownership is the stronger long-term POC default, subject to actual staging validation.

## Measured dependency candidates

| Candidate | noVNC/bookworm image delta | Neko/trixie image delta |
|---|---:|---:|
| Narrow Thunar evaluation |5,727,027B|44,273,995B initial evaluation including xrdb dependencies|
| Xfce core/session candidate |20,659,464B total|78,351,372B core +40,681,442B session =119,032,814B total|

These are Docker-reported image size deltas, not downloaded bytes, host disk allocation, startup time or memory. Package metadata is separately recorded: noVNC full candidate adds40 packages/41,284KiB Installed-Size; Neko adds45 core +10 session packages/136,420KiB total. Neither changed pre-existing package versions. Neko's optional layer is mostly xrdb/x11-xserver-utils/compiler-preprocessor dependencies plus a Polkit **client library**, not a Polkit service/agent. No display manager, network manager, screensaver or host permissions are added.

All figures above are **additive dependency evaluations with old components still installed**, not net growth after removals. Separate clean-runtime prototypes now measure actual removal, with final functional/restart acceptance still pending:

| Prototype | Original Docker image bytes | Clean/pruned image bytes | Net delta | Method |
|---|---:|---:|---:|---|
| noVNC |1,059,242,453|1,018,100,514|−41,141,939 (−39.2MiB)|Fresh pinned Debian build;10 layers versus14, no deletion whiteouts|
| Neko |1,467,548,481|1,558,521,695|+90,973,214 (+86.8MiB)|Fresh never-started candidate rootfs, bounded desktop-package purge, then explicitly restored image metadata in a new rootfs image|

The noVNC package metadata changes by+41,284/−81,073KiB plus2KiB in updated SSL packages, net−39,787KiB. It omits the old stack, GTK2/libfm, xterm/Tk and unused device/system dependencies while explicitly retaining the manually copied x11vnc0.9.17 runtime libraries. A Debian SSL security revision changed fromu1 tou2; that difference is recorded rather than attributed to Xfce. Neko removes seven known unused graphical packages (14,720KiB); it conservatively retains codec/CGO/networking dependencies. Its intermediate later-layer purge image actually grows; only the separate repacked rootfs demonstrates layer-byte reduction. These are provisional exact-image measurements, not yet final deployed identities. Source: `artifacts/novnc/polish/clean-footprint.json` and `pocs/neko/test/artifacts/xfce-pruned/2026-10-28T16-13-23Z/footprint.json`.

Image size, package Installed-Size and shared host disk allocation are different quantities. Old/live images, caches and rollback data remain retained, so no host disk reclamation is claimed. Transport/input/query tools, codecs, fonts and runtime libraries remain where actually used. Final images must repeat the functional/restart checks before promotion.

Sources: `artifacts/novnc/polish/{dependencies,xfce-dependencies}.json` and `pocs/neko/test/artifacts/xfce-eval/2026-10-28T15-30-24Z/footprint.json`. Dependency images initially retain old entrypoints: those size results alone do not establish a working Xfce session. CPU/RSS and decoded-stream/window behavior remain to be measured on final runnable stages. Available host disk space at the checkpoint was36GiB, so template/export preparation must be bounded and avoid unnecessary duplicate archives.

## Configuration and ownership

- Use installed Adwaita GTK3 with original Sidekicks cyan CSS/icons and existing DejaVu fonts. Do not copy the cloud's proprietary OrbitOS branding/theme/font bundle.
- Integration owns `shared/desktop/polish/{settings.ini,gtk.css,icons,Xresources}`. The noVNC worker owns canonical `shared/desktop/polish/xfce/` templates/generator; Neko copies them read-only with explicit helper/home/wallpaper substitutions and documents4.18/4.20 differences.
- One managed Browser/Terminal/Files launch-or-restore path feeds Home, dock, shortcuts and supplied default-app entries. Restoring an existing browser must preserve its tabs. Chromium should use embedded controls; Firefox may remain if its current native controls are reliable.
- Disable or curate desktop/panel settings and obsolete menus; preserve useful window controls and normal in-application context menus. This is UX curation, not a security sandbox.
- Run one shared guest-user D-Bus session for desktop and adapter-launched GTK applications, and one compositor. Do not accidentally leave Openbox/tint2/picom running beside Xfce.

Use Xfce's supported named-user panel configuration lock (`unlocked="root"`), rather than custom input interception. Stock Help/About entries may remain if useful. Neko's staged About works; Help opens an online-manual prompt (external documentation loading remains unverified in no-NIC guests). Stock Log Out was observed closing apps immediately; Supervisor then recreated a fresh desktop. That is disruptive, not a harmless menu entry. The final candidate must set supported `PromptOnLogout=true`, demonstrate actual Cancel retaining app/session identities and verify restart persistence before promotion. Confirmed logout still deliberately ends the guest session. The rejected passive Button3 guard and failed descendant probe are preserved as failed experiments; neither belongs in final startup or readiness. There are no global input grabs, panel-source forks or GTK event filters.

Browser startup, New Tab and Home are separate behaviors. Independent v6 Neko QA found that explicit startup `/home` and the AutoConfig New Tab override worked while Alt+Home opened Firefox's default page. The installed policy incorrectly used `Homepage.StartPage:"home"`; Mozilla's [Homepage policy](https://firefox-admin-docs.mozilla.org/reference/policies/homepage/) requires `"homepage"` for that startup behavior and treats New Tab separately. The small successor must install the corrected JSON into Firefox's actual distribution-policy path and verify all three UI paths after restart. Merely inspecting an unused copy under `/etc/sidekicks` or seeing one Home tab is insufficient.

## Preservation and validation

Kevin's live Docker7901 comparison contains two Chromium processes7579/7731 and terminals7907/7917/8186. Initial window/config/screenshot evidence is under `artifacts/desktop-polish/before/`; the user-modified full-width dock is also captured. No initial input or lifecycle mutation occurred.

The old startup deletes `/tmp/sidekicks-browser`; restart also destroys live terminal jobs and scrollback. Saved images/screenshots are not live-session preservation. Build and verify a separate stage first. Do not restart or close these five clients merely to apply cosmetics. Any later live migration must be explicit, verify client/process preservation and report remaining limitations. Stage restart evidence must stay distinct from restart of the user-occupied7901 session.

**Later user authorization:** Kevin confirmed, “Feel free to restart that container. I'm done with it.” The comparison-session hold is therefore released once the staged candidate passes. A coordinated normal stop/rename/rollback-preserving replacement is preferred to a non-atomic hot WM migration. The original window/profile/config evidence remains historical; no claim of preserving live terminal processes through restart is made. No cutover happened merely because permission was received.

Independent QA uses the bounded `artifacts/independent-qa/desktop-polish/protocol.md` matrix after explicit per-target handoff: actual viewer menu behavior, modern Files/Terminal, Home and Chromium CSD, ordinary window controls, LAN/lease compatibility, component ownership and preservation-cleared persistence. Mobile and performance comparisons are deferred; no claim follows from HTTP health, package installation or a screenshot alone.
