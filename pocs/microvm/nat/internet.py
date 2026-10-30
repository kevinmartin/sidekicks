#!/usr/bin/env python3
"""Known benign public DNS/HTTPS only, run as guest uid1000 through vsock."""
import importlib.util,json,time
from pathlib import Path
HERE=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('vm',HERE/'microvm.py');vm=importlib.util.module_from_spec(spec);spec.loader.exec_module(vm)
code='''import json,socket,urllib.request,ssl,ipaddress,time
out=[]
allowed={'example.com','www.iana.org'}
class SafeRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,req,fp,code,msg,headers,newurl):
  from urllib.parse import urlsplit
  u=urlsplit(newurl)
  if u.scheme!='https' or u.hostname not in allowed:raise ValueError('unexpected redirect target')
  return super().redirect_request(req,fp,code,msg,headers,newurl)
opener=urllib.request.build_opener(SafeRedirect(),urllib.request.HTTPSHandler(context=ssl.create_default_context()))
for host in ['example.com','www.iana.org']:
 start=time.monotonic();ips=sorted({x[4][0] for x in socket.getaddrinfo(host,443,family=socket.AF_INET,type=socket.SOCK_STREAM)})
 assert ips and all(ipaddress.ip_address(x).is_global for x in ips)
 with opener.open('https://'+host+'/',timeout=10) as r:
  body=r.read(1048577);assert r.status==200 and len(body)<=1048576
  out.append({'host':host,'resolvedIPv4':ips,'status':r.status,'bytes':len(body),'tls':'default trust store, verification enabled','seconds':time.monotonic()-start})
print(json.dumps(out))
'''
report={'startedAt':time.time(),'network':vm.rpc('nat',{'op':'info'})['network']}
try:
 result=vm.rpc('nat',{'op':'exec','argv':['python3','-c',code],'timeout':28});report['guest']=result
 assert result['exitCode']==0,result['stderr'];report['requests']=json.loads(result['stdout']);report['status']='PASS'
except Exception as e:report.update(status='FAIL',error=str(e));raise
finally:(HERE/'artifacts/nat/internet.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
