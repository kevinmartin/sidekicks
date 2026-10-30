import {chromium} from 'playwright';
import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile),base='http://127.0.0.1:17802',container='sidekicks-neko-xfce-stage';
const inspect=process.argv.includes('--inspect'),args=Object.fromEntries(process.argv.slice(2).filter(x=>x.includes('=')).map(x=>x.replace(/^--/,'').split('=')));
const root=new URL('./artifacts/xfce-stage/',import.meta.url),out=new URL('confirmation-'+new Date().toISOString().replace(/[:.]/g,'-')+'/',root);await fs.mkdir(out,{recursive:true});
const observer=await fs.readFile(new URL('./x11-stage-observe.py',import.meta.url),'utf8'),report={phase:inspect?'dialog-inspection':'cancel-and-restart',status:'FAIL',checks:[]};
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function guest(...a){return(await exec('docker',['exec','--user','neko',container,...a],{maxBuffer:4e6})).stdout;}
async function api(p,b){const r=await fetch(base+p,{method:b?'POST':'GET',headers:b?{'content-type':'application/json'}:undefined,body:b?JSON.stringify(b):undefined});return await r.json();}
async function x11(){return JSON.parse(await guest('python3','-c',observer));}
async function until(fn,label,ms=20000){for(let t=Date.now();Date.now()-t<ms;){try{const r=await fn();if(r)return r;}catch{}await delay(100);}throw new Error('timeout '+label);}
async function apps(){return(await x11()).windows.filter(w=>['SidekicksBrowser','SidekicksTerminal','Thunar'].includes(w.wmClass.at(-1))&&w.types.includes('_NET_WM_WINDOW_TYPE_NORMAL')).map(w=>({id:w.id,pid:w.pid,class:w.wmClass.at(-1)})).sort((a,b)=>a.class.localeCompare(b.class));}
async function dialog(){const x=await x11();return [...x.windows,...x.popupWindows].find(w=>w.wmClass.at(-1)==='Xfce4-session'&&/log.?out|session/i.test(w.title??''));}
const browser=await chromium.launch({channel:'chrome',headless:true}),page=await browser.newPage({viewport:{width:1440,height:1000}});
async function decoded(){await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element.videoWidth===1280&&window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>5,{},{timeout:30000});}
async function point(x,y){const b=await page.locator('#gated').boundingBox();return{x:b.x+x*b.width/1280,y:b.y+y*b.height/720};}
async function click(x,y,options){const p=await point(x,y);await page.mouse.click(p.x,p.y,options);await delay(180);}
async function take(){await page.evaluate(()=>window.__sidekicksNeko.take());}
async function shot(n){await page.screenshot({path:new URL(n+'-viewer.png',out).pathname});await fs.writeFile(new URL(n+'-desktop.jpg',out),Buffer.from(await(await fetch(base+'/screenshot')).arrayBuffer()));}
async function dock(index){const p=(await x11()).windows.find(w=>w.wmClass.at(-1)==='Xfce4-panel');await click(p.client.x+28+56*index,p.client.y+28);}
async function logout(){const p=(await x11()).windows.find(w=>w.wmClass.at(-1)==='Xfce4-panel');await click(p.client.x+210,p.client.y+28,{button:'right'});const m=await until(async()=>(await x11()).popupWindows.find(w=>w.client.width>80&&w.client.width<300&&w.client.height>100&&w.client.height<200),'stock panel menu');await click(m.client.x+m.client.width/2,m.client.y+m.client.height*.38);return await until(dialog,'logout confirmation');}
try{
 await until(async()=>(await api('/desktop/state')).ready,'desktop ready');await page.goto(base+'/');await decoded();await take();
 assert.equal((await guest('xfconf-query','-c','xfce4-session','-p','/general/PromptOnLogout')).trim(),'true');
 if(inspect){
  const existing=await dialog();
  if(!existing){await page.locator('#terminal').click();await until(async()=>(await apps()).some(w=>w.class==='SidekicksTerminal'),'terminal open');await dock(2);await until(async()=>(await apps()).length===3,'three open apps');}
  const before={apps:await apps(),sessionPid:(await api('/desktop/state')).processes['xfce4-session'][0]};assert.equal(before.apps.length,3);const d=existing??await logout();await shot('confirmation');
  const pending={before,dialog:d,artifact:out.pathname};await fs.writeFile(new URL('confirmation-pending.json',root),JSON.stringify(pending,null,2)+'\n');report.pending=pending;report.status='OBSERVED_DIALOG_AWAITING_MEASURED_CANCEL_CLICK';
 }else{
  const pending=JSON.parse(await fs.readFile(new URL('confirmation-pending.json',root),'utf8'));assert(await dialog());await click(Number(args.x),Number(args.y));await until(async()=>!(await dialog()),'Cancel dismisses dialog');
  const after={apps:await apps(),sessionPid:(await api('/desktop/state')).processes['xfce4-session'][0]};assert.deepEqual(after,pending.before);report.checks.push({name:'actual Cancel preserves all three app clients/PIDs and session PID',status:'PASS',before:pending.before,after});await shot('cancel-preserved');
  await api('/control',{action:'stop'});await exec('docker',['restart','-t','10',container],{timeout:30000});await until(async()=>(await api('/desktop/state')).ready,'restart ready',45000);await page.reload();await decoded();await take();
  assert.equal((await guest('xfconf-query','-c','xfce4-session','-p','/general/PromptOnLogout')).trim(),'true');const beforeRestartCancel={apps:await apps(),sessionPid:(await api('/desktop/state')).processes['xfce4-session'][0]};const d=await logout();await shot('restart-confirmation');
  const dx=Number(args.x)-pending.dialog.client.x,dy=Number(args.y)-pending.dialog.client.y;await click(d.client.x+dx,d.client.y+dy);await until(async()=>!(await dialog()),'restart Cancel');const afterRestartCancel={apps:await apps(),sessionPid:(await api('/desktop/state')).processes['xfce4-session'][0]};assert.deepEqual(afterRestartCancel,beforeRestartCancel);report.checks.push({name:'normal restart preserves supported prompt and actual Cancel',status:'PASS',dialog:d,before:beforeRestartCancel,after:afterRestartCancel});
  await api('/control',{action:'stop'});await shot('restart-home');await page.goto(base+'/?mode=native');await decoded();report.checks.push({name:'gated and native streams decode after restart; actual held state empty',status:'PASS',video:await page.evaluate(()=>{const v=window.__sidekicksNeko.client.transport.element;return{width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames};})});const x=await x11();assert.equal(x.allPressedKeycodes.length,0);assert.equal(x.pointer.pressedButtons.length,0);report.status='PASS';
 }
}catch(e){report.error=e.stack;process.exitCode=1;await shot('failure').catch(()=>{});console.error(e.message);}
finally{await api('/control',{action:'stop'}).catch(()=>{});await browser.close();await fs.writeFile(new URL('report.json',out),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({status:report.status,artifact:out.pathname,pending:report.pending?.dialog?.client}));}
