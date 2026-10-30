import {sanitizeStats,sanitizeLog} from './evidence.mjs';
import {chromium} from 'playwright';import fs from 'node:fs/promises';import assert from 'node:assert/strict';
const native=process.argv.includes('--native'),quality=process.argv.includes('--quality'),variant=quality?'quality':'interactive',route=native?'native-data-channel':'gated-http';
const out=new URL('../../../artifacts/neko/',import.meta.url),result={at:new Date().toISOString(),variant,route,resolution:[1280,720],fpsConfigured:30,samples:[],failures:[],method:'Browser pointerdown capture timestamp to first requestVideoFrameCallback with a changed synchronous fixture click-counter region, same performance.now clock. Actual task-owned Playwright mouse into viewer. No physical hardware or WAN measurement.'};
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--autoplay-policy=no-user-gesture-required','--disable-background-timer-throttling']});const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();
try{
 await page.goto('http://127.0.0.1:7902/'+(native?'?mode=native':''));await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth===1280,{},{timeout:30000});
 await page.evaluate(id=>window.__sidekicksNeko.client.send('signal/video',{selector:{id,type:'exact'}}),variant);await page.waitForTimeout(1200);await page.evaluate(()=>window.__sidekicksNeko.take());await page.waitForTimeout(200);
 const fixture=await(await fetch('http://127.0.0.1:7792/fixture-state')).json();const r=fixture.layout['click-target'],box=await page.locator(native?'#media textarea':'#gated').boundingBox(),point={x:box.x+(r.x+r.width/2)*box.width/fixture.viewport.width,y:box.y+(r.y+r.height/2)*box.height/fixture.viewport.height};
 result.statsBefore=sanitizeStats(await page.evaluate(()=>window.__sidekicksNeko.stats()));
 const probe={x:Math.round(r.x),y:Math.round(r.y+r.height+60),width:Math.min(200,Math.floor(r.width)),height:30};result.probe=probe;
 await page.evaluate(probe=>{
   const v=window.__sidekicksNeko.client.transport.element,c=document.createElement('canvas');c.width=v.videoWidth;c.height=v.videoHeight;const ctx=c.getContext('2d',{willReadFrequently:true});
   const pixels=()=>{ctx.drawImage(v,0,0);return ctx.getImageData(probe.x,probe.y,probe.width,probe.height).data;};
   const b=window.__latency={armed:false,start:0,before:0,samples:[],intervals:[],last:0,pending:false};
   document.getElementById('stage').addEventListener('pointerdown',()=>{if(b.armed){b.start=performance.now();b.before=pixels();b.pending=true;b.armed=false;}},{capture:true});
   const frame=(now,metadata)=>{if(b.last)b.intervals.push(now-b.last);b.last=now;if(b.pending){const data=pixels();let changed=0;for(let j=0;j<data.length;j+=4)if(Math.max(Math.abs(data[j]-b.before[j]),Math.abs(data[j+1]-b.before[j+1]),Math.abs(data[j+2]-b.before[j+2]))>45)changed++;if(changed>=12){b.samples.push({latencyMs:performance.now()-b.start,triggerAt:b.start,observedAt:performance.now(),changedPixels:changed,mediaTime:metadata.mediaTime,presentedFrames:metadata.presentedFrames});b.pending=false;}}v.requestVideoFrameCallback(frame);};v.requestVideoFrameCallback(frame);
 },probe);
 for(let i=0;i<35;i++){
   const wanted=await page.evaluate(()=>{window.__latency.armed=true;return window.__latency.samples.length;});await page.mouse.click(point.x,point.y);
   try{await page.waitForFunction(n=>window.__latency.samples.length>n,wanted,{timeout:3000,polling:20});}catch{result.failures.push(i);await page.evaluate(()=>window.__latency.pending=false);}
   await page.waitForTimeout(130);
 }
 const evidence=await page.evaluate(()=>({samples:window.__latency.samples,frameIntervalsMs:window.__latency.intervals}));Object.assign(result,evidence);result.statsAfter=sanitizeStats(await page.evaluate(()=>window.__sidekicksNeko.stats()));
 const a=result.samples.map(x=>x.latencyMs).sort((a,b)=>a-b),pct=p=>a[Math.min(a.length-1,Math.ceil(a.length*p)-1)];result.summary={count:a.length,failures:result.failures.length,medianMs:pct(.5),p95Ms:pct(.95),maxMs:a.at(-1),frameCallbackIntervalMedianMs:result.frameIntervalsMs.slice().sort((a,b)=>a-b)[Math.floor(result.frameIntervalsMs.length/2)]};
 assert.equal(result.failures.length,0);assert.equal(a.length,35);result.status='PASS';
}catch(e){result.status='FAIL';result.error=e.stack;process.exitCode=1;}
finally{await page.evaluate(()=>window.__sidekicksNeko?.release()).catch(()=>{});await fs.writeFile(new URL(`latency-${route}-${variant}.json`,out),JSON.stringify(result,null,2));await browser.close();console.log(JSON.stringify(result.summary??result));}
