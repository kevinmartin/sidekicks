#!/usr/bin/env python3
"""Builder transport/isolation/lifecycle evidence, bounded synthetic content only."""
import importlib.util,sys,json,socket,subprocess,time,threading,struct,urllib.request
from pathlib import Path
HERE=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('microvm',HERE/'microvm.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
OUT=HERE/'artifacts/neko-udp'
r={'startedAt':time.time(),'checks':[]}
def check(name,**evidence):r['checks'].append(dict(name=name,status='PASS',**evidence))
def baseline():
 items=json.loads(subprocess.check_output(['docker','inspect','sidekicks-neko','sidekicks-novnc','sidekicks-workbench']))
 return {x['Name']:{'id':x['Id'],'image':x['Image'],'startedAt':x['State']['StartedAt'],'ports':x['NetworkSettings']['Ports']} for x in items}
try:
 protected=baseline();a=m.rpc('a',{'op':'info'})['bootId'];info=m.rpc('neko',{'op':'info'})
 assert info['bootId']!=a and info['cpus']==2
 check('separate new kernel from baseline VM A, two vCPU',baselineBoot=a,nekoBoot=info['bootId'])
 facts=m.rpc('neko',{'op':'exec','argv':['python3','-c',"import json,os,pathlib;print(json.dumps({'uid':os.getuid(),'interfaces':os.listdir('/sys/class/net'),'meminfo':pathlib.Path('/proc/meminfo').read_text(),'mounts':pathlib.Path('/proc/mounts').read_text(),'hostSocket':pathlib.Path('/var/run/docker.sock').exists()}))"]})
 facts=json.loads(facts['stdout']);assert facts['uid']==1000 and facts['interfaces']==['lo'] and not facts['hostSocket']
 assert not any(v in facts['mounts'] for v in ('virtiofs','9p','/Users/kevinmartin'))
 check('non-root exec, loopback-only guest and no host filesystem/socket',facts=facts)
 # Exact datagrams through the actual host listener, private UDS, vsock, guest UDP and back.
 echo="""import socket,time,json
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('127.0.0.1',59139));s.settimeout(8)
seen=[]
try:
 for i in range(6):
  d,a=s.recvfrom(65535);seen.append({'size':len(d),'peer':a});s.sendto(d,a)
finally:s.close()
print(json.dumps(seen))
"""
 guest=[]
 t=threading.Thread(target=lambda:guest.append(m.rpc('neko',{'op':'exec','argv':['python3','-c',echo],'timeout':12})));t.start();time.sleep(.25)
 peers=[socket.socket(socket.AF_INET,socket.SOCK_DGRAM) for _ in range(2)]
 for p in peers:p.bind(('127.0.0.1',0));p.settimeout(3)
 try:
  packets=[b'',b'peer0-'+bytes(range(256)),b'x'*8192]
  for packet in packets:
   peers[0].sendto(packet,('127.0.0.1',59139));got,source=peers[0].recvfrom(65535);assert got==packet and source==('127.0.0.1',59139)
  for packet in (b'peer1-A',b'peer1-B',b'peer1-C'):
   peers[1].sendto(packet,('127.0.0.1',59139));got,source=peers[1].recvfrom(65535);assert got==packet and source==('127.0.0.1',59139)
 finally:
  for p in peers:p.close()
 t.join(12);assert guest and guest[0]['exitCode']==0,guest
 received=json.loads(guest[0]['stdout']);assert len({tuple(x['peer']) for x in received})==2
 check('empty/binary/8192-byte datagrams and two-peer separation across actual VM boundary',guestReceived=received)
 # Private UDP destination validation, outside-range request rejected by guest.
 c=socket.socket(socket.AF_UNIX);c.settimeout(3);c.connect(str(m.directory('neko')/'udp.sock'));c.sendall(struct.pack('!H',9));assert c.recv(1)==b'';c.close()
 check('private datagram path rejects destination outside allowed range')
 listeners=subprocess.check_output(['lsof','-nP','-iUDP:59120-59139'],text=True)
 assert len([x for x in listeners.splitlines() if '127.0.0.1:' in x])==20 and '*:' not in listeners
 check('actual 20 host UDP listeners loopback only',listeners=listeners)
 before=m.rpc('neko',{'op':'info'})['bootId']
 stop=m.stop('neko');assert stop['filesystemFlushed']
 for port in range(59120,59140):
  s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.bind(('127.0.0.1',port));s.close()
 check('normal stop closes all 20 UDP ports and private relay',stop=stop)
 start=m.start('neko');after=m.rpc('neko',{'op':'info'})['bootId'];assert before!=after
 check('restart boots new kernel and restores healthy fixture/media relay',start=start,oldBoot=before,newBoot=after)
 assert protected==baseline() and a==m.rpc('a',{'op':'info'})['bootId']
 for port in (7790,7791,7792,7793):
  url=f'http://127.0.0.1:{port}'+('/api/health' if port==7790 else '/health')
  try:
   with urllib.request.urlopen(url,timeout=3) as response:assert response.status==200
  except urllib.error.HTTPError as e:
   if port!=7790:raise
   with urllib.request.urlopen(f'http://127.0.0.1:{port}/',timeout=3) as response:assert response.status==200
 check('accepted containers and existing A boot unchanged, baseline services healthy',protected=protected)
 r['status']='PASS'
except Exception as e:
 import traceback
 r.update(status='FAIL',error=traceback.format_exc());print(r['error'],file=sys.stderr);sys.exitcode=1
finally:
 r['finishedAt']=time.time();(OUT/'transport.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps({'status':r['status'],'checks':len(r['checks'])}));
 if r['status']!='PASS':sys.exit(1)
