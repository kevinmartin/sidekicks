// Read-only decoded-stream/capture check for the two fixed disposable stages.
// Never acquires a lease, injects input, launches guest apps or touches user tabs.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {chromium} from '../../novnc/tests/node_modules/playwright/index.mjs';

const target=process.argv[2];
const config={novnc:{port:17803,api:17793},nat:{port:17807,api:17797}}[target];
assert(config,'Usage: node pocs/microvm/polish/view.mjs novnc|nat');
const root=new URL('../artifacts/desktop-polish/',import.meta.url);
await fs.mkdir(root,{recursive:true});
const report={target,viewer:`http://127.0.0.1:${config.port}/`,startedAt:new Date().toISOString(),
  scope:'Fresh task-owned headless viewer; no lease/input/native-host UI; not independent acceptance',checks:[]};
let browser;
try{
  const before=await(await fetch(`http://127.0.0.1:${config.api}/control`)).json();
  assert.equal(before.owner,null);
  browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args:['--disable-background-networking','--no-default-browser-check']});
  const page=await browser.newPage({viewport:{width:1440,height:1040}});
  await page.goto(report.viewer);
  await page.waitForFunction(isVnc=>isVnc?(window.sidekicks?.connected&&
    window.sidekicks.rfb.getImageData().data.some(value=>value>0)):
    window.__sidekicksNeko?.client.transport.element.videoWidth===1280,target==='novnc',{timeout:30000});
  if(target==='nat'){
    await page.waitForFunction(()=>__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>5,{},{timeout:30000});
  }
  const first=await page.evaluate(isVnc=>{
    if(isVnc){const i=sidekicks.rfb.getImageData();return{kind:'RFB',width:i.width,height:i.height,nonzero:i.data.some(x=>x>0)};}
    const v=__sidekicksNeko.client.transport.element;return{kind:'WebRTC',width:v.videoWidth,height:v.videoHeight,frames:v.getVideoPlaybackQuality().totalVideoFrames};
  },target==='novnc');
  assert.equal(first.width,1280);assert.equal(first.height,720);
  if(target==='novnc')assert(first.nonzero);
  else{
    await page.waitForFunction(frames=>__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames>frames,first.frames,{timeout:10000});
    const stats=await page.evaluate(async()=>{
      const all=await __sidekicksNeko.stats(),by=Object.fromEntries(all.map(x=>[x.id,x]));
      const transport=all.find(x=>x.type==='transport'&&x.selectedCandidatePairId);
      const pair=by[transport?.selectedCandidatePairId];
      const remote=by[pair?.remoteCandidateId];
      return{pair:pair?{state:pair.state,localCandidateId:pair.localCandidateId,remoteCandidateId:pair.remoteCandidateId}:null,
        remote:remote?{address:remote.address||remote.ip,port:remote.port,protocol:remote.protocol}:null,
        decodedFrames:__sidekicksNeko.client.transport.element.getVideoPlaybackQuality().totalVideoFrames};
    });
    assert.equal(stats.pair?.state,'succeeded');assert.equal(stats.remote?.protocol,'udp');
    assert(stats.remote.port>=59220&&stats.remote.port<=59239);
    first.media=stats;
  }
  report.checks.push({name:'actual decoded stage stream',status:'PASS',detail:first});
  const png=Buffer.from(await page.evaluate(isVnc=>{
    const canvas=document.createElement('canvas');
    if(isVnc){const i=sidekicks.rfb.getImageData();canvas.width=i.width;canvas.height=i.height;canvas.getContext('2d').putImageData(i,0,0);}
    else{const v=__sidekicksNeko.client.transport.element;canvas.width=v.videoWidth;canvas.height=v.videoHeight;canvas.getContext('2d').drawImage(v,0,0);}
    return canvas.toDataURL().split(',')[1];
  },target==='novnc'),'base64');
  await fs.writeFile(new URL(target+'-stage-decoded.png',root),png);
  const capture=await fetch(`http://127.0.0.1:${config.api}/screenshot?maxWidth=1280&maxHeight=720`);
  assert.equal(capture.status,200);const bytes=Buffer.from(await capture.arrayBuffer());
  assert(bytes.length>1000&&bytes.length<=4194304);
  const ext=capture.headers.get('content-type')?.includes('jpeg')?'jpg':'png';
  await fs.writeFile(new URL(target+'-stage-capture.'+ext,root),bytes);
  report.checks.push({name:'fresh bounded native capture',status:'PASS',bytes:bytes.length,
    contentType:capture.headers.get('content-type'),sha256:createHash('sha256').update(bytes).digest('hex')});
  const after=await(await fetch(`http://127.0.0.1:${config.api}/control`)).json();
  assert.equal(after.owner,null);report.status='PASS';
}catch(error){report.status='FAIL';report.error=error.stack;process.exitCode=1;}
finally{
  await browser?.close();report.finishedAt=new Date().toISOString();
  await fs.writeFile(new URL(target+'-stage-view.json',root),JSON.stringify(report,null,2)+'\n');
}
console.log(JSON.stringify({status:report.status,target,checks:report.checks.length,error:report.error,
  path:fileURLToPath(new URL(target+'-stage-view.json',root))}));
