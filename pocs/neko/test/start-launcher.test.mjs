import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';

const exec=promisify(execFile),root=new URL('../',import.meta.url).pathname;
const release=JSON.parse(await fs.readFile(new URL('../current-image.json',import.meta.url),'utf8'));
const frozen={Id:release.imageId,Architecture:'arm64',Os:'linux'};
const own=image=>({Image:image,Config:{Labels:{component:'neko'}},State:{Status:'exited'}});
const fake=`#!/usr/bin/env python3
import json,os,sys
p=os.environ['FAKE_DOCKER_STATE']; s=json.load(open(p)); a=sys.argv[1:]
s['calls'].append(a)
with open(p,'w') as f:json.dump(s,f)
if a[:2]==['image','inspect']:
 if s.get('image') is None:sys.exit(1)
 print(json.dumps([s['image']]))
elif a[:2]==['container','inspect']:
 if s.get('container') is None:sys.exit(1)
 print(json.dumps([s['container']]))
elif a[0] in ['stop','rename','start','run']:print('fake-only')
else:raise Exception('unexpected Docker action '+repr(a))
`;
async function run({image=frozen,container=null,args=[],env={}}={}){
 const dir=await fs.mkdtemp(path.join(os.tmpdir(),'sidekicks-neko-launcher-'));
 try{
  const state=path.join(dir,'state.json');await fs.writeFile(state,JSON.stringify({image,container,calls:[]}));
  await fs.writeFile(path.join(dir,'docker'),fake,{mode:0o755});
  const vars={...process.env,PATH:dir+path.delimiter+process.env.PATH,FAKE_DOCKER_STATE:state};
  delete vars.SIDEKICKS_NEKO_IMAGE;delete vars.SIDEKICKS_NEKO_BUILD;
  let result;try{result={...await exec('bash',[root+'start.sh',...args],{env:{...vars,...env},timeout:10000}),code:0};}catch(e){result={stdout:e.stdout,stderr:e.stderr,code:e.code};}
  return{...result,calls:JSON.parse(await fs.readFile(state,'utf8')).calls};
 }finally{await fs.rm(dir,{recursive:true,force:true});}
}
const mutations=r=>r.calls.filter(a=>!['image','container'].includes(a[0]));

test('plain start resumes the stopped matching container without rebuilding',async()=>{
 const r=await run({container:own(release.imageId)});assert.equal(r.code,0);assert.deepEqual(mutations(r),[['start','sidekicks-neko']]);
});
test('missing frozen image fails before runtime operations or implicit downloads',async()=>{
 const r=await run({image:null});assert.equal(r.code,1);assert.match(r.stderr,/No download or fallback/);assert.equal(r.calls.length,1);assert.deepEqual(mutations(r),[]);
});
test('retagged frozen release is rejected',async()=>{
 const r=await run({image:{...frozen,Id:'sha256:changed'}});assert.equal(r.code,1);assert.match(r.stderr,/no longer matches/);assert.deepEqual(mutations(r),[]);
});
test('different existing image needs explicit replacement',async()=>{
 const r=await run({container:own('sha256:old')});assert.equal(r.code,1);assert.match(r.stderr,/--replace/);assert.deepEqual(mutations(r),[]);
});
test('explicit replacement preserves old container before bounded loopback-only run',async()=>{
 const r=await run({container:own('sha256:old'),args:['--replace']});assert.equal(r.code,0);
 const m=mutations(r);assert.deepEqual(m[0],['stop','-t','10','sidekicks-neko']);assert.equal(m[1][0],'rename');assert.match(m[1][2],/^sidekicks-neko-preserved-/);assert.equal(m[2][0],'run');
 assert.equal(m.length,3);const command=m[2];assert(command.includes('--pull=never'));assert(command.includes('--cpus=2'));assert(command.includes('--memory=2g'));assert(command.includes('--security-opt=no-new-privileges:true'));assert.equal(command.at(-1),release.imageId);
 assert.deepEqual(command.flatMap((v,i)=>v==='-p'?[command[i+1]]:[]),['127.0.0.1:7792:7792','127.0.0.1:7902:7902','127.0.0.1:59020-59039:59020-59039/udp']);
 assert(!command.some(v=>['--privileged','-v','--mount'].includes(v)));assert(!r.calls.some(a=>['rm','build','pull'].includes(a[0])));
});
test('unrelated container cannot be replaced even with explicit flag',async()=>{
 const r=await run({container:{...own('sha256:old'),Config:{Labels:{component:'other'}}},args:['--replace']});assert.equal(r.code,1);assert.deepEqual(mutations(r),[]);
});
test('legacy implicit build flag is rejected without any Docker call',async()=>{
 const r=await run({env:{SIDEKICKS_NEKO_BUILD:'1'}});assert.equal(r.code,1);assert.match(r.stderr,/does not build/);assert.deepEqual(r.calls,[]);
});
