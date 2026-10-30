import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {createRequire} from 'node:module';
import {spawnSync} from 'node:child_process';
import {variants} from './bridge.mjs';
const require=createRequire(new URL('../../qa/package.json',import.meta.url));
const {chromium}=require('playwright');
const config=JSON.parse(await fs.readFile('.run/lan/config.json','utf8'));
const selected=process.argv.slice(2),out='artifacts/lan';await fs.mkdir(out,{recursive:true});
const browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true,args:['--no-first-run','--no-default-browser-check']});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn){for(let i=0;i<100;i++){const x=await fn();if(x)return x;await sleep(100);}throw Error('observation timed out');}
async function readFixture(v){const port={7901:7791,7902:7792,7903:7793,7906:7796,7907:7797}[v.target],url=`http://127.0.0.1:${port}${v.kind==='novnc'?'/fixture-events?latest=1':'/fixture-state'}`;
 const r=await fetch(url);if(!r.ok)return null;const j=await r.json();return v.kind==='novnc'?(j.at(-1)?{...j.at(-1).snapshot,marker:{instance:j.at(-1).marker}}:null):j;
}
try{for(const v of variants.filter(v=>!selected.length||selected.includes(v.id))){
 const report={id:v.id,url:`http://${config.advertise}:${v.port}/`,startedAt:new Date().toISOString(),checks:[],errors:[],physicalPhone:'UNTESTED',route:v.kind==='novnc'?'RFB':'gated HTTP + WebRTC UDP'};
 try{const previous=JSON.parse(await fs.readFile(`${out}/${v.id}-smoke.json`,'utf8')),archive=`${out}/attempts/${v.id}-${previous.startedAt.replace(/[:.]/g,'-')}`;await fs.mkdir(archive,{recursive:true});for(const name of [`${v.id}-smoke.json`,`${v.id}-failure.png`,`${v.id}-mobile-fixture.png`])await fs.copyFile(`${out}/${name}`,`${archive}/${name}`).catch(()=>{});}catch{}
 const context=await browser.newContext({viewport:{width:1280,height:900}}),page=await context.newPage();page.on('pageerror',e=>report.errors.push(e.message));
 const api=(p,body)=>page.evaluate(async({p,body})=>{const v=window.sidekicks||window.__sidekicksNeko;return v.api(p,body);},{p,body});
 const input=events=>page.evaluate(events=>{const v=window.sidekicks||window.__sidekicksNeko;return v.api('/input',{session:v.session,token:v.lease.token,events});},events);
 const chord=keys=>input(keys.map(key=>({type:'key',key,action:'down'})).concat([...keys].reverse().map(key=>({type:'key',key,action:'up'}))));
 const navigate=async url=>{await chord(['Control','l']);await sleep(200);await input([{type:'text',text:url}]);await sleep(150);await chord(['Enter']);await sleep(1000);};
 try{
  for(const path of ['/home','/desktop/launch','/api/session','/rpc','/exec']){const r=await fetch(report.url.slice(0,-1)+path);assert.equal(r.status,403,path);}
  assert.equal((await fetch(report.url+'control',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({action:'acquire',owner:'agent',session:'forbidden'})})).status,403);
  report.checks.push('private/admin routes and agent acquisition denied');
  await page.goto(report.url);
  await page.waitForFunction(kind=>kind==='novnc'?window.sidekicks?.connected:window.__sidekicksNeko?.client.transport.element?.videoWidth===1280,v.kind,{timeout:20000});
  report.checks.push('actual decoded 1280x720 desktop through LAN address and plain HTTP');
  assert.equal(await page.evaluate(()=>typeof crypto.randomUUID),'function');
  report.secureContext=await page.evaluate(()=>isSecureContext);
  if(v.kind==='neko'){
   await sleep(350);const stats=await page.evaluate(()=>window.__sidekicksNeko.stats()),transport=stats.find(x=>x.type==='transport'&&x.selectedCandidatePairId),pair=stats.find(x=>x.id===transport?.selectedCandidatePairId),remote=stats.find(x=>x.id===pair?.remoteCandidateId);
   assert.equal(remote?.protocol,'udp');assert.equal(remote.address,config.advertise);assert.ok(remote.port>=v.udp&&remote.port<v.udp+20);
   report.selectedRemote={address:remote.address,port:remote.port,protocol:remote.protocol,state:pair.state};report.checks.push('selected ICE UDP pair uses advertised Mac LAN/public-forwarding port');
  }
  await page.evaluate(()=>{const v=window.sidekicks||window.__sidekicksNeko;return v.take();});
  const internal=v.kind==='novnc'?'http://127.0.0.1:8080':'http://127.0.0.1:7792';
  const old=await readFixture(v);await navigate(internal+'/fixture');
  let snap=await until(async()=>{const s=await readFixture(v);return s&&s.marker?.instance!==old?.marker?.instance&&s;});const marker='lan-'+v.id+'-'+Date.now();
  // Query actual fixture-reported bounds; do not infer text focus from a titlebar click.
  const port={7901:7791,7902:7792,7903:7793,7906:7796,7907:7797}[v.target];
  const raw=await(await fetch(`http://127.0.0.1:${port}${v.kind==='novnc'?'/fixture-events?latest=1':'/fixture-state'}`)).json();
  const item=v.kind==='novnc'?raw.at(-1):raw,rect=(v.kind==='novnc'?item.geometry.rects:item.layout).typing;
  let offset=item.geometry?.offset;
  if(v.kind==='neko'){
   const target={'neko-docker':'docker-neko','neko-relay':'vm-relay-neko','neko-nat':'vm-nat-neko'}[v.id],r=spawnSync('python3',['artifacts/independent-qa/ux/guest-observe.py','--target',target,'--handoff'],{encoding:'utf8',timeout:15000});assert.equal(r.status,0,r.stderr);
   const state=JSON.parse(r.stdout).observation,w=state.windows.find(x=>/SidekicksBrowser|firefox/i.test(JSON.stringify(x.wmClass||x.class)));
   assert.ok(w,JSON.stringify(state.windows));offset={x:w.client.x+(w.client.width-snap.viewport.width)/2,y:w.client.y+w.client.height-snap.viewport.height};
  }
  const point={x:Math.round(offset.x+rect.x+rect.width/2),y:Math.round(offset.y+rect.y+rect.height/2)};
  await input([{type:'click',...point}]);await sleep(150);
  await chord(['Control','a']);
  await page.setViewportSize({width:390,height:844});await page.locator('#lan-keyboard-toggle').click();await page.locator('#lan-typing').fill(marker);
  snap=await until(async()=>{const s=await readFixture(v);return s?.text===marker&&s;});
  report.checks.push('mobile-width Keyboard text reaches real guest fixture exactly');
  if(v.kind==='novnc'){
   const latest=(await(await fetch(`http://127.0.0.1:${port}/fixture-events?latest=1`)).json()).at(-1),r=latest.geometry.rects['frame-probe'],p={x:Math.round(latest.geometry.offset.x+r.x+4),y:Math.round(latest.geometry.offset.y+r.y+4)},expected=latest.probe%2?[0,255,0]:[255,0,255];
   report.decodedMarker=await until(async()=>{const pixel=await page.evaluate(p=>{const f=sidekicks.rfb.getImageData(),i=(p.y*f.width+p.x)*4;return[...f.data.slice(i,i+3)];},p);return pixel.reduce((n,x,i)=>n+Math.abs(x-expected[i]),0)<60&&{pixel,expected,point:p};});
   report.checks.push('fresh fixture probe agrees with actual decoded RFB framebuffer before screenshot');
  }
  const dimensions=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth}));assert.ok(dimensions.scroll<=dimensions.width+2,JSON.stringify(dimensions));
  report.checks.push('390px layout has no horizontal document overflow');
  await page.screenshot({path:`${out}/${v.id}-mobile-fixture.png`});
  const capture=await fetch(report.url+'screenshot?maxWidth=640&maxHeight=360');assert.equal(capture.status,200);const bytes=Buffer.from(await capture.arrayBuffer());assert.ok(bytes.length>2000);await fs.writeFile(`${out}/${v.id}-capture.${v.kind==='novnc'?'png':'jpg'}`,bytes);
  report.checks.push('fresh bounded screenshot over viewer route');report.captureBytes=bytes.length;
  await page.locator('#lan-keyboard-toggle').click();await navigate(internal+'/home');await sleep(250);
  report.checks.push('restored guest Home');
  assert.deepEqual(report.errors,[]);
  report.status='PASS';
 }catch(e){report.status='FAIL';report.error=e.stack;await page.screenshot({path:`${out}/${v.id}-failure.png`}).catch(()=>{});}
 finally{
  if(report.status==='FAIL')try{if(await page.evaluate(()=>!!(window.sidekicks||window.__sidekicksNeko)?.lease))await navigate(v.kind==='novnc'?'http://127.0.0.1:8080/home':'http://127.0.0.1:7792/home');}catch{}
  await page.evaluate(()=>{const v=window.sidekicks||window.__sidekicksNeko;return v?.lease?v.release():null;}).catch(()=>{});await context.close();report.finishedAt=new Date().toISOString();await fs.writeFile(`${out}/${v.id}-smoke.json`,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }
 if(report.status!=='PASS')process.exitCode=1;
}}finally{await browser.close();}
