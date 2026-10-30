#!/usr/bin/env python3
"""Write guest readiness only after the native desktop and first browser map."""
import json
from pathlib import Path
import subprocess
import time
from importlib.util import spec_from_file_location, module_from_spec

spec=spec_from_file_location('desktop_app','/app/desktop-app.py')
apps=module_from_spec(spec);spec.loader.exec_module(apps)
target=Path('/tmp/sidekicks-desktop-ready.json')
deadline=time.monotonic()+35
while time.monotonic()<deadline:
    pids={name:apps.output('pgrep','-x',name).split() for name in ['openbox','tint2','picom']}
    wid=apps.find_window('browser')
    visible=wid and 'IsViewable' in apps.output('xwininfo','-id',wid)
    if visible and all(pids.values()):
        record={'ready':True,'theme':'sidekicks-sky','homeUrl':apps.HOME,
            'firstWindowId':wid,'processes':pids,'readyAt':time.time(),
            'desktop':{'width':1280,'height':720},'compositor':'picom-xrender'}
        temporary=target.with_suffix('.tmp');temporary.write_text(json.dumps(record));temporary.replace(target)
        break
    time.sleep(.15)
else:
    target.write_text(json.dumps({'ready':False,'theme':'sidekicks-sky','error':'desktop or initial browser did not map within35 seconds'}))
