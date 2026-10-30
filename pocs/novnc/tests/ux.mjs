import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
const C='sidekicks-novnc-ux-stage',BASE='http://127.0.0.1:17901',API='http://127.0.0.1:17791',OUT='artifacts/novnc/ux';
const result={startedAt:new Date().toISOString(),container:C,checks:[],limits:['Isolated headless Chrome viewer and synthetic guest; no physical Mac keyboard/trackpad test.']};
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const check=(name,detail={})=>{result.checks.push({name,status:'PASS',...detail});console.log('PASS',name)};
async function until(fn,ms=7000){const start=Date.now();while(Date.now()-start<ms){const value=await fn();if(value)return value;await sleep(80)}throw new Error('observable state timed out')}
async function api(route,body,expected=200){const r=await fetch(API+route,body?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}:{}),text=await r.text();assert.equal(r.status,expected,text);return JSON.parse(text)}
const probeCode=`import importlib.util,json,re,subprocess
s=importlib.util.spec_from_file_location('apps','/app/desktop-app.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
o=m.output;clients=re.findall(r'0x[0-9a-fA-F]+',o('xprop','-root','_NET_CLIENT_LIST'));active=re.findall(r'0x[0-9a-fA-F]+',o('xprop','-root','_NET_ACTIVE_WINDOW'));records=[]
for wid in clients:
 p=o('xprop','-id',wid,'WM_CLASS','_NET_WM_WINDOW_TYPE','_NET_WM_NAME','WM_NAME','_NET_WM_STATE','WM_STATE','_NET_FRAME_EXTENTS');g=o('xwininfo','-id',wid)
 if '_NET_WM_WINDOW_TYPE(ATOM)' in p and '_NET_WM_WINDOW_TYPE_NORMAL' not in p:continue
 row={'id':wid,'properties':p,'active':bool(active and int(active[0],16)==int(wid,16)),'iconic':'Iconic' in p or '_NET_WM_STATE_HIDDEN' in p,'maximized':'_NET_WM_STATE_MAXIMIZED_HORZ' in p,'fullscreen':'_NET_WM_STATE_FULLSCREEN' in p}
 for label,key in [('Absolute upper-left X','x'),('Absolute upper-left Y','y'),('Width','width'),('Height','height')]:
  hit=re.search(r'^\\s*'+re.escape(label)+r':\\s*(-?\\d+)',g,re.M);row[key]=int(hit[1]) if hit else None
 e=re.search(r'_NET_FRAME_EXTENTS\\(CARDINAL\\) = (\\d+), (\\d+), (\\d+), (\\d+)',p);left,right,top,bottom=map(int,e.groups()) if e else (0,0,0,0)
 if row['x'] is not None:row['frame']={'x':row['x']-left,'y':row['y']-top,'width':row['width']+left+right,'height':row['height']+top+bottom,'top':top}
 for kind,cls in m.CLASSES.items():
  if cls in p.lower():row['kind']=kind
 records.append(row)
print(json.dumps(records))`;
const windows=()=>JSON.parse(execFileSync('docker',['exec',C,'python3','-c',probeCode],{encoding:'utf8'}));
const win=kind=>windows().find(w=>w.kind===kind);
const guestCode=`import http.client,json,re,sys
q=json.loads(sys.argv[1]);h=http.client.HTTPConnection('127.0.0.1',8080);h.request('GET','/home');r=h.getresponse();html=r.read().decode();token=re.search("const desktopToken='([^']+)'",html)[1]
c=http.client.HTTPConnection('127.0.0.1',8080,source_address=(q.get('source','127.0.0.1'),0));headers={'Content-Type':'application/json','Origin':q.get('origin','http://127.0.0.1:8080'),'X-Sidekicks-Desktop':token}
c.request(q.get('method','POST'),q.get('path','/desktop/launch'),json.dumps(q.get('body',{'app':'browser'})),headers);r=c.getresponse();text=r.read().decode()
try:data=json.loads(text)
except:data={'message':text[:120]}
if q.get('method')=='GET':data={'exposedToken':token in text}
print(json.dumps({'status':r.status,'data':data}))`;
const guest=q=>JSON.parse(execFileSync('docker',['exec',C,'python3','-c',guestCode,JSON.stringify(q)],{encoding:'utf8'}));
let browser,page;
try{
 assert.equal((await api('/desktop/state')).theme,'sidekicks-sky');await api('/control',{action:'stop',session:'ux-reset'});
 const initial=win('browser');assert(initial&&!initial.maximized);assert(initial.frame.x>30&&initial.frame.y>25);assert(initial.frame.width<1280);check('desktop ready with normal native browser and visible margins',{initial});
 for(const path of ['/home','/desktop/launch']){const r=await fetch(API+path,{method:path==='/home'?'GET':'POST',headers:{Host:'127.0.0.1:8080',Origin:'http://127.0.0.1:8080','X-Sidekicks-Desktop':'forged','Content-Type':'application/json'},body:path==='/home'?undefined:JSON.stringify({app:'terminal'})});assert.equal(r.status,403);assert(!(await r.text()).includes('const desktopToken='))}check('published port cannot forge guest identity or retrieve capability');
 assert.equal(guest({source:'127.0.0.2',path:'/home',method:'GET'}).status,403);assert.equal(guest({source:'127.0.0.2'}).status,403);assert.equal(guest({origin:'http://evil.example'}).status,403);assert.equal(guest({}).status,409);check('exact loopback, Origin and active ownership enforced on Home launcher');
 browser=await chromium.launch({headless:true,executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'});page=await browser.newPage({viewport:{width:1440,height:1080}});await page.goto(BASE);await page.waitForFunction(()=>window.sidekicks?.connected);await page.waitForFunction(()=>sidekicks.rfb.getImageData().data.some(v=>v>0));await page.screenshot({path:OUT+'/viewer-initial.png'});check('actual decoded UX viewer with native guest Home');
 await page.getByRole('button',{name:'Take control',exact:true}).click();await page.waitForFunction(()=>!!sidekicks.lease);
 const canvas=page.locator('#screen canvas');async function point(x,y){const b=await canvas.boundingBox();return{x:b.x+x*b.width/1280,y:b.y+y*b.height/720}}async function click(x,y,options){const p=await point(x,y);await page.mouse.click(p.x,p.y,options)}async function drag(x,y,dx,dy){const a=await point(x,y),b=await point(x+dx,y+dy);await page.mouse.move(a.x,a.y);await page.mouse.down();await page.mouse.move(b.x,b.y,{steps:14});await page.mouse.up();await sleep(250)}
 async function shot(name){const r=await fetch(API+'/screenshot');await fs.writeFile(OUT+'/'+name+'.png',Buffer.from(await r.arrayBuffer()))}
 const before=win('browser');await click(before.frame.x+before.frame.width-61,before.frame.y+16);await until(()=>win('browser')?.iconic);check('native minimize button iconifies actual browser');
 await click(563,672);const restored=await until(()=>{const w=win('browser');return w&&!w.iconic?w:false});assert.equal(restored.id,before.id);for(const k of ['x','y','width','height'])assert(Math.abs(restored.frame[k]-before.frame[k])<=2);assert.equal(windows().filter(w=>w.kind==='browser').length,1);check('dock restores same minimized browser at previous geometry, no duplicate client');
 await click(restored.frame.x+restored.frame.width-39,restored.frame.y+16);await until(()=>win('browser')?.maximized);await page.evaluate(()=>sidekicks.chord(['Alt','F10']));await until(()=>!win('browser')?.maximized);check('native maximize and keyboard restore toggle real window state');
 const current=win('browser');await click(current.x+current.width/2,current.y+292);await until(()=>win('terminal'));check('Home Terminal button launches native terminal through private capability');await shot('terminal');
 await click(563,672);await until(()=>win('browser')?.active);const b=win('browser');await click(b.x+b.width/2+205,b.y+292);await until(()=>win('files'));check('Home Files button opens actual PCManFM');await shot('files');
 await page.evaluate(()=>sidekicks.chord(['Alt','F4']));await until(()=>!win('files'));await sleep(400);assert(!win('files'));await click(664,672);await until(()=>win('files'));check('file window stays closed until explicit dock reopen');
 await click(563,672);await until(()=>win('browser')?.active);await click(614,672);await until(()=>win('terminal')?.active);await page.evaluate(()=>sidekicks.chord(['Alt','Tab']));await until(()=>win('browser')?.active);check('Alt+Tab cycles actual native window focus');
 await page.evaluate(()=>sidekicks.chord(['F11']));await until(()=>win('browser')?.fullscreen);await shot('browser-fullscreen');await page.evaluate(()=>sidekicks.chord(['F11']));await until(()=>!win('browser')?.fullscreen);check('guest browser F11 fullscreen reverses to normal window');
 await page.getByRole('button',{name:'Viewer fullscreen',exact:true}).click();await page.waitForFunction(()=>!!document.fullscreenElement);await page.getByRole('button',{name:'Exit viewer fullscreen',exact:true}).click();await page.waitForFunction(()=>!document.fullscreenElement);check('viewer DOM fullscreen enters and exits with controls retained');
 if(!(await page.evaluate(()=>!!sidekicks.lease)))await page.evaluate(()=>sidekicks.take());await page.setViewportSize({width:860,height:760});await sleep(250);
 const g=win('browser');await drag(g.frame.x+250,g.frame.y+16,-35,20);const moved=await until(()=>{const w=win('browser');return Math.abs(w.frame.x-g.frame.x)>15?w:false});check('titlebar drag moves real browser through scaled viewer',{before:g.frame,after:moved.frame});
 await drag(moved.frame.x+moved.frame.width-2,moved.frame.y+moved.frame.height-2,30,-40);const resized=await until(()=>{const w=win('browser');return w.width!==moved.width||w.height!==moved.height?w:false});check('native resize handle changes real window size at scaled viewer',{before:moved.frame,after:resized.frame});
 await page.locator('#windows summary').click();await page.getByRole('button',{name:'Maximize / restore',exact:false}).click();await until(()=>win('browser')?.maximized);await page.locator('#windows summary').click();await page.getByRole('button',{name:'Maximize / restore',exact:false}).click();await until(()=>!win('browser')?.maximized);check('scaled viewer window controls invoke native maximize/restore');await page.screenshot({path:OUT+'/viewer-scaled.png'});
 await page.keyboard.press('Control+t');await sleep(500);await until(()=>win('browser')?.properties.includes('Sidekicks'));await shot('new-tab');check('normal New Tab lands on Sidekicks Home, screenshot for tab-count inspection');await page.keyboard.press('Control+w');await sleep(200);
 const closing=win('browser');await click(closing.frame.x+closing.frame.width-17,closing.frame.y+16);await until(()=>!win('browser'));await sleep(400);assert(!win('browser'));assert.equal((await api('/desktop/state')).ready,true);check('real close stays closed and desktop readiness survives browser close');
 await click(563,672);const reopened=await until(()=>win('browser'));assert(!reopened.iconic&&!reopened.maximized);assert.equal(windows().filter(w=>w.kind==='browser').length,1);check('dock explicitly reopens browser as one fresh normal client');
 assert.equal(guest({body:{app:'not-allowed'}}).status,400);assert.equal(guest({}).data.state,'restored');assert.equal(windows().filter(w=>w.kind==='browser').length,1);check('allowlisted internal Home launch restores existing browser');
 await shot('final-desktop');result.final=windows();await page.evaluate(()=>sidekicks.release());result.status='PASS';result.finishedAt=new Date().toISOString();
}catch(e){result.status='FAIL';result.error=e.stack;console.error(e);process.exitCode=1;if(page)await page.screenshot({path:OUT+'/failure-viewer.png'}).catch(()=>{});try{result.failureWindows=windows()}catch{}}
finally{if(browser)await browser.close();await api('/control',{action:'stop',session:'ux-cleanup'}).catch(()=>{});await fs.writeFile(OUT+'/ux.json',JSON.stringify(result,null,2))}
