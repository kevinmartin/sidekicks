import test from 'node:test';
import assert from 'node:assert/strict';
import {allowed,rewriteSignal,variants,upgradeURL} from './bridge.mjs';
test('malformed WebSocket request targets are rejected without throwing',()=>{
 for(const target of ['http://[','http://%','http://[::1'])assert.equal(upgradeURL(target),null);
 assert.equal(upgradeURL('/websockify?session=demo').pathname,'/websockify');
});
test('only viewer routes are exposed, never host/guest admin, launch or workbench APIs',()=>{
 for(const v of variants){
  for(const p of ['/api/session','/api/adapters/neko/input','/home','/desktop/launch','/rpc','/exec','/screen','/fixture-state'])for(const method of ['GET','POST'])assert.equal(allowed(v,method,p),false,`${v.id} ${method} ${p}`);
  assert.equal(allowed(v,'GET','/'),true);assert.equal(allowed(v,'POST','/input'),true);
 }
 assert.equal(allowed(variants[0],'GET','/novnc/../../server.py'),false);
});
test('ICE rewrite preserves credentials and maps only the fixed server UDP range',()=>{
 const v={...variants[1]},m={event:'signal/provide',payload:{sdp:'a=ice-pwd:keep-this\r\na=candidate:abc 1 udp 2130706431 127.0.0.1 59027 typ host\r\n'}};
 const result=rewriteSignal(m,v,'192.168.0.97');
 assert.match(result.payload.sdp,/192.168.0.97 59047 typ host/);assert.match(result.payload.sdp,/ice-pwd:keep-this/);
 assert.match(m.payload.sdp,/127.0.0.1 59027/);
 for(const candidate of ['candidate:a 1 tcp 1 127.0.0.1 59027 typ host','candidate:a 1 udp 1 127.0.0.1 8080 typ host','candidate:a 1 udp 1 10.0.0.3 59027 typ host'])assert.equal(rewriteSignal({event:'signal/candidate',payload:{candidate}},v,'192.168.0.97').payload.candidate,candidate);
});
test('NAT candidates map to their own public UDP ports; non-signal messages stay unchanged',()=>{
 const v={...variants[4],address:'192.168.64.4'};
 assert.match(rewriteSignal({event:'signal/candidate',payload:{candidate:'candidate:x 1 udp 1 192.168.64.4 59239 typ host'}},v,'192.168.0.97').payload.candidate,/192.168.0.97 59259/);
 const m={event:'system/init',payload:{value:'candidate:x 1 udp 1 192.168.64.4 59239 typ host'}};assert.equal(rewriteSignal(m,v,'192.168.0.97'),m);
});
