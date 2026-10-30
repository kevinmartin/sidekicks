#!/bin/sh
set -eu
rm -f /tmp/sidekicks-desktop-ready.json
until xdotool getdisplaygeometry >/dev/null 2>&1; do sleep 0.2; done
# Shared resources apply only to future terminals in this new guest session.
xrdb -merge /etc/sidekicks/Xresources
openbox &
wm=$!
picom --config /etc/sidekicks/picom.conf &
compositor=$!
tint2 &
panel=$!
trap 'kill "$panel" "$compositor" "$wm" 2>/dev/null || true' EXIT INT TERM
until curl -fsS http://127.0.0.1:7792/health >/dev/null 2>&1; do sleep 0.2; done
feh --bg-fill /etc/sidekicks/wallpaper.png
# New synthetic browser profile on every desktop boot; no old tabs or crash restore.
PROFILE="$(mktemp -d /tmp/sidekicks-firefox.XXXXXX)"
printf '%s\n' "$PROFILE" > /tmp/sidekicks-firefox-profile-path
cat > "$PROFILE/user.js" <<'PREFS'
user_pref("browser.tabs.inTitlebar", 0);
user_pref("browser.sessionstore.resume_from_crash", false);
user_pref("browser.shell.checkDefaultBrowser", false);
PREFS
/etc/sidekicks/browser.sh &
/usr/bin/python3 /etc/sidekicks/desktop-ready.py "$wm" "$panel" "$compositor" &
# Closing the last browser window must leave a usable desktop and dock.
wait "$wm"
