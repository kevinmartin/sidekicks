import { randomUUID } from 'node:crypto';
export class HttpError extends Error { constructor(status, message) { super(message); this.status = status; } }

// Single enforcement point. Every queued event rechecks its generation after yielding.
export class LeaseGate {
  current = null; generation = 0; revocation = 0; pending = Promise.resolve();
  constructor(driver, now = () => Date.now()) { this.driver = driver; this.now = now; }
  view() { const c = this.current; return c ? { owner:c.owner, session:c.session, mode:c.mode, expiresAt:new Date(c.until).toISOString() } : {owner:null}; }
  async expire() { if (this.current && this.current.until <= this.now()) await this.stop(); }
  async stop() {
    this.current = null; this.generation++; this.revocation++;
    this.pending = this.pending.catch(()=>{}).then(()=>this.driver.reset());
    await this.pending;
    return {ok:true,owner:null};
  }
  async acquire({owner, session, token, leaseMs=30000, mode='gated', viewerId}) {
    if (!['human','agent'].includes(owner) || typeof session !== 'string' || !/^[\w.-]{1,128}$/.test(session)) throw new HttpError(400,'invalid owner/session');
    if (!['gated','native'].includes(mode) || (mode === 'native' && (owner !== 'human' || !viewerId))) throw new HttpError(400,'native mode requires a connected human viewer');
    if (!Number.isFinite(leaseMs) || leaseMs < 100 || leaseMs > 60000) throw new HttpError(400,'leaseMs must be 100..60000');
    // Mark preemption immediately, before asynchronous host transfer. Old input jobs stop.
    if (owner === 'human' && this.current?.owner === 'agent') { this.current = null; this.generation++; this.revocation++; }
    const ticket = this.revocation;
    const operation = this.pending.catch(()=>{}).then(async()=>{
      if (ticket !== this.revocation) throw new HttpError(409,'acquisition cancelled');
      if (!this.driver.ready) throw new HttpError(503,'Neko control socket is not ready');
      let c = this.current;
      if (owner === 'human' && c?.owner === 'agent') { this.current = null; this.generation++; c = null; }
      if (c && c.until > this.now()) {
        if (c.session !== session || c.owner !== owner || c.mode !== mode) throw new HttpError(409,`${c.owner} owns the desktop`);
        if (c.token !== token) throw new HttpError(403,'current token required to renew lease');
        c.until = this.now()+leaseMs;
        return this.result();
      }
      this.current = null; this.generation++;
      await this.driver.reset();
      if (mode === 'native') await this.driver.give(viewerId);
      if (ticket !== this.revocation) throw new HttpError(409,'acquisition cancelled');
      this.current = {owner,session,mode,viewerId,token:randomUUID(),until:this.now()+leaseMs};
      return this.result();
    });
    this.pending = operation.then(()=>{},()=>{});
    return operation;
  }
  result() { return {ok:true,token:this.current.token,...this.view()}; }
  validate({token,session}, generation = this.generation) {
    const c = this.current;
    if (!c || c.until <= this.now() || generation !== this.generation || c.token !== token || c.session !== session) throw new HttpError(409,'missing, stale, expired, or preempted lease');
    return c;
  }
  async release(body) { this.validate(body); return this.stop(); }
}
