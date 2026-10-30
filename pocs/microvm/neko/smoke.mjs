import {chromium} from '../../neko/node_modules/playwright/index.mjs';
import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
const output=new URL('../artifacts/neko-udp/post-restart-stream.json',import.meta.url);
const report={startedAt:new Date().toISOString(),checks:[]};
let browser;
try{
 browser=await chromium.launch({channel:'chrome',headless:true,args:['--autoplay-policy=no-user-gesture-required','--disable-background-networking']});
 const page=await browser.newPage({viewport:{width:1440,height:1000}});
 await page.goto('http://127.0.0.1:7906/?mode=native');
 await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth===1280,{},{timeout:30000});
 await page.waitForFunction(()=>window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>3,{},{timeout:15000});
 const before=await page.evaluate(()=>{const v=__sidekicksNeko.client.transport.element;return{width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames,currentTime:v.currentTime};});
 await page.waitForTimeout(2000);
 const after=await page.evaluate(async()=>{const n=__sidekicksNeko,v=n.client.transport.element,stats=await n.stats(),t=stats.find(x=>x.type==='transport'&&x.selectedCandidatePairId),p=stats.find(x=>x.id===t.selectedCandidatePairId),r=stats.find(x=>x.id===p.remoteCandidateId);return{width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames,currentTime:v.currentTime,dtls:t.dtlsState,pair:{state:p.state,bytesReceived:p.bytesReceived,bytesSent:p.bytesSent},remote:{protocol:r.protocol,address:r.address,port:r.port},inbound:stats.filter(x=>x.type==='inbound-rtp').map(x=>({kind:x.kind,framesDecoded:x.framesDecoded,framesDropped:x.framesDropped,packetsLost:x.packetsLost,jitter:x.jitter,bytesReceived:x.bytesReceived}))};});
 assert(after.frames>before.frames);assert.equal(after.remote.protocol,'udp');assert.equal(after.remote.address,'127.0.0.1');assert(after.remote.port>=59120&&after.remote.port<=59139);assert.equal(after.dtls,'connected');assert.equal(after.pair.state,'succeeded');
 report.checks.push({name:'fresh browser decodes increasing video over selected UDP after VM restart',status:'PASS',before,after});
 const shot=await fetch('http://127.0.0.1:7796/screenshot?maxWidth=640&maxHeight=360');assert.equal(shot.status,200);
 const bytes=Buffer.from(await shot.arrayBuffer());await fs.writeFile(new URL('../artifacts/neko-udp/post-restart-capture.jpg',import.meta.url),bytes);
 report.checks.push({name:'fresh post-restart bounded capture',status:'PASS',bytes:bytes.length,source:shot.headers.get('x-capture-source')});
 report.browser=await browser.version();report.status='PASS';
}catch(e){report.status='FAIL';report.error=e.stack;process.exitCode=1;}
finally{await browser?.close();report.finishedAt=new Date().toISOString();await fs.writeFile(output,JSON.stringify(report,null,2));console.log(JSON.stringify(report));}
