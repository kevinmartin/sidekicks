import http from 'node:http';
import dgram from 'node:dgram';
import os from 'node:os';
import fs from 'node:fs/promises';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
const require=createRequire(new URL('../neko/package.json',import.meta.url));
const {WebSocket,WebSocketServer}=require('ws');
const here=path.dirname(fileURLToPath(import.meta.url));
export const variants=[
 {id:'novnc-docker',name:'noVNC · Docker',kind:'novnc',port:17901,target:7901},
 {id:'neko-docker',name:'Neko · Docker / WebRTC',kind:'neko',port:17902,target:7902,udp:59040,dest:59020,address:'127.0.0.1'},
 {id:'novnc-vm',name:'noVNC · microVM / no NIC',kind:'novnc',port:17903,target:7903},
 {id:'neko-relay',name:'Neko · microVM / UDP over vsock',kind:'neko',port:17906,target:7906,udp:59140,dest:59120,address:'127.0.0.1'},
 {id:'neko-nat',name:'Neko · NAT microVM / host UDP forwarder',kind:'neko',port:17907,target:7907,udp:59240,dest:59220,address:null}
];
export function rewriteSignal(message,v,advertise){
 if(!message.event?.startsWith('signal/'))return message;
 const convert=value=>{
  if(typeof value==='string')return value.replace(/(candidate:\S+\s+\d+\s+udp\s+\d+\s+)(\S+)(\s+)(\d+)(\s+typ\s+\S+)/gi,(all,prefix,ip,space,port,tail)=>{
   const n=Number(port);return (ip===v.address&&n>=v.dest&&n<v.dest+20)?`${prefix}${advertise}${space}${v.udp+n-v.dest}${tail}`:all;
  });
  if(Array.isArray(value))return value.map(convert);
  if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).map(([k,x])=>[k,convert(x)]));
  return value;
 };
 return {...message,payload:convert(message.payload)};
}
export function allowed(v,method,p){
 if(method==='GET'&&['/','/health','/control','/screenshot','/lan/mobile.js','/lan/index'].includes(p))return true;
 if(method==='POST'&&['/control','/input'].includes(p))return true;
 if(v.kind==='novnc')return method==='GET'&&(p==='/web/viewer.css'||/^\/novnc\/[\w./-]+$/.test(p)&&!p.split('/').includes('..'));
 return method==='GET'&&['/viewer.js','/stream/api/whoami','/stream/api/room/screen','/stream/api/room/screen/configurations'].includes(p)||method==='POST'&&p==='/viewer-session';
}
export function upgradeURL(target){try{return new URL(target,'http://localhost');}catch{return null;}}
function safeAuthority(req,port){
 try{
  const u=new URL('http://'+req.headers.host);
  const ips=new Set(['127.0.0.1','localhost',...Object.values(os.networkInterfaces()).flat().filter(Boolean).map(x=>x.address)]);
  return Number(u.port)===port&&ips.has(u.hostname)&&(!req.headers.origin||req.headers.origin===u.origin);
 }catch{return false;}
}
const json=(res,status,data)=>{res.writeHead(status,{'content-type':'application/json','cache-control':'no-store'});res.end(JSON.stringify(data));};
async function readBody(req){let body='';for await(const c of req){body+=c;if(Buffer.byteLength(body)>65536)throw Object.assign(new Error('request too large'),{status:413});}return body?JSON.parse(body):{};}
function upstreamHeaders(req,v){
 const headers={host:`127.0.0.1:${v.target}`,origin:`http://127.0.0.1:${v.target}`};
 for(const k of ['content-type','authorization','sec-websocket-protocol'])if(req.headers[k])headers[k]=req.headers[k];
 return headers;
}
export async function main(advertise,natAddress){
 if(!/^\d+\.\d+\.\d+\.\d+$/.test(advertise)||!/^192\.168\.64\.\d+$/.test(natAddress))throw Error('explicit LAN IPv4 and current VM NAT address required');
 const localIPs=Object.values(os.networkInterfaces()).flat().filter(Boolean).map(x=>x.address);
 if(!localIPs.includes(advertise))throw Error('advertised address is not assigned to this Mac');
 const resources=[],leases=new Map(),stats={startedAt:new Date().toISOString(),advertise,udpReceived:0,udpReturned:0,udpDropped:0,rewrittenSignals:0,websocketConnections:0};let timer,closing=false;
 const mobile=await fs.readFile(path.join(here,'mobile.js'));
 const fail=(res,e)=>json(res,e.status||502,{error:e.message});
 for(const original of variants){
  const v={...original,address:original.id==='neko-nat'?natAddress:original.address};
  const server=http.createServer(async(req,res)=>{
   try{
    if(!safeAuthority(req,v.port))return json(res,403,{error:'viewer Host/Origin mismatch'});
    const u=new URL(req.url,'http://localhost');
    if(!allowed(v,req.method,u.pathname))return json(res,403,{error:'LAN bridge exposes human viewer routes only'});
    if(u.pathname==='/lan/mobile.js'){res.writeHead(200,{'content-type':'text/javascript','cache-control':'no-store'});return res.end(mobile);}
    if(u.pathname==='/lan/index'){res.writeHead(200,{'content-type':'text/html','cache-control':'no-store'});return res.end('<meta name="viewport" content="width=device-width,initial-scale=1"><title>Sidekicks LAN demos</title><h1>Sidekicks LAN demos</h1><p>Temporary unauthenticated HTTP POC. Trusted network and synthetic content only.</p>'+variants.map(x=>`<p><a href="http://${advertise}:${x.port}/">${x.name}</a></p>`).join(''));}
    let body;
    if(req.method==='POST'){
     if(!String(req.headers['content-type']).startsWith('application/json'))return json(res,415,{error:'JSON required'});
     body=await readBody(req);
     if(u.pathname==='/control'&&(!['acquire','release','stop'].includes(body.action)||body.action==='acquire'&&body.owner!=='human'))return json(res,403,{error:'LAN viewer can acquire human control only'});
     if(u.pathname==='/input'){
      const permit=leases.get(body.token);
      if(!permit||permit.variant!==v.id||permit.session!==body.session||permit.expiresAt<Date.now())return json(res,409,{error:'LAN human lease required'});
     }
    }
    const upstream=http.request({host:'127.0.0.1',port:v.target,path:u.pathname+u.search,method:req.method,headers:upstreamHeaders(req,v),timeout:10000},remote=>{
     const pieces=[];let bytes=0;
     remote.on('data',c=>{bytes+=c.length;if(bytes>5*1024*1024){remote.destroy(Error('response too large'));return;}pieces.push(c);});
     remote.on('error',e=>{if(!res.headersSent)fail(res,e);else res.destroy();});
     remote.on('end',()=>{
      let data=Buffer.concat(pieces);
      if(u.pathname==='/control'&&req.method==='POST'&&remote.statusCode===200){try{
       const answer=JSON.parse(data);
       if(answer.token&&answer.owner==='human'){for(const [token,permit]of leases)if(permit.variant===v.id)leases.delete(token);leases.set(answer.token,{variant:v.id,session:answer.session,expiresAt:Date.parse(answer.expiresAt)});}
       if(body.action==='release'&&body.token)leases.delete(body.token);
      }catch{}}
      if(u.pathname==='/'&&remote.statusCode===200){
       let html=data.toString().replace('<!doctype html>','<!doctype html><script src="/lan/mobile.js"></script>');
       html=html.replace('ws://${location.host}/websockify','${location.protocol===\'https:\'?\'wss:\':\'ws:\'}//${location.host}/websockify');
       data=Buffer.from(html);
      }
      res.writeHead(remote.statusCode,{'content-type':remote.headers['content-type']||'application/octet-stream','cache-control':'no-store','x-content-type-options':'nosniff','referrer-policy':'no-referrer','x-sidekicks-lan-variant':v.id,
       'content-security-policy':"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; media-src 'self' blob:; img-src 'self' data: blob:; object-src 'none'; frame-ancestors 'none'"});res.end(data);
     });
    });
    upstream.on('timeout',()=>upstream.destroy(Error('local viewer timeout')));upstream.on('error',e=>{if(!res.headersSent)fail(res,e);else res.destroy();});
    upstream.end(body===undefined?undefined:JSON.stringify(body));
   }catch(e){fail(res,e);}
  });
  server.headersTimeout=10000;server.requestTimeout=15000;
  const wss=new WebSocketServer({noServer:true,maxPayload:1048576});
  server.on('upgrade',(req,socket,head)=>{
   const u=upgradeURL(req.url),expected=v.kind==='novnc'?'/websockify':'/stream/api/ws';
   if(!u){socket.end('HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n');return;}
   if(!safeAuthority(req,v.port)||u.pathname!==expected){socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n');return;}
   try{wss.handleUpgrade(req,socket,head,client=>{
    stats.websocketConnections++;
    const protocols=req.headers['sec-websocket-protocol']?.split(',').map(x=>x.trim());
    const target=new WebSocket(`ws://127.0.0.1:${v.target}${u.pathname}${u.search}`,protocols,{headers:{origin:`http://127.0.0.1:${v.target}`},maxPayload:1048576});
    const pending=[];let queued=0;
    client.on('message',(data,binary)=>{if(target.bufferedAmount>1048576){client.close(1009);return;}if(target.readyState===WebSocket.OPEN)target.send(data,{binary});else if(queued+data.length<=262144){pending.push([data,binary]);queued+=data.length;}else client.close(1009);});
    target.on('open',()=>pending.splice(0).forEach(([data,binary])=>target.send(data,{binary})));
    target.on('message',(data,binary)=>{
     if(client.readyState!==WebSocket.OPEN)return;if(client.bufferedAmount>4*1024*1024){client.close(1013);return;}
     if(v.kind==='neko'&&!binary){try{const m=JSON.parse(data),mapped=rewriteSignal(m,v,advertise);const text=JSON.stringify(mapped);if(text!==data.toString())stats.rewrittenSignals++;client.send(text);return;}catch{}}
     client.send(data,{binary});
    });
    const close=()=>{client.close();target.close();};client.on('close',close);target.on('close',close);client.on('error',close);target.on('error',close);
   });}catch{socket.destroy();}
  });
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(v.port,'0.0.0.0',resolve);});resources.push(()=>server.close());
  if(v.udp){let peerCount=0;for(let offset=0;offset<20;offset++){
   const listener=dgram.createSocket('udp4'),peers=new Map();
   listener.on('message',(packet,source)=>{
    stats.udpReceived++;if(packet.length>9216){stats.udpDropped++;return;}
    const key=source.address+':'+source.port;let peer=peers.get(key);
    if(!peer){
     if(peerCount>=64){stats.udpDropped++;return;}
     const outbound=dgram.createSocket('udp4');peer={outbound,last:Date.now(),ready:false,pending:[],bytes:0};peers.set(key,peer);peerCount++;
     outbound.on('message',reply=>{stats.udpReturned++;listener.send(reply,source.port,source.address,e=>{if(e)stats.udpDropped++;});});
     outbound.on('error',()=>{stats.udpDropped++;if(peers.delete(key)){peerCount--;outbound.close();}});
     outbound.connect(v.dest+offset,v.address,()=>{peer.ready=true;for(const data of peer.pending)outbound.send(data,e=>{if(e)stats.udpDropped++;});peer.pending=[];peer.bytes=0;});
    }
    peer.last=Date.now();if(peer.ready)peer.outbound.send(packet,e=>{if(e)stats.udpDropped++;});else if(peer.bytes+packet.length<=262144){peer.pending.push(packet);peer.bytes+=packet.length;}else stats.udpDropped++;
   });
   listener.on('error',e=>{console.error('UDP listener',v.udp+offset,e.message);process.exitCode=1;shutdown();});
   await new Promise((resolve,reject)=>{listener.once('error',reject);listener.bind(v.udp+offset,'0.0.0.0',resolve);});
   const timer=setInterval(()=>{for(const [key,p] of peers)if(Date.now()-p.last>60000){p.outbound.close();peers.delete(key);peerCount--;}},5000);timer.unref();
   resources.push(()=>{clearInterval(timer);for(const p of peers.values())p.outbound.close();listener.close();});
  }}
 }
 timer=setInterval(()=>{for(const [t,l]of leases)if(l.expiresAt<Date.now())leases.delete(t);console.log(JSON.stringify({...stats,at:new Date().toISOString()}));},30000);timer.unref();
 function shutdown(){if(closing)return;closing=true;clearInterval(timer);for(const f of resources.reverse())try{f();}catch{}setTimeout(()=>process.exit(process.exitCode||0),250).unref();}
 process.on('SIGTERM',shutdown);process.on('SIGINT',shutdown);
 console.log(JSON.stringify({ready:true,...stats,viewers:variants.map(v=>({id:v.id,bind:`0.0.0.0:${v.port}`,url:`http://${advertise}:${v.port}/`,udp:v.udp?[v.udp,v.udp+19]:null}))}));
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv[2],process.argv[3]).catch(e=>{console.error(e.message);process.exit(1);});
