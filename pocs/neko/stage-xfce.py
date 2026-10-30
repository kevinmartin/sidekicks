#!/usr/bin/env python3
"""Start only the separately authorized, loopback-only disposable Xfce stage."""
import datetime
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
import urllib.request

root = Path(__file__).resolve().parent
workspace = root.parent.parent
name = 'sidekicks-neko-xfce-stage'
image = os.environ.get('SIDEKICKS_NEKO_XFCE_IMAGE', 'sidekicks-neko:polish-xfce-stage-v1')
if not image.startswith('sidekicks-neko:polish-xfce-stage-'):
    raise SystemExit('stage-only image tag required')
subprocess.run(['docker', 'image', 'inspect', image], check=True, stdout=subprocess.DEVNULL)
if subprocess.run(['docker', 'container', 'inspect', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
    raise SystemExit('stage container already exists; preserve/inspect it before any replacement')
previous = None
if '--reuse-proxy' in sys.argv:
    previous = json.loads((root/'test/artifacts/xfce-stage/current-stage.json').read_text())
    process = subprocess.check_output(['ps', '-p', str(previous['proxyPid']), '-o', 'command='], text=True).strip()
    if process != str(shutil.which('node'))+' '+str(workspace/'scripts/neko-stage-proxy.mjs'):
        raise SystemExit('refusing to reuse a proxy PID without the exact expected command')
for port in [17792, 17782] + ([] if previous else [17802]):
    with socket.socket() as sock:
        # Ignore old connection TIME_WAIT; a live listener still prevents bind.
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(('127.0.0.1', port))
for port in range(59420, 59440):
    with socket.socket(type=socket.SOCK_DGRAM) as sock:
        sock.bind(('127.0.0.1', port))
stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H-%M-%SZ')
out = root / 'test/artifacts/xfce-stage' / ('runtime-start-'+stamp)
out.mkdir(parents=True)
command = ['docker', 'run', '--pull=never', '-d', '--name', name, '--label', 'app=sidekicks', '--label', 'component=neko-xfce-stage',
           '--platform', 'linux/arm64', '--cpus=2', '--memory=2g', '--shm-size=512m', '--pids-limit=512',
           '--security-opt=no-new-privileges:true', '-p', '127.0.0.1:17792:7792', '-p', '127.0.0.1:17782:7902',
           '-p', '127.0.0.1:59420-59439:59420-59439/udp', '-e', 'NEKO_WEBRTC_EPR=59420-59439', image]
container_id = subprocess.check_output(command, text=True).strip()
if previous:
    proxy_pid, proxy_log = previous['proxyPid'], previous['proxyLog']
else:
    with (out/'proxy.log').open('ab', buffering=0) as logfile:
        proxy = subprocess.Popen([shutil.which('node'), str(workspace/'scripts/neko-stage-proxy.mjs')], cwd=workspace,
                                 stdin=subprocess.DEVNULL, stdout=logfile, stderr=logfile, start_new_session=True, close_fds=True)
    proxy_pid, proxy_log = proxy.pid, str(out/'proxy.log')
info = json.loads(subprocess.check_output(['docker', 'inspect', name], text=True))[0]
record = {'container': name, 'containerId': container_id, 'image': info['Image'], 'imageTag': image,
          'proxyPid': proxy_pid, 'proxyScript': 'scripts/neko-stage-proxy.mjs', 'proxyLog': proxy_log,
          'viewerUrl': 'http://127.0.0.1:17802/', 'nativeViewerUrl': 'http://127.0.0.1:17802/?mode=native',
          'rawViewerPort': 17782, 'rawApiPort': 17792, 'udpPorts': '59420-59439',
          'bindings': info['HostConfig']['PortBindings'], 'cpuNano': info['HostConfig']['NanoCpus'],
          'memory': info['HostConfig']['Memory'], 'shm': info['HostConfig']['ShmSize'],
          'privileged': info['HostConfig']['Privileged'], 'mounts': info['Mounts'], 'artifact': str(out)}
(out/'stage.json').write_text(json.dumps(record, indent=2)+'\n')
(root/'test/artifacts/xfce-stage/current-stage.json').write_text(json.dumps(record, indent=2)+'\n')
print(json.dumps(record, indent=2))
