import { chromium } from '../novnc/tests/node_modules/playwright/index.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const out = fileURLToPath(new URL('./artifacts/', import.meta.url));
const base = process.argv[2] || 'http://127.0.0.1:7903';
const label = process.argv[3] || 'vm-a';
const result = {base, label, checks: [], startedAt: new Date().toISOString(), kind: 'isolated headless Chrome, no user tabs or visible UI'};
let browser;
try {
  browser = await chromium.launch({headless:true, executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', args:['--disable-background-networking','--no-default-browser-check']});
  const page = await browser.newPage({viewport:{width:1440,height:1000},deviceScaleFactor:1});
  await page.goto(base);
  await page.waitForFunction(()=>window.sidekicks?.connected && window.sidekicks.rfb.getImageData().data.some(v=>v>0));
  const dimensions = await page.evaluate(()=>{const im=sidekicks.rfb.getImageData(); return {width:im.width,height:im.height,pixelSum:im.data.reduce((a,v)=>a+v,0)}});
  assert.equal(dimensions.width,1280); assert.equal(dimensions.height,720); assert(dimensions.pixelSum>10000000);
  result.checks.push({name:'actual decoded RFB framebuffer',...dimensions});
  const snapshot = async() => (await (await fetch(base+'/fixture-events?latest=1')).json()).filter(x=>x.type==='snapshot').at(-1);
  const initial = await snapshot(); assert(initial?.marker?.startsWith('SK-'));
  await page.evaluate(()=>sidekicks.take());
  const r=initial.geometry.rects.typing, o=initial.geometry.offset;
  const canvas=await page.locator('#screen canvas').boundingBox();
  await page.mouse.click(canvas.x+(o.x+r.x+r.width/2)*canvas.width/1280,canvas.y+(o.y+r.y+r.height/2)*canvas.height/720);
  await page.keyboard.press('Control+a');
  const marker='microvm-'+Date.now(); await page.keyboard.type(marker,{delay:10});
  await page.waitForFunction(async expected=>{const events=await (await fetch('/fixture-events?latest=1')).json();return events.filter(x=>x.type==='snapshot').at(-1)?.snapshot.text===expected},marker);
  result.checks.push({name:'viewer keyboard through vsock bridge into guest Chromium',text:marker});
  await page.screenshot({path:out+label+'-viewer.png',fullPage:true});
  const png=await fetch(base+'/screenshot?maxWidth=1280&maxHeight=720'); assert.equal(png.status,200);
  await fs.writeFile(out+label+'-capture.png',Buffer.from(await png.arrayBuffer()));
  result.checks.push({name:'fresh direct guest screenshot',marker:initial.marker});
  await page.evaluate(()=>sidekicks.release());
  result.finalFixture=await snapshot(); result.browserVersion=await browser.version(); result.status='PASS';
} catch(error) {result.status='FAIL';result.error=error.stack;process.exitCode=1;}
finally {if(browser) await browser.close();result.finishedAt=new Date().toISOString();await fs.writeFile(out+label+'-viewer.json',JSON.stringify(result,null,2));}
console.log(JSON.stringify({status:result.status,checks:result.checks,error:result.error},null,2));
