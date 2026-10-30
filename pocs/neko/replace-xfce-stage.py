#!/usr/bin/env python3
"""Preserve a worker-owned stage attempt, then start the next local stage image."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys

root=Path(__file__).resolve().parent
name='sidekicks-neko-xfce-stage'
image=os.environ['SIDEKICKS_NEKO_XFCE_IMAGE']
assert image.startswith('sidekicks-neko:polish-xfce-stage-')
subprocess.run(['docker','image','inspect',image],check=True,stdout=subprocess.DEVNULL)
info=json.loads(subprocess.check_output(['docker','inspect',name]))[0]
assert info['Config']['Labels']['component']=='neko-xfce-stage'
assert not info['HostConfig']['Privileged'] and info['Mounts']==[]
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H-%M-%SZ')
out=root/'test/artifacts/xfce-stage'/('preserved-'+stamp)
out.mkdir(parents=True)
record={'containerId':info['Id'],'image':info['Image'],'oldName':name,'bindings':info['HostConfig']['PortBindings']}
for logfile in ['desktop.log','session-bus.log']:
    result=subprocess.run(['docker','exec','--user','neko',name,'cat','/var/log/neko/'+logfile],capture_output=True)
    (out/logfile).write_bytes(result.stdout)
subprocess.run(['docker','stop','-t','10',name],check=True)
preserved=name+'-preserved-'+stamp.lower()
subprocess.run(['docker','rename',name,preserved],check=True)
record['preservedAs']=preserved
(out/'attempt.json').write_text(json.dumps(record,indent=2)+'\n')
subprocess.run([sys.executable,str(root/'stage-xfce.py'),'--reuse-proxy'],check=True)
