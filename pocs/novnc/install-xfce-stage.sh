#!/bin/sh
# Build-time staging setup; no live desktop hot-apply operation.
set -eu
sh /app/install-polish-config.sh
guest_home=/home/desktop
channels="$guest_home/.config/xfce4/xfconf/xfce-perchannel-xml"
python3 /app/polish-assets/xfce/generate.py --output /tmp/sidekicks-xfce-generated --home "$guest_home" \
 --browser-command 'python3 /app/desktop-app.py browser' \
 --terminal-command 'python3 /app/desktop-app.py terminal' \
 --files-command 'python3 /app/desktop-app.py files' \
 --wallpaper /app/desktop-assets/wallpaper.png --icons /app/desktop-assets/icons --terminal-wm-class Xfce4-terminal
cp -R /tmp/sidekicks-xfce-generated/user/. "$guest_home/"
cp -R /tmp/sidekicks-xfce-generated/system/. /etc/xdg/
# Browser association accepts validated URLs; visual launchers stay fixed.
install -m 0644 /app/sidekicks.desktop "$guest_home/.local/share/applications/sidekicks-browser.desktop"
update-desktop-database "$guest_home/.local/share/applications"
install -d -m 1777 /tmp/.ICE-unix
chown -R desktop:desktop "$guest_home/.config" "$guest_home/.local"
chmod +x /app/entrypoint-xfce.sh
