import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import http from 'node:http';
import { chromium } from 'playwright';

const results=[],out='artifacts/independent-qa';await fs.mkdir(out,{recursive:true});
async function raw(route,body,headers={}){
 return new Promise((resolve,reject)=>{const req=http.request('http://127.0.0.1:7790'+route,{method:body===undefined?'GET':'POST',headers:{...(body===undefined?{}:{'Content-Type':'application/json'}),...headers}},res=>{const chunks=[];res.on('data',c=>chunks.push(c));res.on('end',()=>{const text=Buffer.concat(chunks).toString();let data;try{data=JSON.parse(text);}catch{data=text.slice(0,500);}resolve({status:res.statusCode,headers:res.headers,data});});});req.on('error',reject);if(body!==undefined)req.write(JSON.stringify(body));req.end();});
}
async function check(name,fn){try{results.push({name,status:'PASS',detail:await fn()});}catch(e){results.push({name,status:'FAIL',error:e.message});}console.log(`${results.at(-1).status} ${name}`);}
let browser;
try{
 const auth=await raw('/api/session');assert.equal(auth.status,200);const token=auth.data.token;
 await check('workbench Host and external Origin rejected',async()=>{const host=await raw('/api/adapters',undefined,{Host:'untrusted.example:7790'});const origin=await raw('/api/adapters',undefined,{Origin:'https://untrusted.example'});assert.equal(host.status,403);assert.equal(origin.status,403);return{host:host.status,origin:origin.status};});
 await check('workbench mutation requires bearer and same Origin',async()=>{const body={bot:'Scout',intent:'independent-synthetic-probe'};const missing=await raw('/api/thread',body);const external=await raw('/api/thread',body,{Authorization:'Bearer '+token,Origin:'https://untrusted.example'});assert.equal(missing.status,403);assert.equal(external.status,403);return{missingBearer:missing.status,externalOrigin:external.status};});
 await check('workbench has no host command execution route',async()=>{const r=await raw('/api/terminal',{command:'echo synthetic'},{Authorization:'Bearer '+token});assert.equal(r.status,404);return{status:r.status};});
 browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true,args:['--no-first-run','--no-default-browser-check']});
 const context=await browser.newContext({viewport:{width:1440,height:1100}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await check('task-owned browser renders workbench live status',async()=>{await page.goto('http://127.0.0.1:7790/',{waitUntil:'networkidle'});await page.waitForSelector('#adapters .card');const cards=await page.locator('#adapters .card').allTextContents();assert.equal(cards.length,2);assert.equal(errors.length,0);const personas=await page.locator('#bot option').allTextContents();assert.deepEqual(personas,['Scout','Builder','Reviewer']);assert.match(await page.locator('.collaboration').innerText(),/demo intent records, with no autonomous agents or model execution/);await page.screenshot({path:out+'/workbench.png',fullPage:true});return{cards,personas,errors};});
 await check('workbench intent post is explicitly non-executing',async()=>{const marker='independent-QA-'+Date.now();await page.locator('#bot').selectOption('Reviewer');await page.locator('#intent').fill(marker);await page.locator('#intent-form button').click();await page.getByText(marker,{exact:true}).waitFor();const items=await raw('/api/thread');const item=items.data.find(i=>i.intent===marker);assert.equal(item.execution,'intent-only');return item;});
 await check('workbench unknown adapter and dimensions fail closed',async()=>{const unknown=await raw('/api/adapters/no-such-adapter/control',{action:'acquire',owner:'agent',session:'qa'},{Authorization:'Bearer '+token});const bounds=await raw('/api/adapters/novnc/screenshot?maxWidth=999999');assert.equal(unknown.status,404);assert.equal(bounds.status,400);return{unknown:unknown.status,bounds:bounds.status};});
 await check('fixture browser behavior provides observable event evidence',async()=>{const fixture=await context.newPage();await fixture.goto('http://127.0.0.1:7790/fixture/');const marker='fixture-independent-'+Date.now();await fixture.locator('#typing').fill(marker);await fixture.locator('#click-target').dblclick();await fixture.locator('#click-target').click({button:'right'});const snapshot=await fixture.evaluate(()=>window.__sidekicksFixture.snapshot());assert.equal(snapshot.text,marker);assert.equal(snapshot.counts.double,1);assert.equal(snapshot.counts.right,1);await fixture.screenshot({path:out+'/shared-fixture-browser.png',fullPage:true});await fs.writeFile(out+'/shared-fixture-browser-events.json',JSON.stringify(snapshot,null,2));return{counts:snapshot.counts,marker,limitation:'This is direct browser-use of the shared fixture; it does not prove remote desktop input.'};});
}finally{
 await browser?.close();await fs.writeFile(out+'/workbench.json',JSON.stringify({generatedAt:new Date().toISOString(),independent:true,results},null,2));
}
if(results.some(r=>r.status==='FAIL'))process.exitCode=1;
