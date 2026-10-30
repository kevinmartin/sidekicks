import {chromium} from '../../neko/node_modules/playwright/index.mjs';
import fs from 'node:fs/promises';
import assert from 'node:assert/strict';

// Read-only viewer smoke: no lease, keyboard, pointer or lifecycle mutations.
const output=new URL('../artifacts/ux/stream-smoke.json',import.meta.url);
const report={startedAt:new Date().toISOString(),scope:'Fresh decoded streams and workbench rendering only; independent interaction QA separate',checks:[]};
let browser;
try {
 browser=await chromium.launch({channel:'chrome',headless:true,args:['--disable-background-networking','--autoplay-policy=no-user-gesture-required']});
 for(const [name,port,neko] of [['a',7903,false],['neko',7906,true],['nat',7907,true]]) {
  const page=await browser.newPage({viewport:{width:1440,height:1000}});
  await page.goto(`http://127.0.0.1:${port}/${neko?'?mode=native':''}`);
  let decoded;
  if(neko){
   await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth===1280&&__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>3,{},{timeout:30000});
   const before=await page.evaluate(()=>__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames);
   await page.waitForTimeout(800);
   decoded=await page.evaluate(async()=>{
    const n=__sidekicksNeko,v=n.client.transport.element,s=await n.stats(),t=s.find(x=>x.type==='transport'&&x.selectedCandidatePairId),p=s.find(x=>x.id===t?.selectedCandidatePairId),r=s.find(x=>x.id===p?.remoteCandidateId);
    return {width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames,dtls:t?.dtlsState,pairState:p?.state,remote:{protocol:r?.protocol,address:r?.address,port:r?.port},lease:!!n.lease};
   });
   assert(decoded.frames>before);assert.equal(decoded.remote.protocol,'udp');assert.equal(decoded.pairState,'succeeded');assert.equal(decoded.dtls,'connected');assert.equal(decoded.lease,false);
   if(name==='neko'){assert.equal(decoded.remote.address,'127.0.0.1');assert(decoded.remote.port>=59120&&decoded.remote.port<=59139);}
   else {assert.notEqual(decoded.remote.address,'127.0.0.1');assert(decoded.remote.port>=59220&&decoded.remote.port<=59239);}
   decoded.beforeFrames=before;
  }else{
   await page.waitForFunction(()=>window.sidekicks?.connected&&sidekicks.rfb.getImageData().data.some(x=>x>0),{},{timeout:30000});
   decoded=await page.evaluate(()=>{const im=sidekicks.rfb.getImageData();return{width:im.width,height:im.height,pixelSum:im.data.reduce((a,v)=>a+v,0),lease:!!sidekicks.lease};});
   assert.equal(decoded.lease,false);assert(decoded.pixelSum>10000000);
  }
  assert.equal(decoded.width,1280);assert.equal(decoded.height,720);
  await page.screenshot({path:new URL(`../artifacts/ux/${name}-decoded-viewer.png`,import.meta.url).pathname,fullPage:true});
  report.checks.push({name:`${name}: fresh decoded ${neko?'WebRTC UDP':'RFB'} stream`,status:'PASS',decoded});
  await page.close();
 }
 const page=await browser.newPage({viewport:{width:1440,height:1100}});
 await page.goto('http://127.0.0.1:7790/');
 await page.waitForFunction(()=>document.querySelectorAll('#adapters .card').length===2);
 const links=await page.locator('[data-runtime]').evaluateAll(xs=>xs.map(x=>({id:x.dataset.runtime,href:x.href})));
 assert.equal(links.length,5);assert.deepEqual(links.map(x=>new URL(x.href).port),['7901','7902','7903','7906','7907']);
 const text=await page.locator('body').innerText();assert(text.includes('historical pre-UX'));assert(text.includes('no autonomous agents or model execution'));
 await page.screenshot({path:new URL('../../../artifacts/workbench/ux-current.png',import.meta.url).pathname,fullPage:true});
 report.checks.push({name:'current workbench renders five exact runtime links and bounded claims',status:'PASS',links});
 report.status='PASS';report.browser=await browser.version();
}catch(error){report.status='FAIL';report.error=error.stack;process.exitCode=1;}
finally{await browser?.close();report.finishedAt=new Date().toISOString();await fs.writeFile(output,JSON.stringify(report,null,2));console.log(JSON.stringify(report));}
