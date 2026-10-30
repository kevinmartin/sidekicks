import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile),base='http://127.0.0.1:17802',container='sidekicks-neko-xfce-stage';
const out=new URL('./artifacts/xfce-stage/final-lifecycle-'+new Date().toISOString().replace(/[:.]/g,'-')+'/',import.meta.url);await fs.mkdir(out,{recursive:true});
const report={status:'FAIL',scope:'Stock menu facts and normal restart of isolated stage only',checks:[]};
const observer=await fs.readFile(new URL('./x11-stage-observe.py',import.meta.url),'utf8');
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function guest(...args){return(await exec('docker',['exec','--user','neko',container,...args],{maxBuffer:4e6})).stdout;}
async function x11(){return JSON.parse(await guest('python3','-c',observer));}
async function api(path,body){const r=await fetch(base+path,{method:body?'POST':'GET',headers:body?{'content-type':'application/json'}:undefined,body:body?JSON.stringify(body):undefined});return{status:r.status,body:await r.json()};}
async function until(fn,label,ms=20000){for(let t=Date.now();Date.now()-t<ms;){try{const r=await fn();if(r)return r;}catch{}await delay(150);}throw new Error('timeout '+label);}
async function check(name,fn){try{const detail=await fn();report.checks.push({name,status:'PASS',detail});console.log('PASS '+name);}catch(e){report.checks.push({name,status:'FAIL',error:e.message});throw e;}}
const browser=await chromium.launch({channel:'chrome',headless:true}),page=await browser.newPage({viewport:{width:1440,height:1000}});
async function decoded(){await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element.videoWidth===1280&&window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>5,{},{timeout:30000});}
async function take(){await page.evaluate(()=>window.__sidekicksNeko.take());}
async function point(x,y){const b=await page.locator('#gated').boundingBox();return{x:b.x+x*b.width/1280,y:b.y+y*b.height/720};}
async function click(x,y,options){const p=await point(x,y);await page.mouse.click(p.x,p.y,options);await delay(200);}
async function shot(name){await page.screenshot({path:new URL(name+'-viewer.png',out).pathname});await fs.writeFile(new URL(name+'-desktop.jpg',out),Buffer.from(await(await fetch(base+'/screenshot')).arrayBuffer()));}
async function menu(){await take();await page.keyboard.press('Escape');const panel=(await x11()).windows.find(w=>w.wmClass.at(-1)==='Xfce4-panel');await click(panel.client.x+210,panel.client.y+28,{button:'right'});return await until(async()=>(await x11()).popupWindows.find(w=>w.client.width>80&&w.client.width<300&&w.client.height>100&&w.client.height<200),'stock panel menu');}
async function closeDialogs(){for(let n=0;n<3;n++){const w=(await x11()).windows.find(w=>w.types.includes('_NET_WM_WINDOW_TYPE_DIALOG'));if(!w)return;const b=w.decorations?.buttonsRightToLeft?.[0];if(b)await click(b.x+b.width/2,b.y+b.height/2);else await page.keyboard.press('Escape');await delay(150);}}
try{
 await until(async()=>(await api('/desktop/state')).body.ready,'desktop ready');
 await page.goto(base+'/');await decoded();await take();
 await check('supported non-root panel lock and About dialog',async()=>{const m=await menu();await shot('stock-panel-menu');await click(m.client.x+m.client.width/2,m.client.y+m.client.height*.86);const dialog=await until(async()=>(await x11()).windows.find(w=>w.types.includes('_NET_WM_WINDOW_TYPE_DIALOG')),'About dialog');await shot('about');await closeDialogs();return{dialogTitle:dialog.title,dialogClass:dialog.wmClass,classification:'FUNCTIONAL',menuPolicy:'Customization removed by supported named-user lock; stock Help/About/Log Out remain.'};});
 await check('stock Help action inspected in disposable guest',async()=>{const before=(await x11()).windows.map(w=>({id:w.id,title:w.title}));const m=await menu();await click(m.client.x+m.client.width/2,m.client.y+m.client.height*.62);await delay(1200);await shot('help');const after=(await x11()).windows;const changed=after.filter(w=>!before.some(p=>p.id===w.id&&p.title===w.title));await closeDialogs();return{classification:'OBSERVED_UI',changed:changed.map(w=>({id:w.id,title:w.title,wmClass:w.wmClass})),limit:'Screenshot inspected for actual Help behavior; no physical browser or external-site availability claim.'};});
 await check('stock Log Out ends guest apps and session recovery is observed',async()=>{const before=(await api('/desktop/state')).body;const m=await menu();await click(m.client.x+m.client.width/2,m.client.y+m.client.height*.38);await delay(1000);await shot('logout-first');let state=(await api('/desktop/state')).body;
  if(state.processes?.['xfce4-session']?.[0]===before.processes['xfce4-session'][0]){
   const dialogs=(await x11()).windows.filter(w=>w.types.includes('_NET_WM_WINDOW_TYPE_DIALOG'));
   report.logoutPrompt=dialogs.map(w=>({title:w.title,client:w.client}));
   // A confirmation dialog's focused primary action is selected by Enter.
   if(dialogs.length){await page.keyboard.press('Enter');await delay(400);}
  }
  const recovered=await until(async()=>{const s=(await api('/desktop/state')).body;return s.ready&&s.processes['xfce4-session'][0]!==before.processes['xfce4-session'][0]&&s;},'new session after Log Out',30000);
  await shot('logout-recovered');return{classification:'FUNCTIONAL_DISRUPTIVE',effect:'Closes the disposable guest session/apps; Supervisor automatically starts a new Xfce session and Home.',beforeSessionPid:before.processes['xfce4-session'][0],afterSessionPid:recovered.processes['xfce4-session'][0],sameContainer:true};});
 await api('/control',{action:'stop'});
 await check('normal stage container restart restores sole session and fresh decoded Home',async()=>{const before=(await api('/desktop/state')).body;await exec('docker',['restart','-t','10',container],{timeout:30000,maxBuffer:1e6});await until(async()=>(await api('/desktop/state')).body.ready,'restart desktop ready',45000);await page.reload();await decoded();const state=(await api('/desktop/state')).body;for(const ids of Object.values(state.processes))assert.equal(ids.length,1);const x=await x11();assert.equal(x.windows.filter(w=>w.wmClass.at(-1)==='SidekicksBrowser').length,1);assert.equal(x.allPressedKeycodes.length,0);assert.equal(x.pointer.pressedButtons.length,0);await shot('restart-gated');return{desktop:state,actualHeldEmpty:true};});
 await check('native WebRTC also decodes after normal restart',async()=>{await page.goto(base+'/?mode=native');await decoded();await page.screenshot({path:new URL('restart-native-viewer.png',out).pathname});return await page.evaluate(()=>{const v=window.__sidekicksNeko.client.transport.element;return{width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames};});});
 report.status='PASS';
}catch(e){report.error=e.stack;process.exitCode=1;await shot('failure').catch(()=>{});console.error(e.message);}
finally{await api('/control',{action:'stop'}).catch(()=>{});await browser.close();await fs.writeFile(new URL('report.json',out),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({status:report.status,checks:report.checks.length,artifact:out.pathname}));}
