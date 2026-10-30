import {chromium} from 'playwright';
import fs from 'node:fs/promises';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile), base='http://127.0.0.1:17802', native=process.argv.includes('--native');
const stamp=new Date().toISOString().replace(/[:.]/g,'-'), out=new URL(`./artifacts/xfce-stage/${native?'native':'gated'}-smoke-${stamp}/`,import.meta.url);
await fs.mkdir(out,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true}),page=await browser.newPage({viewport:{width:1440,height:1000}});
const report={mode:native?'native':'gated',checks:[],status:'FAIL'};
try{
 await page.goto(base+'/'+(native?'?mode=native':''));
 await page.waitForFunction(()=>window.__sidekicksNeko?.client.transport.element.videoWidth===1280&&window.__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>5,{},{timeout:30000});
 report.video=await page.evaluate(()=>{const v=window.__sidekicksNeko.client.transport.element;return{width:v.videoWidth,height:v.videoHeight,decodedFrames:v.getVideoPlaybackQuality().totalVideoFrames};});
 const source=await fs.readFile(new URL('../../../artifacts/independent-qa/ux/x11-observe.py',import.meta.url),'utf8').catch(()=>null);
 if(source)report.x11=JSON.parse((await exec('docker',['exec','--user','neko','sidekicks-neko-xfce-stage','python3','-c',source],{maxBuffer:3e6})).stdout);
 report.readiness=await(await fetch(base+'/desktop/state')).json();
 await page.screenshot({path:new URL('viewer.png',out).pathname});
 await fs.writeFile(new URL('desktop.jpg',out),Buffer.from(await(await fetch(base+'/screenshot')).arrayBuffer()));
 report.status='PASS';
}catch(e){report.error=e.stack;process.exitCode=1;await page.screenshot({path:new URL('failure.png',out).pathname}).catch(()=>{});}
finally{await browser.close();await fs.writeFile(new URL('report.json',out),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({status:report.status,artifact:out.pathname,video:report.video}));}
