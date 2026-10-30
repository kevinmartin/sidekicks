import {chromium} from 'playwright';
import {execFileSync} from 'node:child_process';
import fs from 'node:fs/promises';
const base='http://127.0.0.1:7901',out='artifacts/novnc',result={startedAt:new Date().toISOString(),checks:[]};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function api(route,body){const r=await fetch(base+route,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{});if(!r.ok)throw new Error(`${route} ${r.status} ${await r.text()}`);return r.json()}
async function shot(name){const r=await fetch(base+'/screenshot');if(!r.ok)throw new Error('capture failed');await fs.writeFile(`${out}/${name}.png`,Buffer.from(await r.arrayBuffer()))}
let browser;
try{
result.before=(await api('/fixture-events?latest=1')).at(-1)?.marker;
execFileSync('docker',['restart','sidekicks-novnc'],{encoding:'utf8',timeout:30000});
for(let i=0;i<60;i++){try{const h=await api('/health'),s=(await api('/fixture-events?latest=1')).at(-1);if(h.ok&&s?.marker&&s.marker!==result.before){result.after=s.marker;break}}catch{}await sleep(300)}
if(!result.after)throw new Error('fresh fixture marker did not appear after restart');result.checks.push('fresh fixture marker after container restart');
browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});const page=await browser.newPage({viewport:{width:1440,height:1040}});await page.goto(base);await page.waitForFunction(()=>window.sidekicks?.connected);await page.waitForFunction(()=>window.sidekicks.rfb.getImageData().data.some(v=>v>0));await page.screenshot({path:`${out}/viewer-after-restart.png`});result.checks.push('actual decoded noVNC canvas after restart');
const s='theme-proof',l=await api('/control',{action:'acquire',owner:'agent',session:s,leaseMs:60000});const send=events=>api('/input',{session:s,token:l.token,events});
// Minimize the synthetic Chromium through adapter-gated desktop input.
await send([{type:'click',x:1197,y:20}]);await sleep(300);await shot('wallpaper');result.checks.push('custom wallpaper and tint2 panel persist after restart');
await send([{type:'key',key:'Meta',action:'down'},{type:'key',key:'Enter',action:'down'},{type:'key',key:'Enter',action:'up'},{type:'key',key:'Meta',action:'up'}]);await sleep(400);
let windows='';for(let i=0;i<25;i++){try{windows=execFileSync('docker',['exec','sidekicks-novnc','xdotool','search','--name','Sidekicks - disposable terminal'],{encoding:'utf8'}).trim();if(windows)break}catch{}await sleep(200)}if(!windows)throw new Error('custom terminal shortcut did not launch');await shot('terminal');result.checks.push('custom Openbox launcher opens themed xterm after restart');
await send([{type:'key',key:'Control',action:'down'},{type:'key',key:'d',action:'down'},{type:'key',key:'d',action:'up'},{type:'key',key:'Control',action:'up'}]);await sleep(300);await send([{type:'click',x:10,y:700}]);for(let i=0;i<30;i++){const marker=(await api('/fixture-events?latest=1')).at(-1)?.marker;if(marker&&marker!==result.after){result.launcherMarker=marker;break}await sleep(200)}if(!result.launcherMarker)throw new Error('panel browser launcher did not open fresh fixture');result.checks.push('tint2 browser launcher opens fresh synthetic fixture');await sleep(300);await shot('desktop-after-restart');await api('/control',{action:'release',session:s,token:l.token});
result.status='PASS';result.finishedAt=new Date().toISOString();console.log(JSON.stringify(result));
}catch(e){result.status='FAIL';result.error=e.stack;console.error(e);process.exitCode=1}finally{if(browser)await browser.close();await fs.writeFile(`${out}/restart.json`,JSON.stringify(result,null,2))}
