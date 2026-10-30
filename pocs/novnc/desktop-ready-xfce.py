"""Readiness for the isolated whole-Xfce evaluation variant."""
import json,time
from pathlib import Path
from importlib.util import spec_from_file_location,module_from_spec
spec=spec_from_file_location('desktop_app','/app/desktop-app.py')
apps=module_from_spec(spec);spec.loader.exec_module(apps)
target=Path('/tmp/sidekicks-desktop-ready.json')
for _ in range(350):
    pids={name:apps.output('pgrep','-x',name).split() for name in ['xfwm4','xfsettingsd','xfce4-panel','xfdesktop','xfce4-session']}
    wid=apps.find_window('browser')
    if wid and 'IsViewable' in apps.output('xwininfo','-id',wid) and all(pids.values()):
        data={'ready':True,'theme':'sidekicks-sky','stack':'xfce','homeUrl':apps.HOME,'firstWindowId':wid,'processes':pids,'readyAt':time.time(),'desktop':{'width':1280,'height':720},'compositor':'xfwm4'}
        temporary=target.with_suffix('.tmp');temporary.write_text(json.dumps(data));temporary.replace(target)
        break
    time.sleep(.1)
else:
    target.write_text(json.dumps({'ready':False,'stack':'xfce','error':'Xfce session or managed browser not ready'}))
