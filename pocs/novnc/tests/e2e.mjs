import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../../..');
const out=path.join(root,'artifacts/novnc');await fs.mkdir(out,{recursive:true});
const base='http://127.0.0.1:7901';
const result={startedAt:new Date().toISOString(),kind:'automation in isolated headless Chrome; no physical human input',checks:[],measurements:{},consoleErrors:[]};
function check(name,data={}){result.checks.push({name,status:'PASS',...data});console.log('PASS',name)}
async function req(route,body,expected=200){const r=await fetch(base+route,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{});const t=await r.text();assert.equal(r.status,expected,route+': '+t);try{return JSON.parse(t)}catch{return t}}
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,timeout=5000){const start=Date.now();while(Date.now()-start<timeout){const v=await fn();if(v)return v;await sleep(40)}throw new Error('timed out waiting for observed fixture state')}
async function snap(){return (await req('/fixture-events?latest=1')).filter(e=>e.type==='snapshot').at(-1)}
function center(s,id){const r=s.geometry.rects[id],o=s.geometry.offset;return{x:o.x+r.x+r.width/2,y:o.y+r.y+r.height/2}}
function summarize(samples){const s=[...samples].sort((a,b)=>a-b),q=p=>s[Math.ceil(p*s.length)-1];return{n:s.length,medianMs:q(.5),p95Ms:q(.95),maxMs:s.at(-1),minMs:s[0],samplesMs:samples}}
const nativeInput=()=>JSON.parse(execFileSync('docker',['exec','sidekicks-novnc','python3','/app/input_state.py'],{encoding:'utf8'}));
let browser;
try{
  await req('/control',{action:'stop',owner:'agent',session:'test-reset'});
  browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',args:['--disable-background-networking','--no-default-browser-check']});
  const context=await browser.newContext({viewport:{width:1440,height:1040},deviceScaleFactor:1});
  const page=await context.newPage();page.on('pageerror',e=>result.consoleErrors.push(e.message));
  await page.goto(base);await page.waitForFunction(()=>window.sidekicks?.connected);await until(async()=>!!(await snap()));
  const initial=await snap();assert(initial.marker.startsWith('SK-'));
  await page.waitForFunction(()=>window.sidekicks.rfb.getImageData().data.some(v=>v>0));
  const dimensions=await page.evaluate(()=>{const i=window.sidekicks.rfb.getImageData();return{width:i.width,height:i.height,pixelSum:i.data.reduce((a,v)=>a+v,0)}});
  assert.equal(dimensions.width,1280);assert.equal(dimensions.height,720);assert(dimensions.pixelSum>10000000);check('actual noVNC decoded canvas',dimensions);
  const canvas=page.locator('#screen canvas');
  async function viewerPoint(pt){const b=await canvas.boundingBox();return{x:b.x+pt.x*b.width/1280,y:b.y+pt.y*b.height/720}}
  async function viewerClick(pt,opts={}){const p=await viewerPoint(pt);await page.mouse.click(p.x,p.y,opts)}
  await page.evaluate(()=>window.sidekicks.take());
  const typing=center(initial,'typing'),target=center(initial,'click-target');
  await viewerClick(typing);await page.keyboard.press('Control+a');const text='viewer-'+Date.now();await page.keyboard.type(text,{delay:8});
  await until(async()=>(await snap())?.snapshot.text===text);check('viewer RFB keyboard text',{text});
  await page.keyboard.press('Control+a');await page.keyboard.type('MODIFIED');await page.keyboard.press('Shift+A');
  await until(async()=>(await snap())?.snapshot.text==='MODIFIEDA');check('viewer modifiers and Control+A shortcut');
  await viewerClick(target);await viewerClick(target,{clickCount:2,delay:80});await viewerClick(target,{button:'right'});
  let state=await until(async()=>{const s=await snap();return s.snapshot.counts.double>=1&&s.snapshot.counts.right>=1?s:false});check('viewer click/double-click/right-click',{counts:state.snapshot.counts});
  const drag=center(state,'drag-token'),drop=center(state,'drop-zone'),dp=await viewerPoint(drag),end=await viewerPoint(drop);
  await page.mouse.move(dp.x,dp.y);await page.mouse.down();await page.mouse.move(end.x,end.y,{steps:12});await page.mouse.up();
  state=await until(async()=>{const s=await snap();return s.snapshot.counts.drop>=1?s:false});check('viewer drag/drop and release',{drops:state.snapshot.counts.drop});
  const scroll=await viewerPoint(center(state,'scroll-box'));await page.mouse.move(scroll.x,scroll.y);await page.mouse.wheel(0,240);
  state=await until(async()=>{const s=await snap();return s.snapshot.scroll.y>0?s:false});await sleep(200);await page.mouse.wheel(240,0);
  state=await until(async()=>{const s=await snap();return s.snapshot.scroll.x>0?s:false});check('viewer vertical/horizontal wheel, each axis asserted',{scroll:state.snapshot.scroll});
  // Check server enforcement independently of the cooperative viewOnly property.
  await page.evaluate(()=>window.sidekicks.release());
  const pre=await req('/metrics');
  await page.evaluate(()=>{const s=window.sidekicks;s.rfb.viewOnly=false;s.rfb.sendKey(0x61,'KeyA',true);s.rfb.sendKey(0x61,'KeyA',false)});
  await until(async()=>(await req('/metrics')).counters.viewer_input_dropped>=(pre.counters.viewer_input_dropped||0)+2);check('unleased raw viewer key packets blocked at gateway');
  // Independent normalized X11 automation path.
  const as='test-agent',agent=await req('/control',{action:'acquire',owner:'agent',session:as,leaseMs:30000});
  const send=events=>req('/input',{session:as,token:agent.token,events});
  await send([{type:'click',...typing},{type:'key',key:'Control',action:'down'},{type:'key',key:'a',action:'down'},{type:'key',key:'a',action:'up'},{type:'key',key:'Control',action:'up'},{type:'text',text:'agent-route'},{type:'releaseAll'}]);
  await until(async()=>(await snap()).snapshot.text==='agent-route');check('normalized automation keyboard/modifiers/text path');
  const before=(await snap()).snapshot.counts,beforeScroll=(await snap()).snapshot.scroll;
  await send([{type:'click',...target,count:2},{type:'click',...target,button:'right'},{type:'scroll',...center(state,'scroll-box'),deltaY:120,deltaX:120}]);
  await until(async()=>{const s=(await snap()).snapshot,c=s.counts;return c.double>before.double&&c.right>before.right&&s.scroll.x>beforeScroll.x&&s.scroll.y>beforeScroll.y});check('normalized double/right click and both wheel axes');
  // Preempt while held key+button and while a long text request has remaining commands.
  await send([{type:'click',...typing},{type:'key',key:'Shift',action:'down'},{type:'pointer',...typing,action:'down',button:'left'}]);
  result.heldBefore=nativeInput();assert(result.heldBefore.keys.Shift_L);assert(result.heldBefore.buttons['1']);
  const queued=req('/input',{session:as,token:agent.token,events:[{type:'text',text:'z'.repeat(4096)},{type:'text',text:'MUST-NOT-RUN'}]},409).then(()=>true).catch(e=>({error:e.message}));
  const takeoverStart=performance.now();await page.evaluate(()=>window.sidekicks.take());const takeoverMs=performance.now()-takeoverStart;
  const takeoverAck=Date.now(),injectionCutoff=(await req('/control')).lastTransition;result.takeoverAckAt=new Date(takeoverAck).toISOString();result.injectionCutoff=injectionCutoff;
  assert.equal(await queued,true);
  result.heldAfter=nativeInput();assert(!result.heldAfter.keys.Shift_L);assert(!result.heldAfter.buttons['1']);
  await req('/input',{session:as,token:agent.token,events:[{type:'text',text:'DENIED'}]},409);
  await req('/control',{action:'acquire',owner:'agent',session:'competing-agent',leaseMs:30000},409);
  await sleep(300);state=await snap();assert(!state.snapshot.text.includes('MUST-NOT-RUN'));assert(!state.snapshot.text.includes('DENIED'));assert.equal(state.snapshot.heldKeys.length,0);
  check('human takeover cancels queued bot work and rejects stale bot',{takeoverRequestMs:takeoverMs,heldKeys:state.snapshot.heldKeys,nativeBefore:result.heldBefore,nativeAfter:result.heldAfter});
  // Restart typing via viewer proves modifiers released, not just a paused label.
  await viewerClick(typing);await page.keyboard.press('Control+a');await page.keyboard.type('lowercase');
  await until(async()=>(await snap()).snapshot.text==='lowercase');check('held Shift/button released during takeover');
  await sleep(650);const settled=await snap();assert.equal(settled.snapshot.text,'lowercase');const laterQ=settled.snapshot.events.filter(e=>e.type==='text-input'&&Date.parse(e.at)>takeoverAck&&/z/.test(e.value));assert.equal(laterQ.length,0);check('paced takeover: no observed bot text after ACK or human intermix',{observationMs:650,injectionCutoff,applicationQueueFlushGuarantee:false});
  // Same browser monotonic clock: trusted viewer mousedown -> first changed decoded marker pixel.
  const samples=[],intervals=[];
  for(const preset of ['text','motion']){
    await page.selectOption('#preset',preset);await page.evaluate(()=>window.sidekicks.rfb.focus());await sleep(300);
    const times=[];let failures=0;
    for(let i=0;i<35;i++){
      const current=await snap(),r=current.geometry.rects['frame-probe'],o=current.geometry.offset,px=Math.round(o.x+r.x+8),py=Math.round(o.y+r.y+8);
      await page.evaluate(({px,py})=>{const canvas=document.querySelector('#screen canvas');let start=null,last=null,polls=[],old=window.sidekicks.rfb.getImageData();const offset=(py*old.width+px)*4,base=Array.from(old.data.slice(offset,offset+3));window.__frameResult=new Promise(resolve=>{canvas.addEventListener('mousedown',()=>{start=performance.now();requestAnimationFrame(poll)},{once:true,capture:true});function poll(){const t=performance.now();if(last!==null)polls.push(t-last);last=t;const im=window.sidekicks.rfb.getImageData(),pixel=Array.from(im.data.slice(offset,offset+3));if(pixel.some((v,j)=>Math.abs(v-base[j])>150)){resolve({ms:t-start,polls,pixel,base});return}if(t-start>2500){resolve({failed:true,polls,pixel,base});return}requestAnimationFrame(poll)}})}, {px,py});
      await viewerClick(target);const measured=await page.evaluate(()=>window.__frameResult);
      if(measured.failed)failures++;else times.push(measured.ms);intervals.push(...measured.polls);
      await sleep(70);
    }
    assert(times.length>=30,`${preset} decoded frame samples: ${times.length}`);
    result.measurements[preset]={...summarize(times),failures,method:'trusted browser mousedown timestamp to first changed marker pixel in noVNC RFB.getImageData(), requestAnimationFrame polling; same performance.now clock; synthetic click workload, not physical-human latency'};
    check(`${preset} decoded-frame measurement`,{n:times.length,failures});
  }
  result.measurements.polling=summarize(intervals);
  await page.screenshot({path:path.join(out,'viewer.png'),fullPage:true});
  const captured=await fetch(base+'/screenshot');assert.equal(captured.status,200);const png=Buffer.from(await captured.arrayBuffer());await fs.writeFile(path.join(out,'desktop.png'),png);
  result.capture={headers:Object.fromEntries([...captured.headers].filter(([k])=>k.startsWith('x-'))),bytes:png.length,marker:(await snap()).marker};assert(png.length>10000);check('fresh independent desktop screenshot endpoint',result.capture);
  const captures=[];
  for(let i=0;i<30;i++){const start=performance.now(),r=await fetch(base+'/screenshot?maxWidth=640&maxHeight=360'),b=await r.arrayBuffer();assert.equal(r.status,200);captures.push({totalMs:performance.now()-start,captureMs:Number(r.headers.get('x-capture-ms')),captureEncodeMs:Number(r.headers.get('x-capture-encode-ms')),bytes:b.byteLength});if(i===0)await fs.writeFile(path.join(out,'desktop-640.png'),Buffer.from(b))}
  result.measurements.screenshot={requestTotal:summarize(captures.map(v=>v.totalMs)),serverCapture:summarize(captures.map(v=>v.captureMs)),serverCaptureEncode:summarize(captures.map(v=>v.captureEncodeMs)),samples:captures,source:'scrot root-window capture + Pillow PNG resize/encode; transport timing measured separately on host'};check('30 screenshot capture samples');
  // Resized viewer must retain coordinate mapping (remote framebuffer stays fixed).
  await page.setViewportSize({width:1000,height:800});await sleep(500);result.resizeDebug={control:await req('/control'),canvas:await canvas.boundingBox(),target,mapped:await viewerPoint(target),viewOnly:await page.evaluate(()=>sidekicks.rfb.viewOnly)};await page.screenshot({path:path.join(out,'resized-viewer.png')});const count=(await snap()).snapshot.counts.click;await viewerClick(target);await until(async()=>(await snap()).snapshot.counts.click>count);check('viewer scaling after resize retains desktop coordinate mapping');
  const cornerState=await snap(),offset=cornerState.geometry.offset,vp=cornerState.snapshot.viewport;const corners=[];
  for(const [x,y] of [[4,4],[vp.width-20,4],[4,vp.height-20],[vp.width-20,vp.height-20]]){await viewerClick({x:x+offset.x,y:y+offset.y});const observed=await until(async()=>{const s=await snap();return s.lastPointer&&Math.abs(s.lastPointer.x-x)<3&&Math.abs(s.lastPointer.y-y)<3?s.lastPointer:false});corners.push({requested:{x,y},observed})}check('four fixture viewport corners map after viewer resize',{corners});
  // Second view can decode while first holds lease; hostile second sends are rejected.
  const other=await context.newPage();await other.goto(base);await other.waitForFunction(()=>window.sidekicks?.connected);await other.waitForFunction(()=>window.sidekicks.rfb.getImageData().data.some(v=>v>0));const image=await other.evaluate(()=>({w:sidekicks.rfb.getImageData().width,h:sidekicks.rfb.getImageData().height}));assert.equal(image.w,1280);
  await page.bringToFront();if(!(await page.evaluate(()=>!!sidekicks.lease)))await page.evaluate(()=>sidekicks.take());const beforeOther=(await req('/metrics')).counters.viewer_input_dropped;
  await other.evaluate(()=>{sidekicks.rfb.viewOnly=false;sidekicks.rfb.sendKey(0x62,'KeyB',true);sidekicks.rfb.sendKey(0x62,'KeyB',false)});await sleep(150);check('two viewers decode; second viewer cannot bypass ownership',{secondDimensions:image});
  assert((await req('/metrics')).counters.viewer_input_dropped>=beforeOther+2);
  await other.close();await page.bringToFront();
  // Reconnect leaves no human ownership; a new control acquisition is required.
  await page.keyboard.down('Shift');await sleep(50);assert(nativeInput().keys.Shift_L);await page.evaluate(()=>window.sidekicks.reconnect());await page.waitForFunction(()=>window.sidekicks.connected);await page.waitForFunction(()=>window.sidekicks.rfb.getImageData().data.some(v=>v>0));await until(async()=>(await req('/control')).owner===null);assert(!nativeInput().keys.Shift_L);await page.keyboard.up('Shift');check('disconnect releases held key; reconnect restores live stream without ownership');
  await page.evaluate(()=>sidekicks.take());await page.keyboard.down('Shift');await sleep(50);assert(nativeInput().keys.Shift_L);await page.evaluate(()=>window.dispatchEvent(new Event('blur')));await until(async()=>(await req('/control')).owner===null);assert(!nativeInput().keys.Shift_L);await page.keyboard.up('Shift');check('simulated viewer window blur releases lease and actual held key');
  const exp=await req('/control',{action:'acquire',owner:'agent',session:'expiry-test',leaseMs:1000});await req('/input',{session:'expiry-test',token:exp.token,events:[{type:'key',key:'Shift',action:'down'}]});await sleep(1300);assert.equal((await req('/control')).owner,null);await until(async()=>(await snap()).snapshot.heldKeys.length===0);check('expired lease releases held key');
  const rejection=await req('/control',{action:'acquire',owner:'agent',session:'rejection-test',leaseMs:10000});await req('/input',{session:'rejection-test',token:rejection.token,events:[{type:'key',key:'Shift',action:'down'},{type:'pointer',...typing,action:'down',button:'left'}]});assert(nativeInput().keys.Shift_L);
  await req('/input',{session:'foreign',token:'invalid',events:[{type:'unsupported'}]},422);assert(nativeInput().keys.Shift_L);assert(nativeInput().buttons['1']);
  await req('/input',{session:'rejection-test',token:rejection.token,events:[{type:'unsupported'}]},422);const rejectedState=nativeInput();assert(!rejectedState.keys.Shift_L);assert(!rejectedState.buttons['1']);assert.equal((await req('/control')).owner,null);check('own invalid input releases actual held key/button; foreign rejection preserves owner',rejectedState);
  const cross=await fetch(base+'/control',{method:'POST',headers:{'Origin':'https://evil.example','Content-Type':'application/json'},body:JSON.stringify({action:'acquire',owner:'human',session:'evil'})});assert.equal(cross.status,403);check('non-loopback browser Origin rejected');
  result.metrics=await req('/metrics');result.finalFixture=await snap();result.browserVersion=await browser.version();result.finishedAt=new Date().toISOString();result.status='PASS';
}catch(e){result.status='FAIL';result.error=e.stack;console.error(e);process.exitCode=1}finally{if(browser)await browser.close();await fs.writeFile(path.join(out,'e2e.json'),JSON.stringify(result,null,2));}
