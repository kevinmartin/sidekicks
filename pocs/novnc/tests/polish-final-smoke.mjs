// Bounded final-image regressions, task-owned stage ONLY. No production calls.
import {chromium} from 'playwright';import fs from 'node:fs/promises';import assert from 'node:assert/strict';import {execFileSync} from 'node:child_process';
const C='sidekicks-novnc-xfce-stage',API='http://127.0.0.1:17791',BASE='http://127.0.0.1:17801';
const out='artifacts/novnc/polish/final-smoke-'+new Date().toISOString().replace(/[:.]/g,'-');await fs.mkdir(out,{recursive:true});
const observer=await fs.readFile('artifacts/independent-qa/ux/x11-observe.py','utf8'),guest=code=>execFileSync('docker',['exec','-i',C,'python3','-'],{input:code,encoding:'utf8'}),state=()=>JSON.parse(guest(observer));
const sleep=ms=>new Promise(r=>setTimeout(r,ms));async function until(fn,ms=8000){const t=Date.now();while(Date.now()-t<ms){const x=await fn();if(x)return x;await sleep(100)}throw Error('bounded final observation timed out')}
const report={stageOnly:true,checks:[],startedAt:new Date().toISOString(),limitations:['Task-owned headless Chrome input, no physical/mobile claim.','Earlier full stage app/menu evidence is separate; this is a bounded final-image regression.']};
const browserWin=()=>state().windows.find(w=>w.wmClass.includes('SidekicksBrowser')&&w.types.includes('_NET_WM_WINDOW_TYPE_NORMAL'));
let browser,page;
try{
 report.runtime=JSON.parse(execFileSync('docker',['inspect',C],{encoding:'utf8'})).map(c=>({id:c.Id,image:c.Image}));
 assert.equal((await (await fetch(API+'/control')).json()).owner,null);
 browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});page=await browser.newPage({viewport:{width:1440,height:1040}});await page.goto(BASE);await page.waitForFunction(()=>window.sidekicks?.connected&&sidekicks.rfb.getImageData().data.some(v=>v));await page.evaluate(()=>sidekicks.take());
 const point=async p=>{const b=await page.locator('#screen canvas').boundingBox();return{x:b.x+p.x*b.width/1280,y:b.y+p.y*b.height/720}};
 const click=async p=>{const q=await point(p);await page.mouse.click(q.x,q.y)};
 const chord=async keys=>{await page.evaluate(keys=>sidekicks.chord(keys),keys);await sleep(180)};
 const shot=async name=>{const r=await fetch(API+'/screenshot');await fs.writeFile(out+'/'+name+'-guest.png',Buffer.from(await r.arrayBuffer()));await page.screenshot({path:out+'/'+name+'-viewer.png'})};
 await chord(['Meta','b']);let w=browserWin();await click({x:w.client.x+250,y:w.client.y+180});await page.evaluate(()=>sidekicks.rfb.focus());await sleep(200);
 await page.keyboard.press('F11');await until(()=>browserWin().states.includes('_NET_WM_STATE_FULLSCREEN'));assert.equal(await page.evaluate(()=>!!document.fullscreenElement),false);await shot('guest-fullscreen');await page.keyboard.press('F11');await until(()=>!browserWin().states.includes('_NET_WM_STATE_FULLSCREEN'));
 await chord(['Alt','F12']);await until(()=>browserWin().states.includes('_NET_WM_STATE_FULLSCREEN'));await chord(['Alt','F12']);await until(()=>!browserWin().states.includes('_NET_WM_STATE_FULLSCREEN'));report.checks.push({name:'browser F11 and WM AltF12 independently reversible',status:'PASS'});
 await page.locator('#fullscreen').click();await page.waitForFunction(()=>!!document.fullscreenElement);let q=await point({x:20,y:590});await page.mouse.move(q.x,q.y);await page.mouse.down();await page.keyboard.down('Shift');await until(()=>{const s=state();return s.allPressedKeycodes.length&&s.pointer.pressedButtons.includes(1)});report.heldBeforeEscape=state();await page.keyboard.press('Escape');await page.waitForFunction(()=>!document.fullscreenElement);await until(()=>{const s=state();return !s.allPressedKeycodes.length&&!s.pointer.pressedButtons.length});await page.keyboard.up('Shift');await page.mouse.up();report.checks.push({name:'viewer Escape exits fullscreen and actual XQuery held input empty',status:'PASS'});
 await chord(['Meta','b']);w=browserWin();await click({x:w.client.x+250,y:w.client.y+180});await page.evaluate(()=>sidekicks.rfb.focus());await page.keyboard.press('Control+w');await until(()=>!browserWin());const readyResponse=await fetch(API+'/desktop/state');report.browserClosedReady={status:readyResponse.status,data:await readyResponse.json()};assert.equal(readyResponse.status,200);assert.equal(report.browserClosedReady.data.ready,true);report.checks.push({name:'closing only browser leaves actual Xfce desktop ready',status:'PASS'});
 await chord(['Meta','b']);await until(()=>browserWin());await sleep(600);
 // Close the two synthetic compatibility-test apps through native close, leaving Home.
 for(const k of ['e','Enter']){await chord(['Meta',k]);await chord(['Alt','F4']);}
 await chord(['Meta','b']);await page.evaluate(()=>sidekicks.release());report.final=state();assert.deepEqual(report.final.allPressedKeycodes,[]);assert.deepEqual(report.final.pointer.pressedButtons,[]);report.controlAfter=await (await fetch(API+'/control')).json();assert.equal(report.controlAfter.owner,null);await shot('final-home');report.status='PASS';
}catch(e){report.status='FAIL';report.error=e.stack;process.exitCode=1;try{report.failure=state();await page?.screenshot({path:out+'/failure-viewer.png'})}catch{}}
finally{try{await page?.mouse.up();await page?.evaluate(()=>sidekicks?.lease?sidekicks.release():null)}catch{}await browser?.close();report.finishedAt=new Date().toISOString();await fs.writeFile(out+'/result.json',JSON.stringify(report,null,2));console.log(JSON.stringify({status:report.status,evidence:out,error:report.error}));}
