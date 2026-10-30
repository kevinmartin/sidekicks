import {spawn} from 'node:child_process';
import {HttpError} from './lease.mjs';

const scripts = Object.freeze({browser:'/etc/sidekicks/browser.sh',terminal:'/etc/sidekicks/terminal.sh',files:'/etc/sidekicks/files.sh'});
export function requireDesktopPage(req) {
  // VM host tunnels deliberately use 127.0.0.2. Only the actual guest browser
  // at 127.0.0.1 may receive the desktop token, even with forged Host/Origin.
  const local = req.socket.remoteAddress === '127.0.0.1';
  if (!local || req.headers.host !== '127.0.0.1:7792' || (req.headers.origin && req.headers.origin !== 'http://127.0.0.1:7792')) throw new HttpError(403,'internal desktop page required');
}
export function validateDesktopLaunch(req, body, token, gate) {
  requireDesktopPage(req);
  if (req.headers.origin !== 'http://127.0.0.1:7792' || req.headers['x-sidekicks-desktop'] !== token) throw new HttpError(403,'desktop page token and same origin required');
  if (!body || Object.keys(body).some(k=>k!=='app') || !Object.hasOwn(scripts,body.app)) throw new HttpError(422,'choose browser, terminal or files');
  const c = gate.current;
  if (!c) throw new HttpError(409,'take control in the viewer before launching an app');
  gate.validate({token:c.token,session:c.session});
  return scripts[body.app];
}
export async function launchDesktopApp(script) {
  // Fixed executable, no shell, arguments, URL or filesystem path from the page.
  const child = spawn(script,[],{cwd:'/home/neko',detached:true,stdio:'ignore'});
  await new Promise((resolve,reject)=>{child.once('spawn',resolve);child.once('error',reject);});
  child.unref();
}
