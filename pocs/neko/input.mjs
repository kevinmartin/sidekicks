import { HttpError } from './lease.mjs';
export const keysyms = {Backspace:0xff08,Tab:0xff09,Enter:0xff0d,Escape:0xff1b,Delete:0xffff,Home:0xff50,End:0xff57,ArrowLeft:0xff51,ArrowUp:0xff52,ArrowRight:0xff53,ArrowDown:0xff54,PageUp:0xff55,PageDown:0xff56,Shift:0xffe1,Control:0xffe3,Alt:0xffe9,Meta:0xffeb,Super:0xffeb,Space:32};
for (let n=1;n<=12;n++) keysyms['F'+n]=0xffbd+n;
export function sym(key) { if(Object.hasOwn(keysyms,key))return keysyms[key];const value=typeof key==='string' && [...key].length===1 ? key.codePointAt(0) : undefined;if(value===undefined)throw new HttpError(422,'unsupported key');return value<=255?value:0x01000000|value; }
const buttons={left:1,middle:2,right:3};
export function validateEvents(events, size={width:1280,height:720}) {
  if (!Array.isArray(events) || !events.length || events.length>100) throw new HttpError(400,'events must contain 1..100 items');
  const pos=e=>{if(!Number.isInteger(e.x)||!Number.isInteger(e.y)||e.x<0||e.y<0||e.x>=size.width||e.y>=size.height)throw new HttpError(400,'coordinates outside desktop');};
  for (const e of events) {
    if (!e || typeof e!=='object') throw new HttpError(400,'invalid event');
    if (e.type==='key') { sym(e.key); if(!['down','up'].includes(e.action))throw new HttpError(400,'invalid key action'); }
    else if(e.type==='text') {if(typeof e.text!=='string'||e.text.length>2000)throw new HttpError(400,'text limited to 2000 characters');if(/[^\x09\x0a\x20-\x7e]/.test(e.text))throw new HttpError(422,'normalized text supports printable ASCII, LF and Tab only; Unicode requires a future text transport');}
    else if(e.type==='releaseAll') {}
    else if(e.type==='pointer') {pos(e);if(!['move','down','up'].includes(e.action)|| (e.action!=='move'&&!buttons[e.button]))throw new HttpError(400,'invalid pointer');}
    else if(e.type==='click') {pos(e);if(!buttons[e.button??'left']||![1,2].includes(e.count??1))throw new HttpError(400,'invalid click');}
    else if(e.type==='scroll') {pos(e);if(!Number.isFinite(e.deltaX)||!Number.isFinite(e.deltaY)||Math.abs(e.deltaX)>2000||Math.abs(e.deltaY)>2000)throw new HttpError(400,'invalid scroll');}
    else throw new HttpError(422,'unsupported event type');
  }
}
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
export async function executeEvents(driver, gate, body) {
  const lease=gate.validate(body), gen=gate.generation;
  if(lease.mode==='native')throw new HttpError(409,'native diagnostic lease uses its data channel; HTTP input disabled');
  validateEvents(body.events,driver.size);
  const check=()=>gate.validate(body,gen);
  let sent=0;
  for(const e of body.events) {
    check();
    if(e.type==='key')driver.key(sym(e.key),e.action==='down');
    if(e.type==='text')for(const ch of e.text){check();const k=ch==='\n'?sym('Enter'):ch==='\t'?sym('Tab'):sym(ch);driver.key(k,true);driver.key(k,false);await sleep(4);}
    if(e.type==='pointer'){driver.move(e.x,e.y);if(e.action!=='move')driver.button(buttons[e.button],e.action==='down');}
    if(e.type==='click'){driver.move(e.x,e.y);for(let n=0;n<(e.count??1);n++){check();driver.button(buttons[e.button??'left'],true);await sleep(12);check();driver.button(buttons[e.button??'left'],false);if(n===0&&e.count===2)await sleep(60);}}
    if(e.type==='scroll'){driver.move(e.x,e.y);const step=n=>n===0?0:-Math.sign(n)*Math.max(1,Math.round(Math.abs(n)/53));driver.send('control/scroll',{delta_x:step(e.deltaX),delta_y:step(e.deltaY),control_key:driver.keys.has(sym('Control'))});}
    if(e.type==='releaseAll')driver.releaseHeld();
    sent++;await sleep(4);
  }
  return {ok:true,sent,route:'adapter HTTP → Neko v3 control websocket → X11',generation:gen};
}
