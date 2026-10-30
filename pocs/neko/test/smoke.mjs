import {sanitizeStats,sanitizeLog} from './evidence.mjs';
import {chromium} from 'playwright';
import fs from 'node:fs/promises';
const out=new URL('../../../artifacts/neko/',import.meta.url);
await fs.mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--autoplay-policy=no-user-gesture-required']});
const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage(),logs=[];
page.on('console',m=>logs.push({type:m.type(),text:m.text()}));page.on('pageerror',e=>logs.push({error:e.message}));
try{
 await page.goto('http://127.0.0.1:7902/');
 await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element?.videoWidth>0,{},{timeout:30000});
 await page.waitForFunction(()=>window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>3,{},{timeout:10000});
 const proof=await page.evaluate(async()=>{const n=window.__sidekicksNeko,v=n.client.transport.element;return{state:n.client.state.connection.status,size:{width:v.videoWidth,height:v.videoHeight},playback:v.getVideoPlaybackQuality(),stats:await n.stats()};});
 proof.stats=sanitizeStats(proof.stats);await fs.writeFile(new URL('smoke.json',out),JSON.stringify(proof,null,2));await page.screenshot({path:new URL('viewer-smoke.png',out).pathname,fullPage:true});
 console.log(JSON.stringify({decoded:true,size:proof.size,state:proof.state}));
}finally{await fs.writeFile(new URL('browser-console.json',out),JSON.stringify(sanitizeLog(logs),null,2));await browser.close();}
