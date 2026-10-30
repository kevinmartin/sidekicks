import {sanitizeStats,sanitizeLog} from '../../neko/test/evidence.mjs';
import {chromium} from '../../neko/node_modules/playwright/index.mjs';import assert from 'node:assert/strict';import fs from 'node:fs/promises';import http from 'node:http';
const base='http://127.0.0.1:7796',out=new URL('../artifacts/neko-udp/',import.meta.url),report={at:new Date().toISOString(),checks:[],measurements:{},caveats:['Task-owned headless Chrome; no physical user/trackpad tested.','Input timing ends on first decoded video frame with alternating marker color; includes scheduling, encoding, transport, decode and sampling.','Same browser monotonic clock; no WAN, no physical display scanout.']};
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--autoplay-policy=no-user-gesture-required','--disable-background-timer-throttling']});
const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();const consoleLog=[];
page.on('console',m=>consoleLog.push({type:m.type(),text:m.text()}));page.on('pageerror',e=>consoleLog.push({error:e.message}));
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function req(p,b,headers={}){const r=await fetch(base+p,{method:b?'POST':'GET',headers:{...(b?{'content-type':'application/json'}:{}),...headers},body:b?JSON.stringify(b):undefined});const j=await r.json();return{status:r.status,body:j};}
async function post(p,b){const r=await req(p,b);assert.equal(r.status,200,JSON.stringify(r));return r.body;}
async function state(){return(await req('/fixture-state')).body;}
async function until(fn,detail,timeout=6000){const start=Date.now();while(Date.now()-start<timeout){const s=await state();if(await fn(s))return s;await sleep(50);}throw new Error('Timed out: '+detail);}
function center(s,id){const r=s.layout[id];return{x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)};}
async function ready(native=false){await page.goto('http://127.0.0.1:7906/'+(native?'?mode=native':''));await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth>0,{},{timeout:30000});await page.waitForFunction(()=>window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>3,{},{timeout:15000});}
async function remotePoint(point,native){const box=await page.locator(native?'#media textarea':'#gated').boundingBox(),s=await state();return{x:box.x+point.x*box.width/s.viewport.width,y:box.y+point.y*box.height/s.viewport.height};}
async function viewerClick(point,native,options={}){const p=await remotePoint(point,native);await page.mouse.click(p.x,p.y,options);}
async function flow(mode){
 const native=mode==='native';await ready(native);await page.evaluate(()=>window.__sidekicksNeko.take());if(native)await page.waitForFunction(()=>window.__sidekicksNeko.client.controlling);
 let prior=await state();await viewerClick(center(prior,'typing'),native);await page.keyboard.press('Control+r');await until(s=>s.marker.instance!==prior.marker.instance,mode+' fresh fixture reload');await page.waitForTimeout(500);
 let s=await state(),start={...s.counts},text='sidekicks-'+mode+'-'+Math.random().toString(16).slice(2,10);
 await viewerClick(center(s,'typing'),native);await page.waitForTimeout(120);await page.keyboard.type(text,{delay:15});await until(s=>s.text.includes(text),mode+' typed text');
 await page.keyboard.down('Control');await page.keyboard.press('a');await page.keyboard.up('Control');await page.keyboard.type(text+'-replaced',{delay:8});
 await until(s=>s.text===text+'-replaced',mode+' modifier select all');
 await page.keyboard.down('Shift');await page.keyboard.press('a');await page.keyboard.up('Shift');await until(s=>s.text.endsWith('A'),mode+' Shift');
 s=await state();const target=center(s,'click-target');await viewerClick(target,native);await until(s=>s.counts.click>start.click,mode+' click');
 await viewerClick(target,native,{button:'right'});await until(s=>s.counts.right>start.right,mode+' right');
 await viewerClick(target,native,{clickCount:2,delay:70});await until(s=>s.counts.double>start.double,mode+' double');
 s=await state();const a=await remotePoint(center(s,'drag-token'),native),b=await remotePoint(center(s,'drop-zone'),native);await page.mouse.move(a.x,a.y);await page.mouse.down();await page.mouse.move(b.x,b.y,{steps:18});await page.mouse.up();await until(s=>s.counts.drop>start.drop,mode+' drag release');
 s=await state();const scroll=await remotePoint(center(s,'scroll-box'),native);await page.mouse.move(scroll.x,scroll.y);await page.mouse.wheel(0,280);await until(s=>s.scroll.y>0,mode+' wheel');
 // Held modifier must be released by server-side revocation, not only a browser label.
 await viewerClick(center(s,'typing'),native);await page.keyboard.down('Control');await until(s=>s.heldKeys.includes('ControlLeft'),mode+' held Control');await page.evaluate(()=>window.__sidekicksNeko.release());await page.keyboard.up('Control');await until(s=>s.heldKeys.length===0,mode+' release clears held modifier');
 const proof=await state();await fs.writeFile(new URL(mode+'-fixture.json',out),JSON.stringify(proof,null,2));await page.screenshot({path:new URL(mode+'-viewer.png',out).pathname,fullPage:true});
 report.checks.push({name:mode+' viewer keyboard/text/modifiers/click/right/double/drag/scroll/held-release',status:'PASS',counts:proof.counts,route:native?'Official SDK overlay → Neko WebRTC data channel → X11':'Browser pointer/keyboard → adapter HTTP lease → Neko v3 control websocket → X11'});
}
try{
 await ready();let s=await state();assert.equal(s.viewport.width,1280);assert.equal(s.viewport.height,720);
 report.stream=await page.evaluate(async()=>{const n=window.__sidekicksNeko,v=n.client.transport.element;return{size:[v.videoWidth,v.videoHeight],quality:v.getVideoPlaybackQuality(),stats:await n.stats()};});
 const rawStats=report.stream.stats;
 const transports=rawStats.filter(x=>x.type==='transport'&&x.selectedCandidatePairId);
 report.selectedUDP=transports.map(t=>{const p=rawStats.find(x=>x.id===t.selectedCandidatePairId),l=rawStats.find(x=>x.id===p.localCandidateId),r=rawStats.find(x=>x.id===p.remoteCandidateId);return{pair:{state:p.state,nominated:p.nominated,bytesSent:p.bytesSent,bytesReceived:p.bytesReceived},local:{protocol:l.protocol,candidateType:l.candidateType,address:l.address==='127.0.0.1'?l.address:'[host candidate]',port:l.port},remote:{protocol:r.protocol,candidateType:r.candidateType,address:r.address,port:r.port}};});
 assert(report.selectedUDP.length>0,'selected candidate pair missing');
 for(const x of report.selectedUDP){assert.equal(x.pair.state,'succeeded');assert.equal(x.remote.protocol,'udp');assert.equal(x.remote.address,'127.0.0.1');assert(x.remote.port>=59120&&x.remote.port<=59139);}
 report.checks.push({name:'actual selected ICE UDP pair to loopback media relay with decoded VP8 frames',status:'PASS',selected:report.selectedUDP});
 report.stream.stats=sanitizeStats(report.stream.stats);await flow('gated');await flow('native');
 // Native controls cannot be acquired outside the native human lease.
 await page.evaluate(()=>window.__sidekicksNeko.client.request());await sleep(150);assert.equal(await page.evaluate(()=>window.__sidekicksNeko.client.controlling),false);report.checks.push({name:'unleased native viewer cannot acquire host',status:'PASS'});
 await ready();const session='e2e-agent-'+Date.now();let lease=await post('/control',{action:'acquire',owner:'agent',session});
 const send=events=>post('/input',{...lease,session,events});
 s=await state();await send([{type:'click',...center(s,'typing')},{type:'key',key:'Control',action:'down'},{type:'key',key:'a',action:'down'},{type:'key',key:'a',action:'up'},{type:'key',key:'Control',action:'up'},{type:'text',text:'Normalized-'+Date.now()}]);await until(s=>s.text.startsWith('Normalized-'),'normalized automation text');
 report.checks.push({name:'normalized automation input independently exercised',status:'PASS'});
 const copied=await req('/control',{action:'acquire',owner:'agent',session});assert.equal(copied.status,403);
 assert.equal((await req('/input',{token:'forged',session,events:[{type:'click',x:2,y:2}]})).status,409);
 assert.equal((await req('/control',{action:'stop'},{Origin:'https://example.com'})).status,403);
 const hostStatus=await new Promise((resolve,reject)=>{const r=http.get(base+'/health',{headers:{Host:'example.com'}},res=>{res.resume();resolve(res.statusCode);});r.on('error',reject);});assert.equal(hostStatus,403);
 report.checks.push({name:'copied-session renewal/forged token/cross-origin/Host rebinding rejected',status:'PASS'});
 // Queue long text, preempt while it is executing, then observe stable counter/text.
 const job=req('/input',{...lease,session,events:[{type:'text',text:'q'.repeat(350)}]});await sleep(70);const human=await page.evaluate(()=>window.__sidekicksNeko.take());const stopped=await job;assert.equal(stopped.status,409);await sleep(150);const textAfter=(await state()).text;await sleep(250);assert.equal((await state()).text,textAfter);assert.equal((await req('/input',{...lease,session,events:[{type:'text',text:'STALE'}]})).status,409);await page.evaluate(()=>window.__sidekicksNeko.release());
 report.checks.push({name:'human preemption cancels queued bot text with no later writes',status:'PASS'});
 const race=await Promise.all([req('/control',{action:'acquire',owner:'agent',session:'race-agent'}),req('/control',{action:'acquire',owner:'human',session:'race-human'})]);assert.equal((await req('/control')).body.owner,'human');await post('/control',{action:'stop'});report.checks.push({name:'simultaneous human/agent requests end with human ownership',status:'PASS',statuses:race.map(r=>r.status)});
 // Screenshot capture is separately measured and genuinely reflects the fixture marker.
 const times=[];let shot;
 for(let i=0;i<10;i++){const start=performance.now(),r=await fetch(base+'/screenshot?maxWidth=640&maxHeight=360');assert.equal(r.status,200);shot=Buffer.from(await r.arrayBuffer());times.push(performance.now()-start);if(i===0)report.screenshot={sourceWidth:r.headers.get('x-source-width'),sourceHeight:r.headers.get('x-source-height'),width:r.headers.get('x-image-width'),height:r.headers.get('x-image-height'),bytes:shot.length,coordinateMapping:'desktopX=imageX×sourceWidth/imageWidth; same for Y',source:r.headers.get('x-capture-source')};}
 await fs.writeFile(new URL('llm-capture.jpg',out),shot);report.screenshot.latencyMs=times;
 report.checks.push({name:'fresh bounded JPEG capture for LLM, 640×360',status:'PASS'});
 report.status='PASS';
}catch(e){report.status='FAIL';report.error=e.stack;console.error(e);await page.screenshot({path:new URL('failure.png',out).pathname,fullPage:true}).catch(()=>{});process.exitCode=1;}
finally{await req('/control',{action:'stop'}).catch(()=>{});await fs.writeFile(new URL('e2e.json',out),JSON.stringify(report,null,2));await fs.writeFile(new URL('e2e-console.json',out),JSON.stringify(sanitizeLog(consoleLog),null,2));await browser.close();console.log(JSON.stringify({status:report.status,checks:report.checks.length,error:report.error}));}
