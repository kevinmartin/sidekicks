// Bounded stock-menu/logout observations on the disposable stage ONLY.
import {chromium} from 'playwright';import fs from 'node:fs/promises';import assert from 'node:assert/strict';import {execFileSync} from 'node:child_process';
const C='sidekicks-novnc-xfce-stage',API='http://127.0.0.1:17791',BASE='http://127.0.0.1:17801',legacy=process.argv.includes('--legacy');
const out='artifacts/novnc/polish/menu-'+(legacy?'legacy-':'confirm-')+new Date().toISOString().replace(/[:.]/g,'-');await fs.mkdir(out,{recursive:true});
const observer=await fs.readFile('artifacts/independent-qa/ux/x11-observe.py','utf8');const guest=code=>execFileSync('docker',['exec','-i',C,'python3','-'],{input:code,encoding:'utf8'});const state=()=>JSON.parse(guest(observer));
const sleep=ms=>new Promise(r=>setTimeout(r,ms));const report={startedAt:new Date().toISOString(),legacy,stageOnly:true,checks:[],limitations:['Synthetic task-owned viewer; stock Help is observed briefly without waiting for documentation network load.','Legacy immediate logout is deliberately disruptive only to this disposable stage; no live7901 calls.']};
const win=(s,k)=>s.windows.find(w=>w.wmClass.some(c=>c.toLowerCase()==={browser:'sidekicksbrowser',terminal:'xfce4-terminal',files:'thunar'}[k])&&w.types.includes('_NET_WM_WINDOW_TYPE_NORMAL'));
const snapshot=()=>{const s=state();return{apps:Object.fromEntries(['browser','terminal','files'].map(k=>{const w=win(s,k);return[k,w?{id:w.id,pid:w.pid}:null]})),state:s,sessionPID:guest("import subprocess;print(subprocess.check_output(['pgrep','-x','xfce4-session'],text=True).strip())").trim()}};
async function until(fn,ms=7000){const t=Date.now();while(Date.now()-t<ms){const x=await fn();if(x)return x;await sleep(100)}throw Error('bounded menu observation timed out')}
const read=async path=>{const r=await fetch(API+path);return{status:r.status,data:await r.json()}};
let browser,page;
try{
 await until(async()=>{try{const r=await read('/desktop/state');return r.status===200&&r.data.ready;}catch{return false;}},35000);
 assert.equal((await read('/control')).data.owner,null);report.beforeReady=await read('/desktop/state');
 report.runtime=JSON.parse(execFileSync('docker',['inspect',C],{encoding:'utf8'})).map(c=>({id:c.Id,image:c.Image}));
 browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});page=await browser.newPage({viewport:{width:1440,height:1040}});await page.goto(BASE);await page.waitForFunction(()=>window.sidekicks?.connected);await page.evaluate(()=>sidekicks.take());await sleep(150);
 await page.keyboard.press('Escape');await sleep(100); // Dismiss an earlier preserved test dialog, if present.
 const chord=async keys=>{await page.evaluate(keys=>sidekicks.chord(keys),keys);await sleep(200)};
 await chord(['Meta','Enter']);await until(()=>win(state(),'terminal'));await chord(['Meta','e']);await until(()=>win(state(),'files'));
 const point=async p=>{const b=await page.locator('#screen canvas').boundingBox();return{x:b.x+p.x*b.width/1280,y:b.y+p.y*b.height/720}};
 const shot=async name=>{const r=await fetch(API+'/screenshot');await fs.writeFile(out+'/'+name+'-guest.png',Buffer.from(await r.arrayBuffer()));await page.screenshot({path:out+'/'+name+'-viewer.png'});};
 const menu=async()=>{const p=state().windows.find(w=>w.types.includes('_NET_WM_WINDOW_TYPE_DOCK')&&w.wmClass.some(c=>c.toLowerCase()==='xfce4-panel')).client;const q=await point({x:p.x+190,y:p.y+28});await page.mouse.click(q.x,q.y,{button:'right'});await sleep(150);};
 if(legacy){
  await menu();await shot('stock-menu');await page.keyboard.press('End');await page.keyboard.press('Enter');await sleep(400);report.about=state();await shot('about');await page.keyboard.press('Escape');await sleep(100);
  await menu();await page.keyboard.press('End');await page.keyboard.press('ArrowUp');await page.keyboard.press('Enter');await sleep(900);report.help=state();await shot('help');await page.keyboard.press('Escape');
 }
 const before=snapshot();report.beforeLogout=before;await menu();await page.keyboard.press('End');await page.keyboard.press('ArrowUp');await page.keyboard.press('ArrowUp');await page.keyboard.press('Enter');await sleep(400);report.afterMenuActivate=state();await shot('logout-activated');
 if(!legacy){
  // Xfce's session prompt is override-redirect and omitted from _NET_CLIENT_LIST.
  const dialog=await until(()=>{const tree=guest("import subprocess;print(subprocess.check_output(['xwininfo','-root','-tree'],text=True))");return tree.split('\n').some(l=>{const m=l.match(/"xfce4-session":.*?\s(\d+)x(\d+)\+/);return m&&+m[1]>200&&+m[2]>100;})&&tree;});report.confirmationDialog={source:'actual X11 session prompt window dimensions/class and captured pixels',tree:dialog};
  await page.keyboard.press('Escape');await sleep(350);const after=snapshot();report.afterCancel=after;assert.deepEqual(after.apps,before.apps);assert.equal(after.sessionPID,before.sessionPID);assert.deepEqual(after.state.allPressedKeycodes,[]);assert.deepEqual(after.state.pointer.pressedButtons,[]);await shot('cancel-preserved');report.checks.push({name:'logout confirmation and Cancel retain all3client IDs/PIDs and session PID',status:'PASS'});
  if(process.argv.includes('--confirm-exit')){
   await menu();await page.keyboard.press('End');await page.keyboard.press('ArrowUp');await page.keyboard.press('ArrowUp');await page.keyboard.press('Enter');await sleep(250);
   const q=await point({x:532,y:310});await page.mouse.click(q.x,q.y);await until(async()=>{const r=await read('/desktop/state');return r.status===503&&!r.data.ready;});report.afterConfirmedLogoutReady=await read('/desktop/state');report.afterConfirmedLogout=state();await shot('confirmed-logout');report.checks.push({name:'confirmed session exit reports503 and ready false',status:'PASS'});
  }
 }else{
  await sleep(900);report.afterLogout=state();report.afterLogoutReady=await read('/desktop/state');report.processes=guest("import subprocess,json;print(json.dumps(subprocess.check_output(['ps','-eo','comm'],text=True).splitlines()))");report.checks.push({name:'stock immediate logout observed',status:'OBSERVED',detail:'See actual windows/processes/readiness; no automatic recovery assertion.'});
 }
 await page.evaluate(()=>sidekicks.lease?sidekicks.release():null);report.controlAfter=await read('/control');report.finalHeld=state();assert.equal(report.controlAfter.data.owner,null);assert.deepEqual(report.finalHeld.allPressedKeycodes,[]);assert.deepEqual(report.finalHeld.pointer.pressedButtons,[]);report.status=legacy?'OBSERVED':'PASS';
}catch(e){report.status='FAIL';report.error=e.stack;process.exitCode=1;}
finally{await page?.evaluate(()=>sidekicks.lease?sidekicks.release():null).catch(()=>{});await browser?.close();report.finishedAt=new Date().toISOString();await fs.writeFile(out+'/result.json',JSON.stringify(report,null,2));console.log(JSON.stringify({status:report.status,evidence:out,error:report.error,afterLogoutReady:report.afterLogoutReady}));}
