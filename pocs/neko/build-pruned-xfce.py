#!/usr/bin/env python3
"""Prune/repack a fresh built image, never a running or user-owned container."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

root=Path(__file__).resolve().parent
base=os.environ['SIDEKICKS_NEKO_PRUNE_BASE']
if not re.fullmatch(r'sidekicks-neko:polish-xfce-stage-v[0-9]+',base):
    raise SystemExit('a separately built stage-vN image is required')
if shutil.disk_usage(root).free<6*1024**3:
    raise SystemExit('need 6 GiB free in workspace for fresh rootfs/repack artifacts')
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H-%M-%SZ')
out=root/'test/artifacts/xfce-pruned'/stamp
context=out/'repack'; context.mkdir(parents=True)
layer='sidekicks-neko:polish-xfce-stage-pruned-layer-v1'
final='sidekicks-neko:polish-xfce-stage-pruned-flat-v1'


def inspect(image):
    return json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]


def run(command,log):
    with (out/log).open('wb') as file:
        subprocess.run(command,check=True,stdout=file,stderr=subprocess.STDOUT)


base_info=inspect(base)
(out/'base-image.json').write_text(json.dumps(base_info,indent=2)+'\n')
command=['docker','build','--platform','linux/arm64','--network','none','--build-arg','CANDIDATE='+base,'-f',str(root/'Dockerfile.xfce-prune')]
run([*command,'--target','pruned','-t',layer,str(root)],'prune-build.log')
layer_info=inspect(layer)
(out/'pruned-layer-image.json').write_text(json.dumps(layer_info,indent=2)+'\n')
run([*command,'--target','evidence','--output','type=local,dest='+str(out/'packages'),str(root)],'export-evidence.log')
fresh='sidekicks-neko-xfce-fresh-export-'+stamp.lower()
container=subprocess.check_output(['docker','create','--name',fresh,'--network','none','--label','component=neko-xfce-fresh-export','--entrypoint','/bin/true',layer],text=True).strip()
try:
    info=json.loads(subprocess.check_output(['docker','inspect',container]))[0]
    assert info['State']['Status']=='created' and not info['State']['Running']
    assert info['Image']==layer_info['Id'] and info['Mounts']==[]
    subprocess.run(['docker','export','-o',str(context/'rootfs.tar'),container],check=True)
finally:
    subprocess.run(['docker','rm',container],check=True,stdout=subprocess.DEVNULL)
cfg=layer_info['Config']
assert not cfg.get('Volumes') and not cfg.get('OnBuild')
lines=['FROM scratch','ADD rootfs.tar /']
for entry in cfg.get('Env',[]):
    key,value=entry.split('=',1)
    assert re.fullmatch('[A-Za-z_][A-Za-z0-9_]*',key)
    lines.append('ENV '+key+'='+json.dumps(value))
if cfg.get('WorkingDir'):lines.append('WORKDIR '+cfg['WorkingDir'])
if cfg.get('User'):lines.append('USER '+cfg['User'])
if cfg.get('Shell'):lines.append('SHELL '+json.dumps(cfg['Shell']))
for key,value in (cfg.get('Labels') or {}).items():lines.append('LABEL '+json.dumps(key)+'='+json.dumps(value))
for port in (cfg.get('ExposedPorts') or {}):lines.append('EXPOSE '+port)
if cfg.get('StopSignal'):lines.append('STOPSIGNAL '+cfg['StopSignal'])
if cfg.get('Entrypoint') is not None:lines.append('ENTRYPOINT '+json.dumps(cfg['Entrypoint']))
if cfg.get('Cmd') is not None:lines.append('CMD '+json.dumps(cfg['Cmd']))
health=cfg.get('Healthcheck')
if health:
    options=[]
    for key,flag in [('Interval','interval'),('Timeout','timeout'),('StartPeriod','start-period'),('StartInterval','start-interval')]:
        if health.get(key):options.append('--'+flag+'='+str(health[key])+'ns')
    if health.get('Retries'):options.append('--retries='+str(health['Retries']))
    test=health['Test']
    if test[0]=='CMD-SHELL':tail='CMD '+test[1]
    elif test[0]=='CMD':tail='CMD '+json.dumps(test[1:])
    elif test[0]=='NONE':tail='NONE'
    else:raise RuntimeError('unknown healthcheck form')
    lines.append('HEALTHCHECK '+' '.join(options)+' '+tail)
(context/'Dockerfile').write_text('\n'.join(lines)+'\n')
run(['docker','build','--platform','linux/arm64','--network','none','-t',final,str(context)],'repack-build.log')
final_info=inspect(final)
(out/'final-image.json').write_text(json.dumps(final_info,indent=2)+'\n')
keys=['Env','User','WorkingDir','Shell','Labels','ExposedPorts','StopSignal','Entrypoint','Cmd','Healthcheck','Volumes']
metadata={key:cfg.get(key)==final_info['Config'].get(key) for key in keys}
assert all(metadata.values()),metadata
empty_layer=hashlib.sha256(bytes(1024)).hexdigest()
nonempty_layers=[p for p in final_info['RootFS']['Layers'] if p!='sha256:'+empty_layer]
assert len(nonempty_layers)==1
assert inspect(base)['Id']==base_info['Id']


def manifest(path):
    rows={}
    for line in path.read_text().splitlines():
        name,version,size=line.split('\t'); rows[name]={'version':version,'installedKiB':int(size)}
    return rows


before=manifest(out/'packages/packages-before.tsv'); after=manifest(out/'packages/packages-after.tsv')
original=inspect('sidekicks-neko:3.2.0-ux1')
assert original['Id']=='sha256:1aca12ae8c4937e2562c0b2331e628b3dc8316d5e494bbfa75e94d91dbaf2d53'
digest=hashlib.sha256()
with (context/'rootfs.tar').open('rb') as file:
    while chunk:=file.read(4*1024**2):digest.update(chunk)
report={'status':'PRUNED_REPACK_BUILT_RUNTIME_UNTESTED','originalImage':original['Id'],'baseImage':base_info['Id'],
        'prunedLayerImage':layer_info['Id'],'finalImage':final_info['Id'],'finalTag':final,'architecture':final_info['Architecture'],
        'originalBytes':original['Size'],'candidateBytes':base_info['Size'],'prunedLayerBytes':layer_info['Size'],'flatFinalBytes':final_info['Size'],
        'netFlatVersusOriginalBytes':final_info['Size']-original['Size'],'flatVersusCandidateBytes':final_info['Size']-base_info['Size'],
        'removedPackages':{name:value for name,value in before.items() if name not in after},
        'packageInstalledDeltaKiB':sum(p['installedKiB'] for p in after.values())-sum(p['installedKiB'] for p in before.values()),
        'configMetadataRestoredExactly':metadata,'finalFilesystemLayers':len(final_info['RootFS']['Layers']),
        'nonemptyRootfsLayers':len(nonempty_layers),'emptyCompatibilityLayer':'WORKDIR restoration may emit the standard empty tar layer',
        'rootfsSource':'Fresh never-started created container from built candidate, not a live/user container',
        'rootfsSha256':digest.hexdigest(),'autoremoveApplied':False,
        'caveat':'Later-layer purge is not claimed as image savings. Measured final size uses one fresh FROM scratch ADD rootfs layer. All old images and host artifacts remain; host disk use has not shrunk.'}
(out/'footprint.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
print('Evidence:',out)
