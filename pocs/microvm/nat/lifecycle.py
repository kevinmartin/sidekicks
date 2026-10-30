#!/usr/bin/env python3
"""NAT-only restart smoke and protected-state evidence."""
from pathlib import Path
import importlib.util,subprocess,json,time,urllib.request
HERE=Path(__file__).resolve().parents[1];OUT=HERE/'artifacts/nat'
s=importlib.util.spec_from_file_location('vm',HERE/'microvm.py');v=importlib.util.module_from_spec(s);s.loader.exec_module(v)
def protected():
 ds=json.loads(subprocess.check_output(['docker','inspect','sidekicks-neko','sidekicks-novnc','sidekicks-workbench']))
 return {'guests':{n:v.rpc(n,{'op':'info'})['bootId'] for n in ('a','neko')},'containers':{x['Name']:{'id':x['Id'],'image':x['Image'],'start':x['State']['StartedAt'],'ports':x['NetworkSettings']['Ports']} for x in ds}}
r={'startedAt':time.time(),'checks':[]}
try:
 before=protected();old=v.rpc('nat',{'op':'info'});stop=v.stop('nat');assert stop['filesystemFlushed'];r['checks'].append({'name':'NAT-only normal stop','status':'PASS','stop':stop})
 start=v.start('nat');new=v.rpc('nat',{'op':'info'});assert old['bootId']!=new['bootId'] and new['network']['ipv4'];r['checks'].append({'name':'repeatable DHCP/routes/DNS/config on new kernel boot','status':'PASS','before':old['network'],'after':new['network'],'start':start})
 for file in ('smoke.mjs','internet.py'):
  p=subprocess.run(['node' if file.endswith('.mjs') else 'python3',str(HERE/'nat'/file)],capture_output=True,text=True);assert p.returncode==0,p.stdout+p.stderr
  r['checks'].append({'name':file+' after restart','status':'PASS'})
 assert protected()==before;r['checks'].append({'name':'accepted Docker and A/relay guest state unchanged','status':'PASS','protected':before});r['status']='PASS'
except Exception as e:
 import traceback
 r['status']='FAIL';r['error']=traceback.format_exc();print(r['error'])
finally:
 r['finishedAt']=time.time();(OUT/'lifecycle.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps({'status':r['status'],'checks':len(r['checks'])}))
 if r['status']!='PASS':raise SystemExit(1)
