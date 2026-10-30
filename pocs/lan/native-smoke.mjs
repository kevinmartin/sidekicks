import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {createRequire} from 'node:module';
import {spawnSync} from 'node:child_process';
import {variants} from './bridge.mjs';
const require=createRequire(new URL('../../qa/package.json',import.meta.url)),{chromium}=require('playwright');
const config=JSON.parse(await fs.readFile('.run/lan/config.json','utf8')),out='artifacts/lan';
const browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true,args:['--no-first-run','--no-default-browser-check']});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const until=async fn=>{for(let i=0;i<100;i++){const v=await fn();if(v)return v;await sleep(100);}throw Error('native observation timeout');};
try{for(const v of variants.filter(x=>x.kind==='neko')){
 const base=`http://${config.advertise}:${v.port}`,apiPort={7902:7792,7906:7796,7907:7797}[v.target],context=await browser.newContext({viewport:{width:390,height:844}}),page=await context.newPage(),report={id:v.id,route:'native SDK/data channel over LAN UDP',startedAt:new Date().toISOString(),checks:[],physicalPhone:'UNTESTED'};
 const input=events=>page.evaluate(events=>{const c=window.__sidekicksNeko;return c.api('/input',{session:c.session,token:c.lease.token,events});},events);
 const chord=keys=>input(keys.map(key=>({type:'key',key,action:'down'})).concat([...keys].reverse().map(key=>({type:'key',key,action:'up'}))));
 const navigate=async url=>{await chord(['Control','l']);await sleep(200);await input([{type:'text',text:url}]);await sleep(150);await chord(['Enter']);await sleep(800);};
 const connect=async native=>{await page.goto(base+(native?'/?mode=native':'/'));await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth===1280,{timeout:20000});await page.evaluate(()=>__sidekicksNeko.take());if(native)await page.waitForFunction(()=>__sidekicksNeko.client.controlling);};
 const fixture=async()=>{const r=await fetch(`http://127.0.0.1:${apiPort}/fixture-state`);return r.ok?r.json():null;};
 try{
  await connect(false);const old=await fixture();await navigate('http://127.0.0.1:7792/fixture');const s=await until(async()=>{const x=await fixture();return x?.marker.instance!==old?.marker.instance&&x;});
  const target={'neko-docker':'docker-neko','neko-relay':'vm-relay-neko','neko-nat':'vm-nat-neko'}[v.id],r=spawnSync('python3',['artifacts/independent-qa/ux/guest-observe.py','--target',target,'--handoff'],{encoding:'utf8',timeout:15000});assert.equal(r.status,0,r.stderr);const w=JSON.parse(r.stdout).observation.windows.find(x=>/SidekicksBrowser|firefox/i.test(x.wmClass.join(' '))),rect=s.layout.typing;
  await input([{type:'click',x:Math.round(w.client.x+(w.client.width-s.viewport.width)/2+rect.x+rect.width/2),y:Math.round(w.client.y+w.client.height-s.viewport.height+rect.y+rect.height/2)}]);await sleep(150);await chord(['Control','a']);await page.evaluate(()=>__sidekicksNeko.release());
  await connect(true);await page.locator('#lan-keyboard-toggle').click();assert.equal(await page.evaluate(()=>document.activeElement===document.querySelector('#media textarea')),true);
  await page.keyboard.type('lanphone123',{delay:25});await until(async()=>(await fixture())?.text==='lanphone123');
  report.checks.push('native viewer decoded, human lease transferred, Keyboard button focuses official SDK textarea, real guest text exact');
  await page.evaluate(()=>__sidekicksMobile.chord(['Control','a']));await sleep(150);await page.keyboard.type('native456',{delay:25});await until(async()=>(await fixture())?.text==='native456');
  report.checks.push('mobile extra Ctrl+A uses native data channel; exact replacement reaches guest');
  const stats=await page.evaluate(()=>__sidekicksNeko.stats()),transport=stats.find(x=>x.type==='transport'&&x.selectedCandidatePairId),pair=stats.find(x=>x.id===transport.selectedCandidatePairId),remote=stats.find(x=>x.id===pair.remoteCandidateId);assert.equal(remote.protocol,'udp');assert.equal(remote.address,config.advertise);report.selectedRemote={address:remote.address,port:remote.port,protocol:remote.protocol};
  await page.screenshot({path:`${out}/${v.id}-native-mobile.png`});report.status='PASS';
 }catch(e){report.status='FAIL';report.error=e.stack;process.exitCode=1;await page.screenshot({path:`${out}/${v.id}-native-failure.png`}).catch(()=>{});}
 finally{
  await page.evaluate(()=>__sidekicksNeko?.lease?__sidekicksNeko.release():null).catch(()=>{});
  try{await connect(false);await navigate('http://127.0.0.1:7792/home');await page.evaluate(()=>__sidekicksNeko.release());report.restoredHome=true;}catch(e){report.cleanupError=e.message;process.exitCode=1;}
  await context.close();report.finishedAt=new Date().toISOString();await fs.writeFile(`${out}/${v.id}-native-smoke.json`,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }
}}finally{await browser.close();}
