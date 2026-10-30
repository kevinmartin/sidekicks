import http from 'node:http';
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {randomUUID} from 'node:crypto';
import WebSocket,{WebSocketServer} from 'ws';
import sharp from 'sharp';
import {LeaseGate,HttpError} from './lease.mjs';
import {executeEvents} from './input.mjs';
import {requireDesktopPage,validateDesktopLaunch,launchDesktopApp} from './desktop-apps.mjs';
const root=path.dirname(fileURLToPath(import.meta.url)), upstream='http://127.0.0.1:8080';
const viewers=new Map(), readerTokens=new Map(); let fixtureState=null; let inputQueue=Promise.resolve();
const desktopPageToken=randomUUID();
let homeRequests=0;
const localOrigin=o=>!o||/^http:\/\/(127\.0\.0\.1|localhost|\[::1\])(:\d+)?$/.test(o);
const json=(r,status,data)=>{r.writeHead(status,{'content-type':'application/json','cache-control':'no-store'});r.end(JSON.stringify(data));};
async function body(req){let b='';for await(const c of req){b+=c;if(b.length>65536)throw new HttpError(413,'request too large');}try{return JSON.parse(b||'{}');}catch{throw new HttpError(400,'invalid JSON');}}

class Driver {
  ready=false;token='';ws=null;keys=new Set();buttons=new Set();autoShift=new Set();sentFrames=0;size={width:1280,height:720,rate:30};lastError='starting';
  async api(method,p,data){const r=await fetch(upstream+'/api'+p,{method,headers:{Authorization:'Bearer '+this.token,'content-type':'application/json'},body:data===undefined?undefined:JSON.stringify(data),signal:AbortSignal.timeout(4000)});if(!r.ok)throw new Error(`Neko ${p}: ${r.status}`);return r;}
  send(event,payload){if(!this.ready||this.ws?.readyState!==WebSocket.OPEN)throw new HttpError(503,'control disconnected');this.ws.send(JSON.stringify({event,payload}));if(event.startsWith('control/'))this.sentFrames++;}
  move(x,y){this.send('control/move',{x,y});}
  key(keysym,down){
    // Xorg's dynamic alphabetic mapping needs a Shift modifier for an uppercase
    // keysym. Normalized text can specify uppercase without a physical Shift.
    const shift=0xffe1;
    if(down&&keysym>=65&&keysym<=90&&!this.keys.has(shift)){
      this.send('control/keydown',{keysym:shift});this.keys.add(shift);this.autoShift.add(keysym);
    }
    this.send(down?'control/keydown':'control/keyup',{keysym});down?this.keys.add(keysym):this.keys.delete(keysym);
    if(!down&&this.autoShift.delete(keysym)&&this.autoShift.size===0){this.send('control/keyup',{keysym:shift});this.keys.delete(shift);}
  }
  button(code,down){this.send(down?'control/buttondown':'control/buttonup',{code});down?this.buttons.add(code):this.buttons.delete(code);}
  releaseHeld(){for(const k of [...this.keys])if(this.keys.has(k))this.key(k,false);for(const b of [...this.buttons])this.button(b,false);}
  async reset(){this.keys.clear();this.buttons.clear();this.autoShift.clear();if(this.ready){await this.api('POST','/room/control/reset');await this.api('POST','/room/control/take');}}
  async give(id){await this.api('POST','/room/control/give/'+encodeURIComponent(id));}
  async connect(){
    try{
      const r=await fetch(upstream+'/api/login',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({username:'Sidekicks-controller',password:'sidekicks-disposable-admin'}),signal:AbortSignal.timeout(3000)});
      if(!r.ok)throw new Error('login '+r.status);const login=await r.json();this.token=login.token;
      const ws=this.ws=new WebSocket('ws://127.0.0.1:8080/api/ws?token='+encodeURIComponent(this.token));
      ws.on('message',async data=>{const m=JSON.parse(data);if(m.event==='system/init'){this.ready=true;this.lastError='';this.size=m.payload.screen_size??this.size;await this.reset().catch(e=>this.lastError=e.message);console.log('Neko controller ready');}if(m.event==='system/heartbeat')this.send('client/heartbeat');if(m.event==='screen/updated')this.size={width:m.payload.width,height:m.payload.height,rate:m.payload.rate};});
      ws.on('error',e=>{this.lastError=e.message;});
      ws.on('close',()=>{this.ready=false;gate.current=null;gate.generation++;readerTokens.clear();viewers.clear();setTimeout(()=>this.connect(),1000);});
    }catch(e){this.ready=false;this.lastError=e.message;setTimeout(()=>this.connect(),1000);}
  }
}
const driver=new Driver(),gate=new LeaseGate(driver);
setInterval(()=>gate.expire().catch(e=>console.error('expiry',e.message)),100).unref();
const capability=(status,detail)=>({status,detail});
function capabilities(){return {id:'neko',viewerUrl:'http://127.0.0.1:7902/',nativeViewerUrl:'http://127.0.0.1:7902/?mode=native',capabilities:{keyboard:capability('implemented','X11 keysyms via v3 websocket; native diagnostic uses SDK data channel'),click:capability('implemented','Gated HTTP and native data channel'),rightClick:capability('implemented','X11 button 3'),doubleClick:capability('implemented','Two button 1 pairs'),drag:capability('implemented','Pointer down/move/up'),scroll:capability('implemented','Wheel delta translation; desktop pixels'),modifiers:capability('implemented','X11 Shift/Control/Alt/Super'),pinchNative:capability('unsupported','Native Xorg touch driver omitted; physical Mac gestures untested'),swipeNative:capability('unsupported','No native gesture route in this configuration; drag/wheel emulation only'),zoomEmulated:capability('implemented','Ctrl+wheel or keyboard shortcut; not native pinch'),screenshot:capability('implemented','Neko X11 screenshot → bounded sharp resize JPEG'),audio:capability('untested','Virtual PulseAudio monitor + Opus; no microphone'),theme:capability('implemented','Xfce X11 desktop, GTK3/VTE terminal, Thunar and Sidekicks Home launchers'),humanTakeover:capability('implemented','Lease gate cancels queued writes; Neko locked controls gates native host transfer')}};}
async function readerLogin(session){
  const r=await fetch(upstream+'/api/login',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({username:'viewer-'+session,password:'sidekicks-disposable-view'}),signal:AbortSignal.timeout(4000)});
  if(!r.ok)throw new HttpError(503,'viewer login unavailable');const data=await r.json();readerTokens.set(data.token,{session,id:data.id});viewers.set(session,data.id);return data;
}
async function handler(req,res){
 try{
  const u=new URL(req.url,'http://127.0.0.1');
  if(!/^(127\.0\.0\.1|localhost|\[::1\])(?::(?:7790|7792|7902|8080))?$/.test(req.headers.host??''))throw new HttpError(403,'loopback Host required');
  if(!localOrigin(req.headers.origin))throw new HttpError(403,'non-loopback Origin rejected');
  if(req.method==='POST'&&!String(req.headers['content-type']).startsWith('application/json'))throw new HttpError(415,'application/json required');
  if(u.pathname==='/desktop/state'&&req.method==='GET'){
    let state={ready:false,theme:'sidekicks-sky',homeUrl:'http://127.0.0.1:7792/home'};
    try{state={...state,...JSON.parse(await fs.readFile('/tmp/sidekicks-desktop-ready.json','utf8'))};}catch{}
    return json(res,200,{...state,homeRequests});
  }
  if(u.pathname==='/desktop/launch'&&req.method==='POST'){
    const b=await body(req);await gate.expire();const script=validateDesktopLaunch(req,b,desktopPageToken,gate);await launchDesktopApp(script);return json(res,200,{ok:true,app:b.app});
  }
  if(u.pathname==='/home'&&req.method==='GET'){
    requireDesktopPage(req);homeRequests++;const html=(await fs.readFile(root+'/public/home.html','utf8')).replaceAll('__SIDEKICKS_DESKTOP_TOKEN__',desktopPageToken);
    res.writeHead(200,{'content-type':'text/html','cache-control':'no-store','content-security-policy':"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'"});return res.end(html);
  }
  if(u.pathname==='/fixture-state'&&req.method==='POST'){if(!['127.0.0.1','::ffff:127.0.0.1','::1'].includes(req.socket.remoteAddress))throw new HttpError(403,'internal synthetic fixture only');fixtureState=await body(req);return json(res,200,{ok:true});}
  if(u.pathname==='/fixture-state'&&req.method==='GET')return json(res,fixtureState?200:503,fixtureState??{error:'fixture not ready'});
  if(u.pathname==='/health')return json(res,driver.ready?200:503,{ok:driver.ready,id:'neko',status:driver.ready?'ready':'starting',runtime:{platform:'linux/arm64',emulated:false,neko:'3.2.0',sourceBuild:true},error:driver.lastError});
  if(u.pathname==='/capabilities')return json(res,200,capabilities());
  if(u.pathname==='/metrics')return json(res,200,{controllerInputFramesSent:driver.sentFrames,heldKeys:driver.keys.size,heldButtons:driver.buttons.size,generation:gate.generation,owner:gate.view().owner,meaning:'Adapter websocket injection boundary; already delivered application input may render later.'});
  if(u.pathname==='/control'&&req.method==='GET'){await gate.expire();return json(res,200,gate.view());}
  if(u.pathname==='/control'&&req.method==='POST'){
    const b=await body(req);await gate.expire();
    if(b.action==='acquire'){b.viewerId=viewers.get(b.session);return json(res,200,await gate.acquire(b));}
    if(b.action==='release')return json(res,200,await gate.release(b));
    if(b.action==='stop')return json(res,200,await gate.stop());
    throw new HttpError(400,'invalid control action');
  }
  if(u.pathname==='/input'&&req.method==='POST'){
    const b=await body(req);gate.validate(b);const job=inputQueue.catch(()=>{}).then(()=>executeEvents(driver,gate,b));inputQueue=job;try{return json(res,200,await job);}catch(e){if(gate.current?.token===b.token&&gate.current?.mode!=='native')await gate.stop().catch(()=>{});throw e;}
  }
  if(u.pathname==='/screenshot'&&req.method==='GET'){
    if(!driver.ready)throw new HttpError(503,'desktop not ready');
    const w=Number(u.searchParams.get('maxWidth')??1280),h=Number(u.searchParams.get('maxHeight')??720);
    if(!Number.isInteger(w)||!Number.isInteger(h)||w<1||h<1||w>1920||h>1080)throw new HttpError(400,'maximum bounds 1920x1080');
    const start=performance.now(),r=await driver.api('GET','/room/screen/shot.jpg?quality=90'),raw=Buffer.from(await r.arrayBuffer());
    const meta=await sharp(raw).metadata(),out=await sharp(raw).resize({width:w,height:h,fit:'inside',withoutEnlargement:true}).jpeg({quality:85}).toBuffer({resolveWithObject:true});
    if(out.data.length>4*1024*1024)throw new HttpError(413,'image exceeds 4 MiB');
    res.writeHead(200,{'content-type':'image/jpeg','cache-control':'no-store','x-source-width':meta.width,'x-source-height':meta.height,'x-image-width':out.info.width,'x-image-height':out.info.height,'x-capture-ms':(performance.now()-start).toFixed(2),'x-capture-source':'Neko X11 screenshot API'});return res.end(out.data);
  }
  if(u.pathname==='/viewer-session'&&req.method==='POST'){const b=await body(req);if(!/^[\w.-]{1,128}$/.test(b.session??''))throw new HttpError(400,'invalid viewer session');return json(res,200,await readerLogin(b.session));}
  if(u.pathname.startsWith('/stream/api/')){
    const p=u.pathname.slice('/stream'.length),token=String(req.headers.authorization??'').replace(/^Bearer /,'');
    if(!readerTokens.has(token))throw new HttpError(401,'viewer token required');
    if(req.method!=='GET'||!['/api/whoami','/api/room/screen','/api/room/screen/configurations'].includes(p))throw new HttpError(403,'viewer REST route denied');
    const r=await fetch(upstream+p,{headers:{Authorization:'Bearer '+token},signal:AbortSignal.timeout(4000)});res.writeHead(r.status,{'content-type':'application/json'});return res.end(Buffer.from(await r.arrayBuffer()));
  }
  if(u.pathname==='/screen'&&req.method==='POST'){const b=await body(req);const c=gate.validate(b);if(c.owner!=='agent'||c.mode!=='gated')throw new HttpError(409,'agent gated lease required');if(![[1280,720],[1024,576]].some(([w,h])=>b.width===w&&b.height===h)||b.rate!==30)throw new HttpError(422,'supported sizes: 1280x720@30, 1024x576@30');await driver.api('POST','/room/screen/',{width:b.width,height:b.height,rate:b.rate});driver.size={width:b.width,height:b.height,rate:b.rate};return json(res,200,{ok:true,...driver.size});}
  if(req.method==='GET'){
    const file=u.pathname==='/fixture'?'/fixture.html':u.pathname==='/'?'/index.html':u.pathname;
    if(!['/index.html','/viewer.js','/fixture.html'].includes(file))throw new HttpError(404,'not found');
    const data=await fs.readFile(root+'/public'+file);res.writeHead(200,{'content-type':file.endsWith('.js')?'text/javascript':'text/html','cache-control':'no-store','content-security-policy':"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws://127.0.0.1:* ws://localhost:*; media-src 'self' blob:; img-src 'self' data: blob:; object-src 'none'; frame-ancestors 'self' http://127.0.0.1:7790 http://localhost:7790"});return res.end(data);
  }
  throw new HttpError(404,'not found');
 }catch(e){json(res,e.status??502,{ok:false,error:e.message});}
}
const wss=new WebSocketServer({noServer:true,maxPayload:1048576});
for(const port of [7792,7902]){
  const server=http.createServer(handler);
  server.on('upgrade',(req,socket,head)=>{
    const u=new URL(req.url,'http://127.0.0.1'),token=u.searchParams.get('token'),reader=readerTokens.get(token);
    if(u.pathname!=='/stream/api/ws'||!reader||!localOrigin(req.headers.origin)||!/^(127\.0\.0\.1|localhost|\[::1\]):(7792|7902)$/.test(req.headers.host??'')){socket.write('HTTP/1.1 403 Forbidden\r\n\r\n');socket.destroy();return;}
    wss.handleUpgrade(req,socket,head,client=>{
      const remote=new WebSocket('ws://127.0.0.1:8080/api/ws?token='+encodeURIComponent(token));const queue=[];
      client.on('message',data=>{try{const m=JSON.parse(data);if(!m.event?.startsWith('signal/')&&m.event!=='system/heartbeat')return;if(remote.readyState===WebSocket.OPEN)remote.send(data.toString());else if(queue.length<50)queue.push(data.toString());}catch{client.close(1003);}});
      remote.on('open',()=>queue.splice(0).forEach(m=>remote.send(m)));
      remote.on('message',data=>client.readyState===WebSocket.OPEN&&client.send(data.toString()));
      remote.on('error',()=>client.close());remote.on('close',()=>client.close());
      client.on('close',()=>{remote.close(1000);if(gate.current?.session===reader.session)gate.stop().catch(()=>{});});
    });
  });
  server.listen(port,'0.0.0.0',()=>console.log('Sidekicks Neko listening internally on '+port));
}
driver.connect();
process.on('SIGTERM',async()=>{await gate.stop().catch(()=>{});driver.ws?.close(1000);process.exit(0);});
