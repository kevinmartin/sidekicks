import {chromium} from 'playwright';
import {execFileSync} from 'node:child_process';
import fs from 'node:fs/promises';
const base='http://127.0.0.1:7901',out='artifacts/novnc',r={startedAt:new Date().toISOString(),checks:[]};let browser;
const sleep=ms=>new Promise(res=>setTimeout(res,ms));
async function api(route,body,expected=200){const response=await fetch(base+route,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{}),text=await response.text();if(response.status!==expected)throw new Error(`${route}: expected${expected}, got${response.status} ${text}`);try{return JSON.parse(text)}catch{return text}}
try{
 await api('/control',{action:'stop',owner:'agent',session:'native-reset'});
 browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});const page=await browser.newPage({viewport:{width:1440,height:1040}});await page.goto(base);await page.waitForFunction(()=>window.sidekicks?.connected);await page.waitForFunction(()=>window.sidekicks.rfb.getImageData().data.some(v=>v>0));
 const session='native-agent',lease=await api('/control',{action:'acquire',owner:'agent',session,leaseMs:60000});const send=events=>api('/input',{session,token:lease.token,events});
 await send([{type:'key',key:'Meta',action:'down'},{type:'key',key:'Enter',action:'down'},{type:'key',key:'Enter',action:'up'},{type:'key',key:'Meta',action:'up'}]);
 for(let i=0;i<30;i++){try{r.window=execFileSync('docker',['exec','sidekicks-novnc','xdotool','getactivewindow','getwindowname'],{encoding:'utf8'}).trim();if(r.window==='Sidekicks - disposable terminal')break}catch{}await sleep(100)}if(r.window!=='Sidekicks - disposable terminal')throw new Error('native terminal not active');r.checks.push('native xterm active, no fixture focus');
 await send([{type:'text',text:'# native-bot: '}]);const long=api('/input',{session,token:lease.token,events:[{type:'text',text:'q'.repeat(4096)},{type:'text',text:'MUST_NOT_RUN'}]},409);
 await sleep(35);const start=performance.now();await page.evaluate(()=>sidekicks.take());r.acquireMs=performance.now()-start;await long;r.control=await api('/control');if(r.control.owner!=='human'||r.control.state!=='ready')throw new Error('native human lease not ready');r.checks.push('generic native takeover succeeds without fixture ACK; queued request409');
 await page.keyboard.press('Control+u');r.marker='# native-human-'+Date.now().toString(36);await page.keyboard.type(r.marker,{delay:15});await page.keyboard.press('Enter');await sleep(700);
 await fs.writeFile(`${out}/native-takeover.png`,Buffer.from(await(await fetch(base+'/screenshot')).arrayBuffer()));r.checks.push('immediate viewer keyboard drives native terminal after ownership ready; screenshot evidence');
 await page.keyboard.press('Control+d');await page.evaluate(()=>sidekicks.release());r.status='PASS';r.finishedAt=new Date().toISOString();r.limit='No generic application queue flush or physical keyboard guarantee; native terminal visual evidence inspected separately.';console.log(JSON.stringify(r));
}catch(e){r.status='FAIL';r.error=e.stack;console.error(e);process.exitCode=1}finally{if(browser)await browser.close();await fs.writeFile(`${out}/native-takeover.json`,JSON.stringify(r,null,2))}
