#!/usr/bin/python3
"""Readiness for the configured Xfce session, independent of the input fixture."""
import json
import os
from pathlib import Path
import sys
import time
from window_clients import clients

required = {'xfce4-session', 'xfwm4', 'xfce4-panel', 'xfdesktop', 'xfsettingsd', 'dbus-daemon', 'Thunar'}
for _ in range(300):
    os.kill(int(sys.argv[1]), 0)
    processes = {}
    for p in Path('/proc').glob('[0-9]*/comm'):
        try:
            if p.stat().st_uid != os.getuid():
                continue
            processes.setdefault(p.read_text().strip(), []).append(int(p.parent.name))
        except (OSError, ValueError):
            continue
    mapped = list(clients())
    browser = next((c['id'] for c in mapped if c['class'] == 'sidekicksbrowser' and c['normal']), None)
    panel = next((c['id'] for c in mapped if c['dock'] and c['class'] == 'xfce4-panel'), None)
    if browser and panel and required <= processes.keys() and not ({'openbox', 'tint2', 'picom'} & processes.keys()):
        state = {'ready': True, 'theme': 'sidekicks-sky', 'desktop': 'Xfce X11',
                 'homeUrl': 'http://127.0.0.1:7792/home', 'windowManager': 'Xfwm4',
                 'dock': 'xfce4-panel', 'compositor': 'Xfwm4 XRender',
                 'primaryTerminal': 'xfce4-terminal GTK3/VTE', 'files': 'Thunar GTK3',
                 'initialBrowserWindow': browser, 'panelWindow': panel,
                 'sessionBus': os.environ['DBUS_SESSION_BUS_ADDRESS'],
                 'processes': {name: processes[name] for name in sorted(required)},
                 'meaning': 'Initial Xfce session, panel and normal browser mapped; later browser closure does not revoke desktop readiness.'}
        p = Path('/tmp/sidekicks-desktop-ready.json')
        p.with_suffix('.partial').write_text(json.dumps(state)+'\n')
        p.with_suffix('.partial').replace(p)
        raise SystemExit(0)
    time.sleep(0.2)
raise SystemExit('Xfce session did not map expected components and browser within 60s')
