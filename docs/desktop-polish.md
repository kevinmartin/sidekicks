# Sidekicks Xfce desktop refresh

All five production instances now run the independently verified Xfce/GTK3 refresh. **Bounded interaction, one normal restart per desktop, existing LAN viewing and the final passive audit passed.** The five desktops remain running; disposable stages are stopped with their rollback data retained. Current identities are recorded in [rollout status](status/desktop-polish.md), with the independent report in [desktop-polish verification](desktop-polish-verification.md).

## What changed and why

The earlier noVNC desktop mixed Openbox, tint2, PCManFM built against GTK2 and several uncoordinated launch paths. Its stock Debian desktop menu exposed settings and missing commands. The Sidekicks browser helper deliberately selected Chromium's system frame and a dedicated profile, while a generic browser entry bypassed that helper/profile and its Home/New Tab configuration. The dark terminal was an explicitly configured xterm; generic xterm and uxterm used different default resources. uxterm wraps the same xterm engine with UTF-8 and a different resource class.

The supported desktop now uses official Debian Xfce session components: Xfwm4 as the sole WM/compositor, xfsettingsd/xfconf, xfdesktop, xfce4-panel, **Thunar GTK3** and **xfce4-terminal GTK3/VTE**. NoVNC keeps Chromium with real embedded controls; Neko keeps Firefox with reliable native controls. Browser, Terminal and Files helpers feed the supplied dock, curated desktop menu, Home page, shortcuts and default browser associations. Existing-window restore preserves identity/geometry/tabs. Terminal defaults share DejaVu Sans Mono11 and the same navy/text palette; supported launchers no longer offer competing xterm/uxterm choices.

GTK is a toolkit, not an OS or isolation version. The guest Debian releases, signed VZ runners, Linux kernel and transport/control boundaries remain. Bookworm uses Xfce4.18 and terminal1.0.4 (managed window role); Trixie uses Xfce4.20 and terminal1.1.4 (supported explicit WM class). Both use GTK3/VTE for the primary terminal. This is a coherent desktop, not a claim that every library or browser widget is implemented in GTK3.

Installed Adwaita themes/icons receive original Sidekicks cyan accents and folder artwork with existing DejaVu fonts. No proprietary OrbitOS theme/font/mascot bundle was copied.

## Menus, Home and lifecycle

Desktop menus contain the three supported apps. Xfce's named-user panel lock removes customization access; standard Help/About/Log Out remain. About works; Help opens an online-manual prompt, whose external load is not guaranteed in no-NIC guests. This menu curation is **not a security sandbox**.

Log Out initially ended apps immediately in a disposable stage. Both candidates now enable supported logout confirmation, with actual Cancel preserving Browser/Terminal/Files IDs/PIDs, session identity and content before/after a normal restart. Confirming logout still intentionally ends apps: Neko's Supervisor recreates the desktop, while noVNC reports503/ready:false until restarted. Closing only the browser leaves the desktop ready, with dock reopen available.

Home, startup and New Tab are checked separately. Independent Neko QA found an invalid installed `Homepage.StartPage:"home"` policy even though explicit startup and the separate New Tab override worked. The v7 image installs `"homepage"` in Firefox's actual distribution policy path; seven focused independent checks pass, with unaffected v6 evidence explicitly retained. The old Home failure is not erased. Actual Chromium's house button and profile preferences pass; ordinary Mac Alt+Home has a separate upstream modifier translation and is not treated as a broken Home preference.

## Image footprint

| Backend | Previous accepted bytes | New accepted bytes | Net image bytes |
|---|---:|---:|---:|
| noVNC |1,059,242,453|1,018,096,080|−41,146,373 (about39.2MiB smaller)|
| Neko |1,467,548,481|1,558,730,439|+91,181,958 (about87.0MiB larger)|

NoVNC is a fresh pinned Debian runtime with verified noVNC/x11vnc payload, not a later-layer removal. It adds41,284KiB of package metadata and removes81,073KiB; two official SSL security-revision updates add2KiB, for−39,787KiB net. Neko conservatively removes seven unused graphical packages, then uses a fresh verified rootfs layer plus small configuration layers; a purge in a later layer alone did not save underlying image bytes. Runtime codecs, fonts, X11, udev/networkd and required libraries remain. Package Installed-Size, Docker image bytes and shared host disk allocation are distinct. Old images/disks/rollback data remain, so these numbers do not imply host disk reclamation or performance improvement. See [build/footprint details](architecture/desktop-stack.md).

Exact images:

- noVNC `sha256:f8dee4d59f9bb8b26676928b80f784cd4f982a591812c0f1fe501d0b564a1b38`.
- Neko `sha256:36e11c585ece469a01f5bf705d5e1357afe767ba8e858eafba548d38786457a1`.

## Viewers and operation

| POC | Mac viewer | Existing LAN viewer |
|---|---|---|
| noVNC Docker · RFB |http://127.0.0.1:7901/|http://192.168.0.97:17901/|
| Neko Docker · WebRTC |http://127.0.0.1:7902/|http://192.168.0.97:17902/|
| noVNC VM · no NIC/vsock |http://127.0.0.1:7903/|http://192.168.0.97:17903/|
| Neko VM · no NIC/UDP over vsock |http://127.0.0.1:7906/|http://192.168.0.97:17906/|
| Neko NAT VM · direct guest UDP |http://127.0.0.1:7907/|http://192.168.0.97:17907/|

Neko native diagnostic uses `?mode=native`. LAN NAT media is forwarded through the Mac to the guest; its signaling/control remains on the existing viewer boundary. Workbench stays local at http://127.0.0.1:7790/; the existing LAN index is http://192.168.0.97:17901/lan/index. Existing LAN exposure/authentication has not been broadened, and shell/admin/private RPC remain local. Reload an old viewer tab after a backend replacement.

```bash
# Start/resume only the pinned current Docker desktop; no implicit download/build.
bash pocs/novnc/start.sh
bash pocs/neko/start.sh

# Inspect/start a stopped current VM; stop syncs that guest before power-off.
python3 pocs/microvm/microvm.py status a
python3 pocs/microvm/microvm.py start a
python3 pocs/microvm/microvm.py stop a
# Other canonical VM IDs: neko, nat.
```

Explicit rebuilds create candidates requiring verification, not automatic production replacements. See [noVNC polish](../pocs/novnc/POLISH.md), [Neko Xfce](../pocs/neko/XFCE.md) and [VM preparation/rollback](../pocs/microvm/polish/README.md). Current images and the earlier verified transport artifact are local prerequisites; no new unsupported host software, OS upgrade or security permission was introduced.

## Evidence and limits

Independent stage report: `artifacts/independent-qa/desktop-polish/stage-report.md`. Actual noVNC production smoke: `production-novnc-smoke-2026-10-28T17-46-24-758Z/result.json` in that directory. Exact new production identities are under `artifacts/desktop-polish/production/`; VM transition journal is `pocs/microvm/artifacts/desktop-polish/promotion.json`. Failed guard experiments, invalid Home policy, test-locator failures and timeouts remain separately recorded.

The frozen independent final report is [desktop-polish verification](desktop-polish-verification.md), SHA256 `5eaf6dfa0bee61fde4cb55aee1c96343ae880333286bdf79a5f397246a82f439`. That written report was saved as a new Library item, preserving earlier POC reports. Its exact identity/version and successful local metadata application are recorded in `artifacts/desktop-polish/library-deliverables.json`; no screenshots were uploaded.

The current production acceptance records actual viewer interaction, configuration/toolkit observation, logout Cancel preserving three apps and their content, one supported normal stop/start, fresh decoded input markers on the existing LAN path, and actual held-input cleanup. Neko also has separate native-route input/cleanup checks. Docker Neko and the relay/NAT VMs each passed ten checks; both noVNC targets passed eight each: **46 accepted groups, comprising35 new PASS and11 explicitly referenced PASS**. Integration verified the raw hashes and the full reference chains back to original passing checks. NoVNC results span preserved attempts after driver interruptions; the VM A restore-bubble failure remains alongside its successful bounded continuation. These are scoped functional checks, not a rerun of the old performance or full physical-keyboard matrix.

The final independent audit at18:31UTC found all five ready with one Home browser each, sole Xfce components, no owner and no actual held keys/buttons. Eleven private TCP listeners remain127.0.0.1-only; the five intentional LAN TCP listeners retain their previously authorized wildcard bindings. The LAN bridge PID/address/NAT target, workbench health and historical report hashes were unchanged. See `artifacts/independent-qa/desktop-polish/{production-summary,final-production-state}.json` and `artifacts/desktop-polish/production/final-readiness.json`.

After QA released the window, integration stopped only the exact two task-owned staging containers and their proxy. Containers/images were preserved. Production Docker identities/StartedAt values were unchanged and all six service health checks remained200; `artifacts/desktop-polish/production/stage-cleanup.json` records that cleanup.

Integration personally inspected saved production pixels showing Chromium's embedded controls/Home via LAN, Neko's Thunar/VTE styling, and the Neko postrestart Home page. Exact image paths/hashes are in `artifacts/desktop-polish/production/visual-review.json`. No new browser screenshots were uploaded to Library for this refresh.

Mobile/physical-phone testing is deferred. Physical Mac reserved-key/trackpad behavior is not inferred from synthetic browser actions or explicit Linux SDK chords. Historical performance measurements predate this refresh and were not rerun. Chromium may show a restore-pages bubble after power-off/restart; QA records/dismisses it as an observed setup nuance. Stop/restart preserves configuration/disks, not live terminal jobs.

A disposable NAT clone reported the same IP as the running canonical guest and was immediately stopped before media testing. The original stream subsequently decoded successfully with the same boot. Rollout stops old NAT before starting its replacement; simultaneous NAT clones remain unvalidated, not an Apple-platform impossibility claim. No host routing/firewall/VPN changes or private-service scans were used.

Rollback retains Docker `sidekicks-novnc-pre-xfce-20261028` and `sidekicks-neko-preserved-20261028t175109110539z`, plus their images. Prior VM disks are stopped under `.state/*-pre-polish-v1/`; old templates/runners/kernel are cloned under `.build/pre-desktop-polish-v1/`. Rollback needs an exclusive window, stopping the new generation first; no prune/reset/clean was performed.

## Later operational follow-up

After the frozen QA audit, Kevin reported clicking Log Out on7901. Actual inspection found a black desktop, all five Xfce components and guest apps absent, and `/desktop/state`503: logout had completed, rather than leaving a confirmation open. Integration performed one supported stop/start of only the same noVNC container, retaining its image, filesystem and browser profile. A fresh actual RFB frame showed Sidekicks Home; the observed Home-button click was accepted, and final owner/held input were empty. Other Docker identities/StartedAt were unchanged. This additional user-requested recovery is separate from the exactly-one-restart-per-target QA counts above. Evidence and preserved verifier attempts: `artifacts/desktop-polish/recovery-7901/`.

Neko used Debian Firefox ESR in the earliest preserved source build (`artifacts/neko/build.log`, initial package-install step), before the later UX/Xfce work. Initial registry access and AMD64-only Docker Hub image constraints are documented in `pocs/neko/README.md`; they explain the native ARM64 build route, not a proven inability to run Chromium. No Git commits or recorded Chromium-versus-Firefox comparison justify a stronger historical claim. Neko's [official image documentation](https://neko.m1k1o.net/docs/v3/installation/docker-images) lists both browsers and calls Firefox its historical default. Current Firefox was retained during this refresh rather than silently changing another verified application; Chromium migration remains possible with its own browser/profile/runtime verification.
