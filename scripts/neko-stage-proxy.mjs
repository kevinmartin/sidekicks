// Loopback-only staging transport. Production viewers and LAN bridge are untouched.
import http from 'node:http';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../pocs/neko/package.json',import.meta.url));
const {WebSocket,WebSocketServer}=require('ws');
const listen=17802,target=17782,authority='127.0.0.1:7902';
function local(req){return ['127.0.0.1:17802','localhost:17802'].includes(req.headers.host)&&(!req.headers.origin||req.headers.origin==='http://'+req.headers.host);}
function headers(req){const h={host:authority,origin:'http://'+authority};for(const k of ['content-type','authorization','sec-websocket-protocol'])if(req.headers[k])h[k]=req.headers[k];return h;}
const server=http.createServer((req,res)=>{
 if(!local(req)){res.writeHead(403);res.end();return;}
 let url;try{url=new URL(req.url,'http://localhost');}catch{res.writeHead(400);res.end();return;}
 let bytes=0;
 const upstream=http.request({host:'127.0.0.1',port:target,path:url.pathname+url.search,method:req.method,headers:headers(req),timeout:10000},r=>{
  res.writeHead(r.statusCode,{'content-type':r.headers['content-type']||'application/octet-stream','cache-control':'no-store','x-sidekicks-stage':'loopback-only'});
  r.on('data',c=>{bytes+=c.length;if(bytes>5*1024*1024){upstream.destroy();res.destroy();}else res.write(c);});r.on('end',()=>res.end());r.on('error',()=>res.destroy());
 });
 upstream.on('error',()=>{if(!res.headersSent)res.writeHead(502);res.end();});upstream.on('timeout',()=>upstream.destroy());
 let sent=0;req.on('data',c=>{sent+=c.length;if(sent>65536){upstream.destroy();req.destroy();}else upstream.write(c);});req.on('end',()=>upstream.end());
});
const wss=new WebSocketServer({noServer:true,maxPayload:1048576});
server.on('upgrade',(req,socket,head)=>{
 let url;try{url=new URL(req.url,'http://localhost');}catch{socket.end('HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n');return;}
 if(!local(req)||url.pathname!=='/stream/api/ws'){socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n');return;}
 try{wss.handleUpgrade(req,socket,head,client=>{
  const upstream=new WebSocket(`ws://127.0.0.1:${target}${url.pathname}${url.search}`,{headers:{origin:'http://'+authority,host:authority},maxPayload:1048576});
  const pending=[];let queued=0;
  client.on('message',(data,binary)=>{if(upstream.bufferedAmount>1048576)return client.close(1009);if(upstream.readyState===1)upstream.send(data,{binary});else if((queued+=data.length)<=262144)pending.push([data,binary]);else client.close(1009);});
  upstream.on('open',()=>pending.splice(0).forEach(([data,binary])=>upstream.send(data,{binary})));
  upstream.on('message',(data,binary)=>{if(client.bufferedAmount>4*1024*1024)return client.close(1013);if(client.readyState===1)client.send(data,{binary});});
  const close=()=>{client.close();upstream.close();};client.on('close',close);upstream.on('close',close);client.on('error',close);upstream.on('error',close);
 });}catch{socket.destroy();}
});
server.headersTimeout=10000;server.requestTimeout=15000;
server.listen(listen,'127.0.0.1',()=>console.log('Neko stage viewer http://127.0.0.1:17802/ -> 127.0.0.1:17782; production unchanged'));
process.on('SIGTERM',()=>server.close(()=>process.exit(0)));
