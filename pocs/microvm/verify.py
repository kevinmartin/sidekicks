#!/usr/bin/env python3
"""Destructive only to task-owned microvm sessions a/b and named baseline containers."""
import base64
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request
import microvm as vm

OUT = vm.HERE / 'artifacts'
report = {'startedAt': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'checks': [], 'timings': {}, 'limitations': ['direct VZ runner; Apple container CLI unsupported on this macOS15.3 host', 'no NIC, no egress, no UDP or Neko VM test', 'no memory-exhaustion, CPU-saturation or adversarial escape test', 'cached OCI image and host caches; not factory-cold boot', 'stop syncs guest data then powers off; not application save or suspend', 'unsynced recent writes were observed lost after SIGKILL; write acknowledgment is not a durability fence']}

def save(): (OUT / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
def check(name, condition, **evidence):
    report['checks'].append({'name': name, 'status': 'PASS' if condition else 'FAIL', **evidence}); save()
    if not condition: raise AssertionError(name)
def command(*argv): return subprocess.check_output(argv, text=True).strip()
def docker(*args): return command('docker', *args)
def get(url):
    with urllib.request.urlopen(url, timeout=4) as response: return json.load(response)
def guest(name, *argv): return vm.rpc(name, {'op':'exec','argv':list(argv)})
def source_state():
    return json.loads(docker('inspect', 'sidekicks-novnc','sidekicks-neko','sidekicks-workbench'))
def minimal_state(states):
    return {s['Name']:{'id':s['Id'],'image':s['Image'],'startedAt':s['State']['StartedAt'],'restartCount':s['RestartCount'],'ports':s['NetworkSettings']['Ports']} for s in states}

try:
    before = minimal_state(source_state()); report['protectedBefore'] = before
    report['host'] = {'os':command('sw_vers'), 'hardware':command('system_profiler','SPHardwareDataType','-detailLevel','mini'), 'disk':command('df','-h',str(vm.HERE)), 'virtualizationProbe':json.loads(command(str(vm.BUILD/'probe')))}
    # Capture current preliminary guests before replacing them with the final template.
    import shutil
    report['preliminaryGuests'] = {}
    for name in ('a','b'):
        if vm.directory(name).exists():
            if vm.active(name): report['preliminaryGuests'][name] = vm.rpc(name, {'op':'info'}, timeout=2)
            vm.stop(name)
            shutil.rmtree(vm.directory(name))
    create_start=time.monotonic(); vm.create('a',7903,7793); create_a=time.monotonic()-create_start
    report['timings']['vzColdFreshDisk'] = {**vm.start('a'),'diskCloneSeconds':create_a,'definition':'fresh writable disk, prepared image/kernel already cached; excludes provisioning/download/compile'}
    vm.create('b',7904,7794); report['timings']['vzSecondFreshDisk'] = vm.start('b')
    a=vm.rpc('a',{'op':'info'}); b=vm.rpc('b',{'op':'info'})
    report['guests']={'a':a,'b':b}
    report['guestOS']=guest('a','/bin/cat','/etc/os-release')['stdout']
    report['guestPackages']=guest('a','/bin/sh','-c','dpkg-query -W xvfb openbox chromium python3 xterm; /usr/local/bin/x11vnc -version 2>&1 | head -1')['stdout']
    check('separate live Linux guests',a['bootId']!=b['bootId'],bootIds=[a['bootId'],b['bootId']],kernels=[a['kernel'],b['kernel']])
    check('2 vCPU and 2 GiB configuration visible in both guests',all(x['cpus']==2 and 1900000<int(x['meminfo'].splitlines()[0].split()[1])<2097152 for x in (a,b)))
    boundary=guest('a','/usr/bin/python3','-c','import os,json,socket; print(json.dumps({"uid":os.getuid(),"hostHome":os.path.exists("/Users/kevinmartin"),"dockerSocket":os.path.exists("/var/run/docker.sock"),"containerdSocket":os.path.exists("/run/containerd/containerd.sock"),"networkInterfaces":os.listdir("/sys/class/net"),"bootId":open("/proc/sys/kernel/random/boot_id").read().strip()}))')
    boundary=json.loads(boundary['stdout']); report['boundary']=boundary
    check('guest exec is non-root, no host home/socket, loopback only',boundary['uid']==1000 and not any(boundary[k] for k in ('hostHome','dockerSocket','containerdSocket')) and boundary['networkInterfaces']==['lo'])
    check('guest mount evidence has no shared host filesystem',all('virtiofs' not in x['mounts'] and '9p' not in x['mounts'] and '/Users/' not in x['mounts'] for x in (a,b)),mounts=a['mounts'])
    failed_exec=guest('a','/bin/sh','-c','exit 17')
    check('exec preserves nonzero guest exit status',failed_exec['exitCode']==17,result=failed_exec)
    marker=b'sidekicks-vm-persistence-marker\n'
    vm.rpc('a',{'op':'write','path':'marker.txt','base64':base64.b64encode(marker).decode()})
    read=vm.rpc('a',{'op':'read','path':'marker.txt'})
    check('guest files round-trip',base64.b64decode(read['base64'])==marker)
    try: vm.rpc('b',{'op':'read','path':'marker.txt'}); isolated=False
    except RuntimeError: isolated=True
    check('per-session disk data isolation',isolated)
    try: vm.rpc('a',{'op':'read','path':'../etc/passwd'}); escaped=True
    except RuntimeError: escaped=False
    check('files API rejects path outside guest workspace',not escaped)
    command('node',str(vm.HERE/'viewer-smoke.mjs'))
    viewer=json.loads((OUT/'vm-a-viewer.json').read_text())
    check('decoded viewer, input, and capture over vsock',viewer['status']=='PASS',report='vm-a-viewer.json')
    # Demonstrate an ordinary native guest terminal sharing this same desktop/kernel.
    terminal=guest('a','/bin/sh','-c','DISPLAY=:99 xterm -T "Sidekicks microVM terminal" -e sh -c \'printf "SESSION A / Native Virtualization.framework\\n\\n"; uname -r; cat /proc/sys/kernel/random/boot_id; printf "\\nDisposable guest terminal, uid: "; id -u; exec bash\' >/tmp/microvm-terminal.log 2>&1 & echo $!')
    time.sleep(0.5)
    with urllib.request.urlopen(vm.manifest('a')['api']+'/screenshot?maxWidth=1280&maxHeight=720') as response: (OUT/'vm-a-terminal.png').write_bytes(response.read())
    check('native terminal launched in same guest session',terminal['exitCode']==0,bootId=a['bootId'],terminalPid=terminal['stdout'].strip())
    report['hostAtTwoGuests']={'processes':command('/bin/ps','-axo','pid,ppid,rss,comm'), 'memoryPressure':command('memory_pressure')}
    report['hostAtTwoGuests']['processes']='\n'.join(line for line in report['hostAtTwoGuests']['processes'].splitlines() if any(k in line for k in ('Virtualization','/microvm/','com.docker')))
    report['preCrashSync']=guest('a','/bin/sync')
    check('explicit pre-crash guest sync completes',report['preCrashSync']['exitCode']==0)
    vm.stop('a',crash=True)
    b_after=vm.rpc('b',{'op':'info'})
    check('runner crash stays contained to session A',b_after['bootId']==b['bootId'] and get(vm.manifest('b')['api']+'/health')['ok'],survivingBootId=b_after['bootId'])
    report['timings']['vzRecoveryAfterCrash']=vm.start('a')
    check('restart after crash gets a new kernel boot',vm.rpc('a',{'op':'info'})['bootId']!=a['bootId'])
    check('explicitly synced guest file survives VM crash/restart',base64.b64decode(vm.rpc('a',{'op':'read','path':'marker.txt'})['base64'])==marker)
    vm.stop('b'); shutil.rmtree(vm.directory('b'))
    warm=[]
    for _ in range(3): vm.stop('a'); warm.append(vm.start('a'))
    report['timings']['vzWarmRestarts']=warm
    vm.stop('a'); shutil.rmtree(vm.directory('a')); vm.create('a',7903,7793)
    report['timings']['vzResetFreshDisk']=vm.start('a')
    try: vm.rpc('a',{'op':'read','path':'marker.txt'}); reset=False
    except RuntimeError: reset=True
    check('destroy/create resets guest disk',reset)
    vm.stop('a')
    # Shared-Docker-VM baseline: two new task-owned containers, never mutate accepted POCs.
    baseline='sidekicks-microvm-baseline'; peer='sidekicks-microvm-peer'
    existing=docker('ps','-a','--format','{{.Names}}').splitlines()
    if any(n in existing for n in (baseline,peer)): raise RuntimeError('baseline helper already exists; inspect before rerun')
    image=json.loads((OUT/'source-image.json').read_text())[0]['Id']
    create_begin=time.monotonic()
    docker('create','--name',baseline,'--platform','linux/arm64','--init','--network','none','--cpus','2','--memory','2g','--shm-size','512m','--cap-drop','ALL','--security-opt','no-new-privileges',image)
    report['timings']['dockerCreateSeconds']=time.monotonic()-create_begin
    docker('run','-d','--name',peer,'--network','none','--cpus','0.25','--memory','64m','--cap-drop','ALL','--security-opt','no-new-privileges','--entrypoint','/bin/sleep',image,'infinity')
    def desktop_start():
        begin=time.monotonic(); docker('start',baseline); healthy=None
        deadline=begin+40
        while time.monotonic()<deadline:
            try:
                state=json.loads(docker('exec',baseline,'python3','-c',
                    'import json,urllib.request; base="http://127.0.0.1:8080"; print(json.dumps({"health":json.load(urllib.request.urlopen(base+"/health",timeout=1)),"snapshots":json.load(urllib.request.urlopen(base+"/fixture-events?latest=1",timeout=1))}))'))
                if state['health']['ok']:
                    if healthy is None: healthy=time.monotonic()-begin
                    snapshots=state['snapshots']
                    if snapshots: return {'desktopHealthSeconds':healthy,'fixtureReadySeconds':time.monotonic()-begin,'fixtureMarker':snapshots[-1]['marker'],'probe':'docker exec guest-internal loopback HTTP; host exec overhead included'}
            except (OSError,ValueError,subprocess.CalledProcessError): pass
            time.sleep(0.1)
        raise RuntimeError('Docker baseline desktop timeout')
    report['timings']['dockerColdCachedImage']=desktop_start()
    ids=[docker('exec',n,'cat','/proc/sys/kernel/random/boot_id') for n in (baseline,peer)]
    kernel=docker('exec',baseline,'uname','-r')
    check('Docker containers share one guest kernel boot',ids[0]==ids[1] and ids[0]!=a['bootId'],bootIds=ids,kernel=kernel)
    inspect=json.loads(docker('inspect',baseline))[0]
    report['dockerBaseline']={'bootIds':ids,'kernel':kernel,'hostConfig':inspect['HostConfig'],'mounts':inspect['Mounts'],'stats':json.loads(docker('stats','--no-stream','--format','{{json .}}',baseline))}
    check('baseline has CPU/memory caps, no network/host mounts/published ports',inspect['HostConfig']['NanoCpus']==2000000000 and inspect['HostConfig']['Memory']==2147483648 and not inspect['Mounts'] and not inspect['HostConfig']['Privileged'] and inspect['HostConfig']['NetworkMode']=='none' and not inspect['HostConfig']['PortBindings'])
    capture=json.loads(docker('exec',baseline,'python3','-c','import urllib.request,base64,json; r=urllib.request.urlopen("http://127.0.0.1:8080/screenshot?maxWidth=1280&maxHeight=720",timeout=5); d=r.read(); print(json.dumps({"base64":base64.b64encode(d).decode(),"bytes":len(d),"contentType":r.headers["Content-Type"]}))'))
    (OUT/'docker-baseline-capture.png').write_bytes(base64.b64decode(capture.pop('base64')))
    check('same-image Docker baseline guest-internal capture',capture['contentType']=='image/png' and capture['bytes']>10000,capture=capture,limitation='No host-published viewer or input latency measured for network-none baseline')
    docker_warm=[]
    for _ in range(3): docker('stop','-t','10',baseline); docker_warm.append(desktop_start())
    report['timings']['dockerWarmRestarts']=docker_warm
    docker('stop','-t','10',baseline); docker('rm',baseline); docker('stop','-t','1',peer); docker('rm',peer)
    report['timings']['finalDemoStart']=vm.start('a')
    after=minimal_state(source_state()); report['protectedAfter']=after
    check('accepted POC containers unchanged by verification',before==after)
    report['protectedHealth']={str(port):get(f'http://127.0.0.1:{port}/health') for port in (7791,7792)}
    check('existing POCs still healthy',all(v.get('ok',v.get('status')=='ready') for v in report['protectedHealth'].values()))
    report['listeners']=command('lsof','-nP','-iTCP','-sTCP:LISTEN')
    new_listeners=[line for line in report['listeners'].splitlines() if ':7903 ' in line or ':7793 ' in line]
    check('new live viewer/control host ports bind only loopback',len(new_listeners)==2 and all('127.0.0.1:' in line for line in new_listeners),listeners=new_listeners)
    report['status']='PASS'
except BaseException as error:
    report['status']='FAIL'; report['error']=str(error); raise
finally:
    report['finishedAt']=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()); save()
    print(json.dumps({'status':report['status'],'checks':len(report['checks']),'timings':report['timings'],'error':report.get('error')},indent=2))
