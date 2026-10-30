import fs from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
const run=(cmd,args)=>{const r=spawnSync(cmd,args,{encoding:'utf8',timeout:15000});return{status:r.status,stdout:r.stdout?.trim(),stderr:r.stderr?.trim()};};
const names=['sidekicks-workbench','sidekicks-novnc','sidekicks-neko'],containers=[];
for(const name of names){
 const raw=run('docker',['inspect',name]);
 if(raw.status!==0){containers.push({name,status:'UNTESTED',reason:raw.stderr});continue;}
 const d=JSON.parse(raw.stdout)[0],h=d.HostConfig,c=d.Config;
 containers.push({name,id:d.Id,image:d.Image,imagePlatform:run('docker',['image','inspect','--format','{{.Os}}/{{.Architecture}}',d.Image]).stdout,running:d.State.Running,status:d.State.Status,health:d.State.Health?.Status,user:c.User,platform:d.Platform,ports:d.NetworkSettings.Ports,mounts:d.Mounts.map(m=>({type:m.Type,source:m.Source,destination:m.Destination,readWrite:m.RW})),resources:{nanoCpus:h.NanoCpus,memoryBytes:h.Memory,shmSizeBytes:h.ShmSize,pidsLimit:h.PidsLimit},privileged:h.Privileged,networkMode:h.NetworkMode,capAdd:h.CapAdd,capDrop:h.CapDrop,securityOpt:h.SecurityOpt,readOnlyRootfs:h.ReadonlyRootfs,restartPolicy:h.RestartPolicy,validation:{allHostPublishedBindingsLoopback:Object.values(d.NetworkSettings.Ports||{}).flat().filter(Boolean).every(p=>['127.0.0.1','::1'].includes(p.HostIp)),noHostMounts:d.Mounts.length===0,unprivileged:h.Privileged===false}});
}
const report={generatedAt:new Date().toISOString(),independent:true,containers,engine:run('docker',['info','--format','{{.Architecture}} {{.NCPU}} {{.MemTotal}}']),listeners:run('lsof',['-nP','-iTCP:7790','-iTCP:7791','-iTCP:7792','-iTCP:7901','-iTCP:7902','-sTCP:LISTEN']),taskStats:run('docker',['stats','--no-stream','--format','{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}',...containers.filter(c=>c.running).map(c=>c.name)])};
await fs.mkdir('artifacts/independent-qa',{recursive:true});await fs.writeFile('artifacts/independent-qa/runtime.json',JSON.stringify(report,null,2));
for(const c of containers)console.log(c.name,c.validation||c.status);
