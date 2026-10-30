import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { performance } from 'node:perf_hooks';
import http from 'node:http';

const id = process.argv[2];
if (!['novnc', 'neko'].includes(id)) throw Error('Usage: node qa/contract.mjs novnc|neko [--active]');
const active = process.argv.includes('--active');
const base = `http://127.0.0.1:${id === 'novnc' ? 7791 : 7792}`;
const evidence = path.resolve('artifacts/independent-qa');
await fs.mkdir(evidence, { recursive: true });
const results = [];
const leases = [];
const redact = value => JSON.parse(JSON.stringify(value, (key, v) => /token/i.test(key) ? '[redacted]' : key === 'bytes' && v instanceof Uint8Array ? { length: v.length } : v));
async function request(route, body, headers = {}, method) {
  const start = performance.now();
  const response = await new Promise((resolve,reject)=>{
    const req=http.request(base+route,{method:method||(body===undefined?'GET':'POST'),headers:{...(body===undefined?{}:{'Content-Type':'application/json'}),...headers},timeout:6000},res=>{
      const chunks=[];res.on('data',c=>chunks.push(c));res.on('end',()=>resolve({status:res.statusCode,headers:res.headers,bytes:new Uint8Array(Buffer.concat(chunks))}));
    });req.on('error',reject);req.on('timeout',()=>req.destroy(Error('HTTP timeout')));
    if(body!==undefined)req.write(typeof body==='string'?body:JSON.stringify(body));req.end();
  });
  const bytes = response.bytes;
  let data;
  try { data = JSON.parse(new TextDecoder().decode(bytes)); } catch { data = new TextDecoder().decode(bytes).slice(0, 300); }
  return { status: response.status, headers: response.headers, data, bytes, httpMs: +(performance.now() - start).toFixed(3) };
}
async function check(name, fn) {
  try { const detail = await fn(); results.push({ name, status: 'PASS', detail: redact(detail) }); }
  catch (error) { results.push({ name, status: 'FAIL', error: error.message }); }
  console.log(`${results.at(-1).status} ${name}`);
}
const rejected = r => assert.ok([400, 403, 409, 413, 415, 422].includes(r.status), JSON.stringify(redact(r.data)));
const session = suffix => `independent-${id}-${Date.now()}-${suffix}`;
async function acquire(owner, suffix, leaseMs = 10000) {
  const s = session(suffix), r = await request('/control', { action: 'acquire', owner, session: s, leaseMs });
  assert.equal(r.status, 200, JSON.stringify(r.data));
  assert.ok(typeof r.data.token === 'string' && r.data.token.length >= 16);
  const lease = { owner, session: s, token: r.data.token };
  leases.push(lease);
  return lease;
}
async function release(lease) { return request('/control', { action: 'release', ...lease }); }
try {
  let health;
  try { health = await request('/health'); }
  catch (error) {
    results.push({ name: 'backend readiness', status: 'UNTESTED', reason: `Service unavailable: ${error.cause?.code || error.message}. Pending builder handover; not a completed verification.` });
  }
  if (health) {
    await check('health reports actual ready runtime', async () => { assert.equal(health.status, 200); assert.equal(health.data.ok, true); assert.equal(health.data.id, id); assert.ok(health.data.runtime?.platform); return health; });
    await check('capability declarations are explicit', async () => { const r = await request('/capabilities'); assert.equal(r.status, 200); assert.equal(r.data.id, id); for (const key of ['keyboard','click','rightClick','doubleClick','drag','scroll','modifiers','pinchNative','swipeNative','screenshot','audio','theme','humanTakeover']) assert.ok(r.data.capabilities?.[key]?.status, key); return r; });
    await check('control does not disclose lease token', async () => { const r = await request('/control'); assert.equal(r.status, 200); assert.equal(r.data.token, undefined); return r; });
    await check('external browser Origin rejected on mutating route', async () => { const r = await request('/input', {}, { Origin: 'https://untrusted.example' }); assert.equal(r.status, 403); return r; });
    await check('null Origin rejected', async () => { const r = await request('/input', {}, { Origin: 'null' }); assert.equal(r.status, 403); return r; });
    await check('DNS-rebound Host rejected', async () => { const r = await request('/health', undefined, { Host: 'untrusted.example' }); assert.equal(r.status, 403); return r; });
    await check('simple form Content-Type rejected', async () => { const r = await request('/input', 'events=bad', { 'Content-Type': 'application/x-www-form-urlencoded' }); assert.equal(r.status, 415); return r; });
    await check('missing lease rejects input', async () => { const r = await request('/input', { session: session('none'), token: 'stale-not-a-token', events: [{ type: 'releaseAll' }] }); rejected(r); return r; });
    await check('oversize event batch rejected', async () => { const r = await request('/input', { session: session('none'), token: 'stale-not-a-token', events: Array.from({ length: 101 }, () => ({ type: 'releaseAll' })) }); rejected(r); return r; });
    await check('shell-shaped input is unsupported', async () => { const r = await request('/input', { session: session('none'), token: 'stale-not-a-token', events: [{ type: 'shell', command: 'echo sidekicks-synthetic' }] }); rejected(r); return r; });
    await check('screenshot bounds rejected above contract', async () => { const r = await request('/screenshot?maxWidth=1921&maxHeight=1081'); assert.equal(r.status, 400); return r; });
    await check('screenshot binary has bounded dimensions', async () => {
      const r = await request('/screenshot?maxWidth=640&maxHeight=360');
      assert.equal(r.status, 200); assert.ok(r.bytes.length < 4 * 1024 * 1024); assert.match(r.headers['content-type'], /image\/(png|jpeg)/);
      let dimensions;
      if (r.bytes.slice(0,8).toString() === '137,80,78,71,13,10,26,10') {
        const view = new DataView(r.bytes.buffer, r.bytes.byteOffset, r.bytes.byteLength);
        dimensions = { width: view.getUint32(16), height: view.getUint32(20) };
        assert.ok(dimensions.width > 0 && dimensions.width <= 640 && dimensions.height > 0 && dimensions.height <= 360);
      } else {
        assert.equal(r.bytes[0],255);assert.equal(r.bytes[1],216);
        let offset=2;
        while(offset<r.bytes.length){
          if(r.bytes[offset++]!==255)continue;
          const marker=r.bytes[offset++];if(marker===216||marker===217)continue;
          const size=(r.bytes[offset]<<8)|r.bytes[offset+1];
          if([192,193,194,195,197,198,199,201,202,203,205,206,207].includes(marker)){
            dimensions={height:(r.bytes[offset+3]<<8)|r.bytes[offset+4],width:(r.bytes[offset+5]<<8)|r.bytes[offset+6]};break;
          }offset+=size;
        }
        assert.ok(dimensions?.width>0&&dimensions.width<=640&&dimensions.height>0&&dimensions.height<=360);
      }
      const file = path.join(evidence, `${id}-contract-capture.${r.headers['content-type'].includes('jpeg')?'jpg':'png'}`); await fs.writeFile(file, r.bytes);
      return { file, dimensions, bytes: r.bytes.length, httpMs: r.httpMs, headers: r.headers, limitation: 'Binary dimensions only; fresh marker and real viewer are separately verified.' };
    });
    if (active) {
      const original = await request('/control');
      assert.ok(!original.data.owner, 'Cannot begin active checks: desktop has an owner. Wait for coordinated test window.');
      await check('human preemption and stale agent token rejection', async () => {
        const agent = await acquire('agent', 'agent');
        const stolen=await request('/control',{action:'acquire',owner:agent.owner,session:agent.session,leaseMs:10000});
        assert.ok([403,409].includes(stolen.status),'Copied public session must not recover an existing lease token');
        const human = await acquire('human', 'human');
        const stale = await request('/input', { ...agent, events: [{ type: 'releaseAll' }] }); rejected(stale);
        const forbidden = await request('/control', { action: 'acquire', owner: 'agent', session: session('intruder'), leaseMs: 10000 }); assert.equal(forbidden.status, 409);
        const badRelease = await request('/control', { action: 'release', ...agent }); rejected(badRelease);
        const state = await request('/control'); assert.equal(state.data.owner, 'human'); assert.equal(state.data.session, human.session);
        assert.equal((await release(human)).status, 200);
        return { staleStatus: stale.status, agentAcquireStatus: forbidden.status, state };
      });
      await check('invalid whole batch has no accepted input', async () => {
        const agent = await acquire('agent', 'invalid');
        const r = await request('/input', { ...agent, events: [{ type: 'key', key: 'Shift', action: 'down' }, { type: 'shell', command: 'echo synthetic' }] }); rejected(r);
        const state=await request('/control');
        let clean;
        if(state.data.owner){clean=await request('/input',{...agent,events:[{type:'releaseAll'}]});assert.equal(clean.status,200);assert.equal((await release(agent)).status,200);}
        return { rejectionStatus:r.status, cleanupStatus:clean?.status, state, limitation:'Fixture evidence is required to prove first key was never delivered. Either cleanup or safe ownership revocation is accepted.' };
      });
      await check('lease expiry rejects old token', async () => {
        const agent = await acquire('agent', 'expiry', 1000);
        await new Promise(resolve => setTimeout(resolve, 1300));
        const stale = await request('/input', { ...agent, events: [{ type: 'releaseAll' }] }); rejected(stale);
        const state = await request('/control'); assert.ok(!state.data.owner);
        return { rejectionStatus: stale.status, state };
      });
      await check('simultaneous agent requests acquire at most one owner', async () => {
        const a = session('raceA'), b = session('raceB');
        const responses = await Promise.all([a,b].map(session => request('/control', { action:'acquire', owner:'agent', session, leaseMs:10000 })));
        assert.deepEqual(responses.map(r=>r.status).sort(), [200,409]);
        const winner = responses.findIndex(r=>r.status===200);
        const lease = { owner:'agent', session:[a,b][winner], token:responses[winner].data.token }; leases.push(lease);
        assert.equal((await release(lease)).status,200);
        return responses.map(r=>({status:r.status,data:r.data}));
      });
    } else results.push({ name: 'active lease/takeover/race checks', status: 'UNTESTED', reason: 'Run --active only in coordinated exclusive desktop window.' });
  }
} finally {
  for (const lease of leases) await release(lease).catch(()=>{});
  const report = { generatedAt: new Date().toISOString(), independent: true, backend: id, base, active, results };
  await fs.writeFile(path.join(evidence, `${id}-contract${active ? '-active' : ''}.json`), JSON.stringify(report, null, 2));
}
if (results.some(r=>r.status==='FAIL')) process.exitCode=1;
