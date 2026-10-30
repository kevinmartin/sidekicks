#!/usr/bin/env python3
"""Start/stop only the temporary Sidekicks LAN bridge. No host settings edits."""
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time
import urllib.request

HERE=Path(__file__).resolve().parent
ROOT=HERE.parent.parent
RUN=ROOT/'.run'/'lan'
ENTRY=HERE/'bridge.mjs'

def owned():
    if not (RUN/'pid').exists(): return None
    pid=int((RUN/'pid').read_text())
    r=subprocess.run(['ps','-p',str(pid),'-o','command='],capture_output=True,text=True)
    if r.stderr.strip(): raise RuntimeError(r.stderr.strip())
    return pid if str(ENTRY) in r.stdout else None

def main():
    action=sys.argv[1] if len(sys.argv)>1 else 'status'
    if action=='stop':
        pid=owned()
        if pid:
            os.kill(pid,signal.SIGTERM)
            for _ in range(40):
                if not owned(): break
                time.sleep(.1)
            else: raise RuntimeError('bridge is still stopping; no broad kill attempted')
        print('LAN bridge stopped; original loopback demos remain running.')
        return
    if action=='status':
        print(json.dumps({'pid':owned(),'config':json.loads((RUN/'config.json').read_text()) if (RUN/'config.json').exists() else None},indent=2));return
    if action!='start': raise RuntimeError('usage: python3 pocs/lan/manage.py start|stop|status')
    if owned(): raise RuntimeError('LAN bridge already running; inspect status rather than duplicate it')
    route=subprocess.run(['route','-n','get','default'],capture_output=True,text=True,check=True).stdout
    interface=re.search(r'interface:\s+(\S+)',route).group(1)
    address=subprocess.run(['ipconfig','getifaddr',interface],capture_output=True,text=True,check=True).stdout.strip()
    sys.path.insert(0,str(ROOT/'pocs'/'microvm'))
    import microvm
    info=microvm.rpc('nat',{'op':'info'})
    nat=info['network']['ipv4']
    if info['profile']!='neko-nat' or not re.fullmatch(r'192\.168\.64\.\d+',nat): raise RuntimeError('unexpected NAT profile/address')
    RUN.mkdir(parents=True,exist_ok=True)
    config={'bind':'0.0.0.0','advertise':address,'interface':interface,'natGuest':nat,'authentication':False,'tcp':[17901,17902,17903,17906,17907],'udp':[[59040,59059],[59140,59159],[59240,59259]],'pid':None}
    log_offset=(RUN/'bridge.log').stat().st_size if (RUN/'bridge.log').exists() else 0
    with (RUN/'bridge.log').open('ab',buffering=0) as log:
        process=subprocess.Popen([shutil.which('node') or 'node',str(ENTRY),address,nat],cwd=ROOT,stdout=log,stderr=log,start_new_session=True)
    (RUN/'pid').write_text(str(process.pid)+'\n');config['pid']=process.pid
    (RUN/'config.json').write_text(json.dumps(config,indent=2)+'\n')
    for _ in range(100):
        if process.poll() is not None: raise RuntimeError('LAN bridge exited; inspect .run/lan/bridge.log')
        try:
            with urllib.request.urlopen(f'http://{address}:17907/health',timeout=.3) as response:
                if response.status==200:
                    # Ready log is emitted only after all fixed UDP sockets have bound.
                    if b'"ready":true' in (RUN/'bridge.log').read_bytes()[log_offset:]:
                        print(json.dumps(config,indent=2));return
        except Exception: pass
        time.sleep(.1)
    os.kill(process.pid,signal.SIGTERM)
    raise RuntimeError('LAN bridge readiness timed out and was stopped')

if __name__=='__main__': main()
