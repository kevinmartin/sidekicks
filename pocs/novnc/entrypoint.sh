#!/bin/sh
set -eu
if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]; then exec dbus-run-session -- "$0"; fi
rm -f /tmp/.X99-lock /tmp/.X11-unix/X99 /tmp/sidekicks-desktop-ready.json
# Browser profile data is retained. A restart still ends running processes; it
# is never a preservation mechanism for live comparison windows or terminal jobs.
Xvfb :99 -screen 0 1280x720x24 -nolisten tcp -ac > /tmp/xvfb.log 2>&1 &
for i in $(seq 1 100); do test -S /tmp/.X11-unix/X99 && break; sleep .1; done
openbox > /tmp/openbox.log 2>&1 &
xsetroot -cursor_name left_ptr
xrdb -merge /app/polish-assets/Xresources
picom --config /app/picom.conf --daemon > /tmp/picom.log 2>&1
if [ -f /app/desktop-assets/wallpaper.png ]; then
  feh --bg-fill /app/desktop-assets/wallpaper.png
else
  python3 /app/wallpaper.py
  feh --bg-fill /tmp/sidekicks-wallpaper.png
fi
tint2 > /tmp/tint2.log 2>&1 &
/usr/local/bin/x11vnc -display :99 -rfbport 5900 -localhost -rfbversion 3.8 -forever -shared -nopw -noxdamage -nowireframe -wait 10 -defer 5 -o /tmp/x11vnc.log &
python3 /app/server.py &
gateway=$!
trap 'kill "$gateway" 2>/dev/null || true; exit 0' TERM INT
sleep 1
python3 /app/desktop-app.py browser > /tmp/sidekicks-browser-start.json
python3 /app/desktop-ready.py > /tmp/sidekicks-desktop-ready.log 2>&1 &
wait "$gateway"
