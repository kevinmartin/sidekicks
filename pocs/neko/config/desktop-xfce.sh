#!/bin/sh
set -eu
rm -f /tmp/sidekicks-desktop-ready.json
until xdotool getdisplaygeometry >/dev/null 2>&1; do sleep 0.2; done
until dbus-send --session --type=method_call --print-reply --dest=org.freedesktop.DBus / org.freedesktop.DBus.ListNames >/dev/null 2>&1; do sleep 0.2; done
xrdb -merge /etc/sidekicks/Xresources
# Stage-owned disposable profile. Production desktops are never attached here.
PROFILE="$(mktemp -d /tmp/sidekicks-firefox.XXXXXX)"
printf '%s\n' "$PROFILE" > /tmp/sidekicks-firefox-profile-path
cat > "$PROFILE/user.js" <<'PREFS'
user_pref("browser.tabs.inTitlebar", 0);
user_pref("browser.sessionstore.resume_from_crash", false);
user_pref("browser.shell.checkDefaultBrowser", false);
PREFS
xfce4-session &
session=$!
trap 'kill "$session" 2>/dev/null || true' EXIT INT TERM
until xprop -root _NET_SUPPORTING_WM_CHECK 2>/dev/null | grep -q '0x'; do
  kill -0 "$session"
  sleep 0.2
done
until curl -fsS http://127.0.0.1:7792/health >/dev/null 2>&1; do sleep 0.2; done
/etc/sidekicks/browser.sh &
/usr/bin/python3 /etc/sidekicks/desktop-xfce-ready.py "$session" &
wait "$session"
