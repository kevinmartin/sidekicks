import {NekoClient,Overlay} from '@m1k1o/neko';
import {cursorStyle,observerPosition} from './cursor.mjs';
const $=id=>document.getElementById(id),q=new URLSearchParams(location.search),native=q.get('mode')==='native';
const session=q.get('session')||crypto.randomUUID();let lease=q.get('token')?{token:q.get('token'),session}:null,client,overlay,sequence=Promise.resolve(),dropped=0,lastMove=0;
let detachOverlay=null;
let cursor=null,cursorPos=null,cursorCleanup=[];
function syncCursor(){
 if(native){$('media').dataset.observer=String(!client?.controlling);return;}
 const active=!!lease,visible=!!(cursor&&cursorPos&&cursor.width>1&&cursor.height>1);
 $('gated').style.cursor=active&&cursor?cursorStyle(cursor):visible?'none':'default';
 const img=$('remote-cursor');img.hidden=active||!visible;
 if(visible){img.src=cursor.uri;img.width=cursor.width;img.height=cursor.height;Object.assign(img.style,observerPosition(cursorPos,client.state.screen.size,cursor));}
}
// Ephemeral lease tokens from the workbench must not remain in the URL/history.
history.replaceState(null,'',location.pathname+(native?'?mode=native':''));
const api=async(p,b)=>{const r=await fetch(p,{method:b?'POST':'GET',headers:b?{'content-type':'application/json'}:undefined,body:b?JSON.stringify(b):undefined});const j=await r.json();if(!r.ok)throw new Error(j.error||r.status);return j;};
function error(e){$('status').textContent=e.message;}
function release(beacon=false){const b={action:'release',session,token:lease?.token};lease=null;syncCursor();sequence=Promise.resolve();if(beacon){navigator.sendBeacon('/control',new Blob([JSON.stringify(b)],{type:'application/json'}));return;}return api('/control',b).catch(error);}
function queue(events){if(!lease||native)return;const auth={...lease,session};sequence=sequence.catch(()=>{}).then(()=>{if(!lease||lease.token!==auth.token){dropped++;return;}return api('/input',{...auth,events});}).catch(e=>{lease=null;syncCursor();error(e);});}
const pos=e=>{const r=$('gated').getBoundingClientRect(),s=client.state.screen.size;return{x:Math.max(0,Math.min(s.width-1,Math.floor((e.clientX-r.left)*s.width/r.width))),y:Math.max(0,Math.min(s.height-1,Math.floor((e.clientY-r.top)*s.height/r.height)))};};
async function connect(){
 detachOverlay?.();detachOverlay=null;overlay=null;cursorCleanup.splice(0).forEach(fn=>fn());if(cursor)URL.revokeObjectURL(cursor.uri);cursor=null;cursorPos=null;$('remote-cursor').hidden=true;client?.disconnect();const login=await api('/viewer-session',{session});
 client=new NekoClient({autoplay:true});client.setUrl(location.origin+'/stream');await client.authenticate(login.token);client.transport.attach($('media'));client.mute();client.connect();
 $('gated').style.display=native?'none':'block';
 if(native){overlay=new Overlay(client);detachOverlay=overlay.attach($('media'));}
 if(!native){cursorCleanup.push(client.transport.on('cursor.image',img=>{const old=cursor;cursor=img;syncCursor();if(old)URL.revokeObjectURL(old.uri);}),client.transport.on('cursor.position',p=>{cursorPos=p;syncCursor();}));}
 cursorCleanup.push(client.store.subscribe(s=>s.control.host_id,syncCursor));syncCursor();
 client.store.subscribe(s=>s.screen.size,s=>{$('stage').style.aspectRatio=s.width+'/'+s.height;fitScreen();});
 client.events.on('connection.status',status=>$('status').textContent='Stream '+status);
 client.events.on('connection.closed',e=>{lease=null;cursorPos=null;syncCursor();if(e)error(e);});
 window.__sidekicksNeko={get client(){return client;},get session(){return session;},get lease(){return lease;},native,queue,api,get dropped(){return dropped;},async take(){return take();},release,reconnect:connect,
   get cursor(){return cursor?{width:cursor.width,height:cursor.height,x:cursor.x,y:cursor.y,mode:lease?'local-css':'remote-observer'}:null;},
   async stats(){const pc=client.transport.pc;if(!pc)return[];return[...await pc.getStats()].map(x=>x[1]);},
   framePixel(x,y){const v=client.transport.element,c=document.createElement('canvas');c.width=v.videoWidth;c.height=v.videoHeight;const ctx=c.getContext('2d');ctx.drawImage(v,0,0);return[...ctx.getImageData(x,y,1,1).data];}
 };
}
async function take(){$('status').textContent='Taking control · releasing prior held input…';lease=await api('/control',{action:'acquire',owner:'human',session,token:lease?.token,leaseMs:30000,mode:native?'native':'gated'});syncCursor();if(!native)$('gated').focus();$('status').textContent='Human control ready · '+(native?'native data channel':'gated HTTP');return{...lease,session};}
function launcher(key){if(!lease){error(new Error('Take human control first'));return;}const terminal=key==='Enter',modifier=terminal?0xffeb:0xffe9,target=terminal?0xff0d:0xffc6;if(native){if(!client.controlling){error(new Error('Native control is not ready'));return;}client.input.key(modifier,true);client.input.key(target,true);client.input.key(target,false);client.input.key(modifier,false);overlay.wrap.querySelector('textarea').focus();}else {const mod=terminal?'Meta':'Alt',target=terminal?'Enter':'F9';queue([{type:'key',key:mod,action:'down'},{type:'key',key:target,action:'down'},{type:'key',key:target,action:'up'},{type:'key',key:mod,action:'up'}]);$('gated').focus();}}
$('terminal').onclick=()=>launcher('Enter');$('minimize').onclick=()=>launcher('m');
$('take').onclick=()=>take().catch(error);$('release').onclick=()=>release();$('stop').onclick=async()=>{lease=null;syncCursor();await api('/control',{action:'stop'}).catch(error);};$('reconnect').onclick=()=>connect().catch(error);$('sound').onclick=()=>{client.unmute();client.play();};
$('mode').textContent=native?'Native Neko / data-channel diagnostic':'Workbench / lease-gated HTTP input';$('switch').textContent=native?'Lease-gated workbench input':'Native input diagnostic';$('switch').href=native?'/':'/?mode=native';$('help').textContent=native?'Human-only diagnostic. Official Neko overlay sends native WebRTC data-channel input after the server transfers host ownership. Bot HTTP input is blocked for this lease.':'Human input and normalized automation both use the adapter HTTP lease gate, then Neko’s v3 control websocket. The stream remains WebRTC. Take control before typing.';
const layer=$('gated');layer.addEventListener('contextmenu',e=>e.preventDefault());
layer.addEventListener('pointerdown',e=>{if(!lease)return;e.preventDefault();layer.focus();layer.setPointerCapture(e.pointerId);queue([{type:'pointer',...pos(e),action:'down',button:['left','middle','right'][e.button]}]);});
layer.addEventListener('pointermove',e=>{if(!lease||performance.now()-lastMove<16)return;lastMove=performance.now();queue([{type:'pointer',...pos(e),action:'move'}]);});
layer.addEventListener('pointerup',e=>{e.preventDefault();queue([{type:'pointer',...pos(e),action:'up',button:['left','middle','right'][e.button]}]);});
layer.addEventListener('pointercancel',()=>queue([{type:'releaseAll'}]));
layer.addEventListener('wheel',e=>{e.preventDefault();queue([{type:'scroll',...pos(e),deltaX:Math.round(e.deltaX),deltaY:Math.round(e.deltaY)}]);},{passive:false});
for(const name of ['keydown','keyup'])layer.addEventListener(name,e=>{if(!lease)return;e.preventDefault();if(!e.repeat)queue([{type:'key',key:e.key===' '?'Space':e.key,action:name==='keydown'?'down':'up'}]);});
// Closing, leaving, or hiding a controlling tab revokes at the server; SDK key reset alone is insufficient.
window.addEventListener('blur',()=>{if(lease)release(true);});window.addEventListener('pagehide',()=>{if(lease)release(true);});document.addEventListener('visibilitychange',()=>{if(document.hidden&&lease)release(true);});
setInterval(async()=>{try{const c=await api('/control');if(lease&&c.session!==session)lease=null;else if(lease)lease=await api('/control',{action:'acquire',owner:'human',session,token:lease?.token,leaseMs:30000,mode:native?'native':'gated'});syncCursor();const v=client?.transport.element;$('metrics').textContent=JSON.stringify({owner:c.owner??'none',mode:c.mode??'none',decodedVideo:v?`${v.videoWidth}×${v.videoHeight}`:'pending',currentTime:v?.currentTime,playback:v?.getVideoPlaybackQuality?.(),queuedInputDropped:dropped},null,2);}catch(e){error(e);}},3000);
connect().catch(error);
function fitScreen(){
 const s=client?.state.screen.size;if(!s?.width)return;
 const stage=$('stage'),w=Math.min(stage.clientWidth,stage.clientHeight*s.width/s.height),h=w*s.height/s.width,x=(stage.clientWidth-w)/2,y=(stage.clientHeight-h)/2;
 Object.assign($('media').style,{width:w+'px',height:h+'px',marginLeft:x+'px',marginTop:y+'px'});
 Object.assign($('gated').style,{width:w+'px',height:h+'px',left:x+'px',top:y+'px',right:'auto',bottom:'auto'});
 syncCursor();
}
new ResizeObserver(fitScreen).observe($('stage'));
async function toggleFullscreen(){try{if(document.fullscreenElement)await document.exitFullscreen();else await $('stage').requestFullscreen();}catch(e){error(e);}}
$('fullscreen').onclick=toggleFullscreen;$('exit-fullscreen').onclick=toggleFullscreen;
window.addEventListener('keydown',e=>{
 if(e.key==='Escape'&&document.fullscreenElement){e.preventDefault();e.stopImmediatePropagation();document.exitFullscreen().catch(error);}
},{capture:true});
document.addEventListener('fullscreenchange',async()=>{
 const restore=!!lease;$('fullscreen').textContent=document.fullscreenElement?'Exit viewer fullscreen':'Viewer fullscreen';fitScreen();
 // Release at the server, clear queued events, reset the published SDK overlay
 // through its attach disposer, then restore this viewer's human ownership.
 // Browser Escape/F11 UI transitions must never leave guest modifiers held.
 if(restore){await release();if(native&&overlay)detachOverlay=overlay.attach($('media'));await take().catch(error);}
});
