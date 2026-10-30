#!/usr/bin/python3
"""Write readiness once the desktop and initial normal browser are mapped."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from window_clients import clients

processes = [int(pid) for pid in sys.argv[1:]]
for _ in range(180):
    for pid in processes:
        os.kill(pid, 0)
    mapped = list(clients())
    browser = next((c['id'] for c in mapped if c['class'] == 'sidekicksbrowser' and c['normal']), None)
    if browser and any(c['dock'] and 'Sidekicks Dock' in c['properties'] for c in mapped):
        state = {"ready": True, "theme": "sidekicks-sky", "homeUrl": "http://127.0.0.1:7792/home", "initialBrowserWindow": browser, "windowManager": "Openbox", "dock": "tint2", "compositor": "picom XRender", "meaning": "Initial desktop mapped; closing the browser does not revoke desktop readiness."}
        p = Path("/tmp/sidekicks-desktop-ready.json")
        p.with_suffix(".partial").write_text(json.dumps(state)+"\n")
        p.with_suffix(".partial").replace(p)
        raise SystemExit(0)
    time.sleep(0.2)
raise SystemExit("desktop did not map dock and initial normal browser within 36s")
