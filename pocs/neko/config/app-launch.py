#!/usr/bin/python3
"""Launch or restore one normal Sidekicks app window, preserving its geometry."""
import fcntl
import os
from pathlib import Path
import subprocess
import sys
import time
from window_clients import clients

app = sys.argv[1] if len(sys.argv) == 2 else ""
classes = {"browser": "sidekicksbrowser", "terminal": "sidekicksterminal", "files": "thunar"}
if app not in classes:
    raise SystemExit("unknown desktop app")


def windows():
    for client in clients():
        if client['class'] == classes[app] and client['normal']:
            yield client['id']


with open(f"/tmp/sidekicks-launch-{app}.lock", "w") as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        # A launch is already in progress; never queue a later duplicate launch.
        raise SystemExit(0)
    existing = next(windows(), None)
    if existing:
        # EWMH activation restores the same minimized window. Do not remap every
        # process window, which also maps dialogs and Firefox utility windows.
        subprocess.run(["wmctrl", "-ia", existing], check=True)
        raise SystemExit(0)
    if app == "browser":
        profile = Path("/tmp/sidekicks-firefox-profile-path").read_text().strip()
        command = ["firefox-esr", "--profile", profile, "--class", "SidekicksBrowser", "--width", "1120", "--height", "552", "--new-window", "http://127.0.0.1:7792/home"]
    elif app == "terminal":
        # A separate VTE process keeps the documented class and stable identity.
        # Legacy xterm/uxterm remain installed and receive shared Xresources.
        command = ["xfce4-terminal", "--disable-server", "--class=SidekicksTerminal", "--title=Sidekicks Terminal", "--geometry=88x24", "--font=DejaVu Sans Mono 11", "--color-bg=#10263d", "--color-text=#e8f7ff", "--show-borders", "--hide-toolbar", "--hide-scrollbar", "--working-directory=/home/neko/Sidekicks", "--execute", "/bin/bash", "--noprofile", "--norc"]
    else:
        # Thunar is a normal app, never the desktop manager or a full Xfce session.
        command = ["thunar", "/home/neko/Sidekicks"]
    environment = os.environ.copy()
    if app == "terminal":
        environment.update(LANG="C.UTF-8", LC_ALL="C.UTF-8")
    with open(f"/tmp/sidekicks-app-{app}.log", "ab", buffering=0) as log:
        subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True, cwd="/home/neko", env=environment)
    # Hold the per-app lock until the normal client appears. Concurrent dock and
    # home clicks then restore that window rather than spawning duplicates.
    for _ in range(100):
        mapped = next(windows(), None)
        if mapped:
            if app == "browser":
                # Initial placement only; activating an existing client above
                # must preserve its previous position, size and tabs.
                subprocess.run(["wmctrl", "-ir", mapped, "-e", "0,80,56,1120,552"], check=True)
            raise SystemExit(0)
        time.sleep(0.1)
    raise SystemExit("application did not create a normal window within 10s")
