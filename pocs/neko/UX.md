# Sidekicks sky desktop revision

This revision replaces the kiosk startup with a normal Firefox window, a shared cyan/blue wallpaper and light local Home, and a floating translucent three-app dock. It retains Openbox, tint2, the pinned Neko v3.2.0 binary, native WebRTC input and the separate lease-gated HTTP input path. Picom uses software XRender; this does not enable GPU encoding.

The accepted baseline image `sidekicks-neko:3.2.0-source` remains exactly `sha256:a7120e76048242bacf22b36e3c916339f71ad82858e0ce69c53d2fe8611fec85`. Its stopped container is preserved as `sidekicks-neko-baseline-preserved`. Historical baseline latency/audio/fixture reports remain historical; they do not measure this compositor/windowed revision.

Current verification and exact image identity are in [ux-status.json](ux-status.json). New test evidence is under `test/artifacts/ux/`; failed attempts are retained. Independent QA is required after the worker's checks before a final acceptance claim.

## Run and restore

```sh
# Build only; no service is started or restarted.
bash pocs/neko/build-ux.sh
# Apply the built image to the task-owned container only.
SIDEKICKS_NEKO_BUILD=0 bash pocs/neko/start.sh
bash pocs/neko/stop.sh
```

Viewer: http://127.0.0.1:7902/ . Native diagnostic: http://127.0.0.1:7902/?mode=native . API: http://127.0.0.1:7792/health . Desktop readiness: http://127.0.0.1:7792/desktop/state . All published ports remain loopback only; UDP59020–59039,2CPU/2GiB, no mounts or privileged mode.

To restore the accepted image through the same task launcher, use `SIDEKICKS_NEKO_BUILD=0 SIDEKICKS_NEKO_IMAGE=sidekicks-neko:3.2.0-source bash pocs/neko/start.sh` during an agreed input/lifecycle window. The preserved stopped baseline container is additional evidence/recovery state, not a second active desktop.

## Window and app behavior

Firefox starts as a decorated1120×552 client near the center with visible desktop margins. Its separate WM titlebar contains actual minimize, maximize and close buttons. Double-clicking or dragging the titlebar and dragging window borders uses Openbox bindings. Closing the last browser window leaves the desktop running and stays closed until an explicit launch.

The dock's Browser, Terminal and Files buttons call fixed scripts. A per-app nonblocking lock prevents duplicate launches while the first window maps. Existing normal EWMH clients with the expected WM_CLASS are activated through `wmctrl -ia`, which restores a minimized window without changing geometry or creating tabs. Enumeration uses read-only `xprop` because the installed ARM64 `wmctrl -lx` crashed during the first startup. Legacy clients with no `_NET_WM_WINDOW_TYPE` are treated as normal; explicit utility/dialog/dock clients are excluded.

The Home buttons use POST `/desktop/launch` with a per-boot page token, a fixed three-app allowlist, and active lease ownership. No shell, arguments, URL or path is supplied by the page. Both token issuance and launch require the actual guest connection source127.0.0.1 and internal Host127.0.0.1:7792; launch also requires exact same Origin. Host-forwarded microVM connections must use a distinct guest source127.0.0.2. Forged Host/Origin headers alone must not obtain a token or launch an app.

Firefox AutoConfig imports its installed `AboutNewTab.sys.mjs` and sets the actual new-tab URL to local `/home`; the homepage preference alone is insufficient. This is a version-specific Firefox integration inside the disposable guest. The AutoConfig JS setting affects the local AutoConfig environment, not the browser content sandbox or host OS. Verify actual Ctrl+T and the visible plus button on the installed ESR after upgrades. Home request counts in `/desktop/state` are non-secret diagnostic evidence; New Tab can intentionally show a blank URL bar even when the local Home document loaded.

## Cursor and fullscreen

Neko's capture has `show_pointer:false`: the stream contains no baked pointer. Cursor images and exact image-pixel hotspots arrive separately over the WebRTC data channel. During gated human control the input layer uses that image as its local CSS cursor and hides the observer image. Observers see one positioned remote cursor with their local pointer hidden over the desktop. Native mode retains the official SDK cursor/input overlay; its observer local pointer is hidden. Hotspots remain image pixels while desktop coordinates scale with the fitted video rectangle.

**Viewer fullscreen** changes the local viewer; the visible **Exit fullscreen · Esc** button or Escape exits it. Letterboxed video and input regions keep the same bounds. The fullscreen transition releases ownership/held input at the server, resets the SDK overlay via its published attach disposer, and restores this viewer's human lease. **Guest F11** separately changes Firefox inside Linux and does not make the local viewer fullscreen.

The native SDK still maps macOS Command to Linux Control. Physical Mac trackpad, OS cursor rendering, mobile gestures, and performance under the new compositor remain untested unless new evidence explicitly says otherwise. Headless screenshots omit the operating system's pointer, so screenshot appearance alone is insufficient cursor proof; tests inspect CSS hotspot/observer rendering and decoded video configuration separately.

## Verification commands

```sh
cd pocs/neko
npm test
npm run build
cd ../..
node pocs/neko/test/ux-smoke.mjs --finish
node pocs/neko/test/ux-smoke.mjs --native --finish
node pocs/neko/test/escape-debug.mjs
node pocs/neko/test/escape-debug.mjs --native
node pocs/neko/test/observer-proof.mjs
```

The UI checks use a separate headless Chrome context, actual decoded WebRTC frames and browser mouse/keyboard inputs. Read-only EWMH/XQuery queries inspect resulting window geometry, IDs/PIDs and actual held keys/buttons. They do not control the Mac cursor or inspect user browser tabs. The fixture remains available through Home's Input Lab link; its viewport is now a windowed browser content area, so historical kiosk-coordinate tests need a measured window offset or explicit maximize step.

Primary implementation references: [Openbox mouse bindings](https://openbox.org/help/Bindings), [tint2 configuration](https://manpages.debian.org/trixie/tint2/tint2.1.en.html), [Firefox AutoConfig](https://support.mozilla.org/en-US/kb/customizing-firefox-using-autoconfig), [Firefox new-tab module](https://github.com/mozilla-firefox/firefox/blob/main/browser/modules/AboutNewTab.sys.mjs). Installed package versions and resolved base images are recorded in the build evidence.

Final worker handoff: image `sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53`, Home SHA256 `45899f0abc84afd59fec79aa7d3b149202965f32a1052f87dcad33002b1cc14a`. Source is frozen; both final finish runs are6/6PASS. Earlier whole diagnostic runs remain FAIL, with their individual passed window-control/New-Tab cases retained. Separate Escape artifacts verify actual held Shift/button release in both routes. Independent final-image normal restart and fresh tab-count acceptance are pending; no new UX performance benchmark is claimed.
# Historical Openbox UX revision

This document preserves the earlier image's work and evidence. The current frozen Xfce candidate, GTK3 terminal/Thunar, logout confirmation and guarded launch path are in [XFCE.md](XFCE.md). Earlier benchmark/QA results are not automatically assigned to that revision.

