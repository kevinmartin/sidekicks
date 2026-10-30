#!/bin/sh
# Image-build setup only. Do not run against Kevin's active comparison desktop.
set -eu
guest_home=/home/desktop
install -d "$guest_home/.config/gtk-3.0" "$guest_home/.local/share/icons" "$guest_home/.local/share/applications" "$guest_home/.config/xfce4/xfconf/xfce-perchannel-xml"
install -m 0644 /app/polish-assets/settings.ini "$guest_home/.config/gtk-3.0/settings.ini"
install -m 0644 /app/polish-assets/gtk.css "$guest_home/.config/gtk-3.0/gtk.css"
cp -R /app/polish-assets/icons "$guest_home/.local/share/icons/Sidekicks"
install -m 0644 /app/sidekicks.desktop "$guest_home/.local/share/applications/sidekicks-browser.desktop"
install -m 0644 /app/terminal.desktop "$guest_home/.local/share/applications/sidekicks-terminal.desktop"
install -m 0644 /app/files.desktop "$guest_home/.local/share/applications/sidekicks-files.desktop"
install -m 0644 /app/mimeapps.list "$guest_home/.config/mimeapps.list"
install -m 0644 /app/thunar.xml "$guest_home/.config/xfce4/xfconf/xfce-perchannel-xml/thunar.xml"
printf '%s\n' 'file:///home/desktop/Sidekicks Sidekicks' > "$guest_home/.config/gtk-3.0/bookmarks"
desktop-file-validate "$guest_home/.local/share/applications/sidekicks-browser.desktop"
update-desktop-database "$guest_home/.local/share/applications"
gtk-update-icon-cache -f -t "$guest_home/.local/share/icons/Sidekicks"
chown -R desktop:desktop "$guest_home/.config" "$guest_home/.local"
