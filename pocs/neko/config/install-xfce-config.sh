#!/bin/sh
set -eu
python3 /etc/sidekicks/polish/xfce/generate.py --output /tmp/sidekicks-xfce-config --home /home/neko --browser-command /etc/sidekicks/browser.sh --terminal-command /etc/sidekicks/terminal.sh --files-command /etc/sidekicks/files.sh --wallpaper /etc/sidekicks/wallpaper.png --icons /etc/sidekicks --monitor DUMMY0
cp -R /tmp/sidekicks-xfce-config/user/. /home/neko/
cp -R /tmp/sidekicks-xfce-config/system/. /etc/xdg/
rm -f /home/neko/.config/autostart/sidekicks-panel-context.desktop
# Supervisor owns the virtual PulseAudio server; never launch a second server.
mkdir -p /home/neko/.config/autostart
printf '%s\n' '[Desktop Entry]' 'Type=Application' 'Name=PulseAudio (managed by Sidekicks)' 'Hidden=true' > /home/neko/.config/autostart/pulseaudio.desktop
update-desktop-database /home/neko/.local/share/applications
chown -R neko:neko /home/neko
rm -rf /tmp/sidekicks-xfce-config
