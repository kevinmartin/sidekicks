# Desktop polish asset contract

Integration owns this directory; backend builders copy these assets read-only into their own build contexts.

The cloud reference uses custom GTK3 OrbitOS based on Adwaita, an Adwaita-inheriting icon overlay, Xfwm4 and a custom dock. Its artwork/font/theme bundle has no established reuse grant. These files independently implement Sidekicks styling with the installed Adwaita GTK3 theme/icons, existing DejaVu fonts, original cyan folder artwork and small original GTK CSS accents. No proprietary reference assets are copied.

Candidate desktop target: coherent official Debian Xfce core/session with **Xfwm4, xfce4-panel, xfdesktop, xfsettingsd, Thunar and xfce4-terminal**, preserving noVNC/Neko transports and current OS/kernel. Replace the Openbox/tint2/picom runtime rather than running duplicate WMs, panels or compositors. Existing components may remain installed for comparison, but are not the supported launch path. Record actual package/image size and runtime observations; do not infer performance from toolkit version.

Install `settings.ini` and `gtk.css` in each guest user's `.config/gtk-3.0/`; install `icons/` as `.local/share/icons/Sidekicks/`. Inherit missing icons from installed Adwaita/hicolor. GTK CSS must be checked against both installed GTK3 versions; remove only unsupported rules if diagnostics identify them.

Primary terminal candidate: GTK3/VTE **xfce4-terminal**, stable `SidekicksTerminal` class if supported by the installed CLI, DejaVu Sans Mono11,88x24, navy `#10263d` / text `#e8f7ff`. Retain `Xresources` compatibility for manually launched legacy XTerm/UXTerm instances; merge with `xrdb` on desktop start where installed. These resources do not retroactively recolor existing terminals, and must not be used as a reason to kill Kevin's comparison processes. uxterm is an xterm wrapper that selects UTF-8 and a different resource class, not a different terminal engine.

Desktop context-menu exposure may be disabled or curated to fixed Browser, Terminal and Files helpers shared by Home/dock/shortcuts. Remove settings/restart/logout/obsolete launch entries, constrain panel customization menus, and keep useful native window controls and in-application file/browser context menus. Menu curation is not a security sandbox.

Chromium should request its embedded frame, retain normal window management and Sidekicks startup/Home/new-tab behavior across provided launch paths. Restoring an existing window must preserve its tabs. Avoid a blanket undecorate rule or editing an active Chromium Preferences file behind its back. Firefox can remain in Neko with the same Home and working native controls unless a bounded CSD trial succeeds.

Preservation: Docker7901 currently holds Kevin's two browsers and three terminals. Do not restart it or run clean-desktop tests against it. Stage independently first; any live hot-apply must preserve original process/window identities, profiles, tabs and jobs as far as possible, and separately report what could not be preserved. A saved screenshot/container image does not preserve live terminal jobs or scrollback.
