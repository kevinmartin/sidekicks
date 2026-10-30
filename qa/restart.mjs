import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {performance} from 'node:perf_hooks';

const id=process.argv[2];if(!['novnc','neko'].includes(id))throw Error('Usage: node qa/restart.mjs novnc|neko (reserved window required)');
const name=`sidekicks-${id}`,base=`http://127.0.0.1:${id==='novnc'?7791:7792}`,out='artifacts/independent-qa';await fs.mkdir(out,{recursive:true});
const report={generatedAt:new Date().toISOString(),independent:true,id,checks:[],limitation:'Restarts only the named disposable task container; no image rebuild, source edits or unrelated resource changes. Actual viewer decode/input after restart is verified separately.'};
const run=(args)=>{const r=spawnSync('docker',args,{encoding:'utf8',timeout:20000});assert.equal(r.status,0,r.stderr);return r.stdout.trim();};
const req=async route=>{const r=await fetch(base+route,{signal:AbortSignal.timeout(5000)});return{status:r.status,data:await r.json()};};
async function marker(){const r=await req(id==='novnc'?'/fixture-events?latest=1':'/fixture-state');if(r.status!==200)return null;return id==='novnc'?r.data.at(-1)?.marker:r.data.marker?.instance;}
async function capture(label){const r=await fetch(base+'/screenshot?maxWidth=1280&maxHeight=720'),data=Buffer.from(await r.arrayBuffer());assert.equal(r.status,200);const file=`${out}/${id}-restart-${label}.${r.headers.get('content-type').includes('jpeg')?'jpg':'png'}`;await fs.writeFile(file,data);return{file,bytes:data.length,headers:Object.fromEntries(r.headers)};}
const inspect=()=>JSON.parse(run(['inspect','--format','{{json .State}}',name]));
const themeFiles=id==='novnc'?['/home/desktop/.config/openbox/rc.xml','/home/desktop/.config/tint2/tint2rc','/tmp/sidekicks-wallpaper.png']:['/home/neko/.config/openbox/rc.xml','/home/neko/.config/tint2/tint2rc','/etc/sidekicks/wallpaper.png'];
try{
 const control=await req('/control');assert.ok(!control.data.owner,'Wait for the exclusive idle restart window');
 const firstMarker=await marker();assert.ok(firstMarker);const first=inspect();report.before={marker:firstMarker,state:first,themeHashes:run(['exec',name,'sha256sum',...themeFiles]),capture:await capture('before')};
 const repeated=run(['start',name]);const afterRepeated=inspect();assert.equal(first.StartedAt,afterRepeated.StartedAt);report.checks.push({name:'starting already-running task container is repeatable',status:'PASS',detail:{container:repeated,startedAt:first.StartedAt}});
 const started=performance.now();run(['restart','-t','10',name]);const deadline=performance.now()+45000;let health,lastMarker;
 while(performance.now()<deadline){try{health=await req('/health');lastMarker=await marker();if(health.status===200&&health.data.ok&&lastMarker&&lastMarker!==firstMarker)break;}catch{}await new Promise(r=>setTimeout(r,250));}
 assert.equal(health?.status,200);assert.ok(lastMarker&&lastMarker!==firstMarker,'Fresh fixture marker required after restart');const state=inspect(),hashes=run(['exec',name,'sha256sum',...themeFiles]);assert.notEqual(state.StartedAt,first.StartedAt);assert.equal(hashes,report.before.themeHashes);const owner=(await req('/control')).data;assert.ok(!owner.owner);
 report.after={marker:lastMarker,state,themeHashes:hashes,capture:await capture('after'),health:health.data,control:owner};report.checks.push({name:'restart returns real ready desktop with fresh fixture marker and same theme',status:'PASS',detail:{restartToReadyHTTPMs:performance.now()-started,beforeMarker:firstMarker,afterMarker:lastMarker,themeHashes:hashes}});
}catch(e){report.checks.push({name:'coordinated task restart',status:'FAIL',error:e.message});process.exitCode=1;}
await fs.writeFile(`${out}/${id}-restart.json`,JSON.stringify(report,null,2));for(const c of report.checks)console.log(c.status,c.name);
