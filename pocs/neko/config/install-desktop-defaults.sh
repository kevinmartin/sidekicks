#!/bin/sh
# Image-build setup only; never modifies the host or an existing desktop.
set -eu
mkdir -p /home/neko/.config/xfce4/xfconf/xfce-perchannel-xml /home/neko/.local/share/applications /home/neko/.config/gtk-3.0 /home/neko/.local/share/icons/Sidekicks /home/neko/Sidekicks
cp /etc/sidekicks/mimeapps.list /home/neko/.config/mimeapps.list
cp /etc/sidekicks/thunar.xml /home/neko/.config/xfce4/xfconf/xfce-perchannel-xml/thunar.xml
cp /etc/sidekicks/polish/settings.ini /etc/sidekicks/polish/gtk.css /home/neko/.config/gtk-3.0/
cp -R /etc/sidekicks/polish/icons/. /home/neko/.local/share/icons/Sidekicks/
printf '%s\n' 'file:///home/neko/Sidekicks Sidekicks' > /home/neko/.config/gtk-3.0/bookmarks
printf '%s\n' 'Sidekicks disposable desktop workspace.' 'Files created here stay inside this guest; no host folders are mounted.' > /home/neko/Sidekicks/README.txt
cp /etc/sidekicks/*.desktop /home/neko/.local/share/applications/
update-desktop-database /home/neko/.local/share/applications
desktop-file-validate /home/neko/.local/share/applications/*.desktop
chown -R neko:neko /home/neko/.config /home/neko/.local /home/neko/Sidekicks
