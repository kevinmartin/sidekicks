# Sidekicks sky desktop UX

Live viewer: <http://127.0.0.1:7901/>. Adapter: <http://127.0.0.1:7791/health>. `./pocs/novnc/start.sh` waits for both API health and `GET /desktop/state` before reporting ready. The default guest page is `http://127.0.0.1:8080/home`; diagnostics remain separate at `/fixture` (legacy `/fixture/` also works).

The guest uses the integration-owned original assets copied from `shared/desktop/`: cyan/blue gradient wallpaper, a light Sidekicks Home and Browser/Terminal/Files icons. Openbox manages real native decorated windows. Chromium starts at a normal size with visible desktop margins; there is no kiosk or startup-maximize flag. Picom's Xrender compositor provides actual32-bit dock transparency; tint2 supplies the rounded three-icon floating dock. PCManFM opens the disposable `/home/desktop/Sidekicks` folder. No host home is mounted.

## Controls

- Native titlebar minimize, maximize/restore and close buttons work. Drag a titlebar to move, an edge/corner to resize, or use Alt+F7 / Alt+F8. Alt+Tab changes native window focus.
- Dock buttons restore an existing minimized normal client at its previous geometry; they reopen a closed application only when explicitly clicked. Helpers use per-app file locks, `_NET_CLIENT_LIST` plus WM_CLASS/type/geometry filtering, and `wmctrl -ia` only for activation. Missing EWMH type is treated as normal for legacy xterm; explicit utility/dialog clients are excluded. No hidden utility window is force-mapped.
- The viewer's **Open apps** and **Window** menus send lease-gated guest shortcuts, so window controls remain usable when the desktop is scaled down. **Fit desktop** switches to actual pixel size and back.
- F11 toggles actual Chromium fullscreen. Alt+F12 toggles native window fullscreen. **Viewer fullscreen** uses the browser DOM Fullscreen API and retains the viewer controls; the Exit button or Escape reverses it. Escape is intercepted only for viewer fullscreen, then reaches the guest normally outside that mode.
- Fullscreen exits release held guest keys/buttons. Cleanup resets client pointer state and queries actual X11 button state in addition to tracked RFB/XTest state; this covers mixed input transports. The existing lease, takeover, blur/disconnect and paced-input protections remain active.
- Chromium's local ManifestV3 new-tab override has no requested permissions and redirects a normal new tab to the same guest `/home`. This is tested on the packaged unbranded Chromium; it is not a claim about another engine or a branded Chrome installation's extension policies. Mac key remapping remains as documented in the original report; physical Mac keyboard/trackpad input is untested.

## Home launch boundary and readiness

`GET /home` injects a per-boot capability only for exact source `127.0.0.1`, exact Host `127.0.0.1:8080`, and absent or matching guest Origin. `POST /desktop/launch` additionally requires exact Origin `http://127.0.0.1:8080`, `X-Sidekicks-Desktop`, an active owner and an allowlisted app name. It invokes only fixed guest launcher scripts with no arbitrary arguments or shell. Published Docker requests cannot forge internal identity, and source `127.0.0.2` is rejected even with internal Host/Origin and a real capability. No token is stored in evidence.

`/desktop/state` reads an atomic guest marker written only after Openbox, picom, tint2 and the first normal visible Chromium client exist. It returns503 while starting,200 when ready. Browser close does not make a ready desktop unready and does not trigger automatic browser reopening. `/health` remains API/display readiness.

## Executed checks and evidence

The isolated stage used loopback17791/17901, the same2CPU/2GiB/no-mount/cap-drop/no-new-privileges constraints, and the exact image later applied live. `node pocs/novnc/tests/ux.mjs` passed20 checks: rendered viewer, normal-window margins, private launch/CSRF boundary, native minimize/maximize, same-window dock restoration, Home Terminal/Files, close/reopen, Alt+Tab, guest/viewer fullscreen, actual titlebar drag/resize and helper controls at an860×760 viewer, and normal New Tab→Home. New-tab screenshots were visually checked for actual Home content/tabs. X11 IDs can be reused after complete client close; reopen proof uses the observed closed interval and one reopened normal client, not globally unique resource IDs.

`node pocs/novnc/tests/ux-escape.mjs` passed Escape and Exit-button paths with actual `XQueryKeymap`/`XQueryPointer`: held Shift+left-button mask257 became0; pointer movement after exit stayed0, and guest Escape outside viewer fullscreen was forwarded. The original fullscreen failure and mixed RFB/API bookkeeping failure are preserved as `artifacts/novnc/ux/escape-before.json` and `escape-mixed-before.json`. Earlier stage attempts remain under `attempt-1/` and `ux-before-pointer-fix.json`.

`node pocs/novnc/tests/ux-live-smoke.mjs` passed on the live ports without sending input: ready desktop, decoded1280×720 stream, no owner and actual held mask0. Evidence is under `artifacts/novnc/ux/`: `ux.json`, `escape.json`, `live-smoke.json`, native-window screenshots, `live-viewer.png`, `live-desktop.png`, exact container/image inspections and `runtime-files.sha256`. Thirteen running functional source files match the workspace. Independent QA is queued after Neko; no input/runtime activity will occur from this worker during QA.

## Frozen image and rollback

- Tested/live image: `sha256:01acf649de1dcb169ae4dcc4ddd42d70f1b044fb0fada48246496260117d3001`, tagged `sidekicks-novnc:cloud-ux` and `sidekicks-novnc:local`.
- Live container: `2de0e97264b8590f5fa5395f782daca61e4df7cdd72533b4b8af9592d57875ec` (`sidekicks-novnc`).
- Shared Home SHA256: `45899f0abc84afd59fec79aa7d3b149202965f32a1052f87dcad33002b1cc14a`.
- Old container `sidekicks-novnc-pre-cloud-ux` is preserved stopped, with old image `sha256:64c4e5e66e745ee817c0baecf37e8f9084f4ba30c3d988f5c5089fb351c87b0c` tagged `sidekicks-novnc:pre-cloud-ux`. Pre-update inspections/screenshot are in `artifacts/novnc/ux-before/`.
- The task UX stage is stopped. No unrelated containers, host OS/security/network settings or user browser profiles were changed.

Coordinate before rollback so the live container can be stopped/renamed, the preserved old container renamed back to `sidekicks-novnc` and started, and the old image retagged `:local`. Do not delete the new or old container as part of rollback; retain evidence.
