import test from 'node:test';
import assert from 'node:assert/strict';
import {requireDesktopPage,validateDesktopLaunch} from '../desktop-apps.mjs';
import {cursorStyle,observerPosition} from '../cursor.mjs';
const request=()=>({socket:{remoteAddress:'127.0.0.1'},headers:{host:'127.0.0.1:7792',origin:'http://127.0.0.1:7792','x-sidekicks-desktop':'page-token'}});
const gate={current:{token:'lease-token',session:'desktop'},validate(x){assert.equal(x.token,'lease-token');assert.equal(x.session,'desktop');}};
test('launcher requires guest page token, exact internal origin and active ownership',()=>{
  assert.equal(validateDesktopLaunch(request(),{app:'files'},'page-token',gate),'/etc/sidekicks/files.sh');
  for(const req of [ {...request(),socket:{remoteAddress:'172.17.0.1'}}, {...request(),socket:{remoteAddress:'127.0.0.2'}}, {...request(),headers:{...request().headers,host:'127.0.0.1:7902'}}, {...request(),headers:{...request().headers,origin:'http://127.0.0.1:7902'}}, {...request(),headers:{...request().headers,'x-sidekicks-desktop':'wrong'}} ]) assert.throws(()=>validateDesktopLaunch(req,{app:'terminal'},'page-token',gate),e=>e.status===403);
  assert.throws(()=>validateDesktopLaunch(request(),{app:'browser'},'page-token',{current:null}),e=>e.status===409);
});
test('launcher rejects shell commands, arbitrary arguments and inherited object keys',()=>{
  for(const body of [{app:'sh'},{app:'browser',url:'https://example.com'},{app:'terminal',args:['-e','anything']},{app:'__proto__'},{app:'constructor'}]) assert.throws(()=>validateDesktopLaunch(request(),body,'page-token',gate),e=>e.status===422);
  assert.throws(()=>validateDesktopLaunch(request(),{app:'browser'},'page-token',{current:gate.current,validate(){throw new Error('revoked');}}),/revoked/);
});
test('new-tab navigation may omit Origin but cannot fetch home through external viewer Host',()=>{
  const req=request();delete req.headers.origin;assert.doesNotThrow(()=>requireDesktopPage(req));
  req.headers.host='127.0.0.1:7902';assert.throws(()=>requireDesktopPage(req),e=>e.status===403);
});
test('cursor hotspot stays in image pixels while observer coordinates scale with video',()=>{
  const cursor={uri:'blob:test',width:32,height:32,x:5,y:9};
  assert.equal(cursorStyle(cursor),'url("blob:test") 5 9, default');
  assert.deepEqual(observerPosition({x:640,y:180},{width:1280,height:720},cursor),{left:'50%',top:'25%',transform:'translate(-5px, -9px)'});
  assert.equal(cursorStyle({width:1,height:1}),'none');
});
