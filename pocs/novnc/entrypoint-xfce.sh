#!/bin/sh
# Separate full-session evaluation variant. Never hot-apply to live7901.
set -eu
if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]; then exec dbus-run-session -- "$0"; fi
export XDG_CURRENT_DESKTOP=XFCE XDG_SESSION_DESKTOP=xfce DESKTOP_SESSION=xfce
export SIDEKICKS_DESKTOP_STACK=xfce
rm -f /tmp/.X99-lock /tmp/.X11-unix/X99 /tmp/sidekicks-desktop-ready.json
Xvfb :99 -screen 0 1280x720x24 -nolisten tcp -ac > /tmp/xvfb.log 2>&1 &
for i in $(seq 1 100); do test -S /tmp/.X11-unix/X99 && break; sleep .1; done
xsetroot -cursor_name left_ptr
xrdb -merge /app/polish-assets/Xresources
xfce4-session > /tmp/xfce-session.log 2>&1 &
/usr/local/bin/x11vnc -display :99 -rfbport 5900 -localhost -rfbversion 3.8 -forever -shared -nopw -noxdamage -nowireframe -wait 10 -defer 5 -o /tmp/x11vnc.log &
python3 /app/server.py &
gateway=$!
trap 'kill "$gateway" 2>/dev/null || true; exit 0' TERM INT
for i in $(seq 1 100); do pgrep -x xfwm4 >/dev/null && pgrep -x xfdesktop >/dev/null && break; sleep .1; done
python3 /app/desktop-app.py browser > /tmp/sidekicks-browser-start.json
python3 /app/desktop-ready-xfce.py > /tmp/sidekicks-desktop-ready.log 2>&1 &
wait "$gateway"
