// LAN-only enhancement. Existing loopback viewers and frozen images are unchanged.
if(!crypto.randomUUID)Object.defineProperty(crypto,'randomUUID',{value:()=>{
 const b=crypto.getRandomValues(new Uint8Array(16));b[6]=(b[6]&15)|64;b[8]=(b[8]&63)|128;
 const h=[...b].map(x=>x.toString(16).padStart(2,'0')).join('');return`${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`;
}});
document.addEventListener('DOMContentLoaded',()=>{
 const style=document.createElement('style');style.textContent=`header{flex-wrap:wrap!important}body{max-width:100%;overflow-x:hidden}#metrics{overflow-wrap:anywhere}#agent{display:none!important}.lan-keys{padding:6px 10px;background:#e9f6fc;color:#21465d;display:flex;gap:6px;flex-wrap:wrap;flex-shrink:0}.lan-keys[hidden]{display:none}.lan-keys button{min-height:36px}.lan-keys textarea{font:16px system-ui;flex:1;min-width:150px;height:42px;resize:none;background:white;color:#123;border:1px solid #9bc}.lan-note{font:12px system-ui;width:100%}@media(max-width:600px){header{gap:6px!important;padding:8px!important}header h1{font-size:17px!important}main{padding:8px!important}.bar{gap:5px!important}.bar button,.bar a,header button{font-size:12px!important;padding:7px!important}}`;
 document.head.append(style);
 const button=document.createElement('button');button.id='lan-keyboard-toggle';button.textContent='Keyboard';button.type='button';
 (document.querySelector('.control-row')||document.querySelector('header')).append(button);
 const panel=document.createElement('div');panel.className='lan-keys';panel.hidden=true;panel.innerHTML='<textarea id="lan-typing" rows="1" aria-label="Remote keyboard text" placeholder="Type into the focused remote app" autocapitalize="off" autocomplete="off" autocorrect="off" spellcheck="false"></textarea><span class="lan-note">Take human control, tap a remote text field, then Keyboard. Printable ASCII supported; physical iPhone behavior is not yet verified.</span>';
 (document.querySelector('#state')||document.querySelector('header')).after(panel);
 const typing=panel.querySelector('textarea'),note=panel.querySelector('.lan-note');let serial=Promise.resolve();
 const view=()=>window.sidekicks||window.__sidekicksNeko;
 const error=e=>{note.textContent=e.message;};
 function send(events){const current=view(),token=current?.lease?.token;if(!token)return error(Error('Take human control first'));
  serial=serial.catch(()=>{}).then(async()=>{
   const c=view();if(!c?.lease||c.lease.token!==token)throw Error('Control changed; pending keyboard input cancelled');
   if(c.native){
    if(!c.client.controlling)throw Error('Native control is not ready');
    const keys={Control:0xffe3,Alt:0xffe9,Shift:0xffe1,Enter:0xff0d,Backspace:0xff08,Tab:0xff09,Escape:0xff1b,ArrowLeft:0xff51,ArrowRight:0xff53};
    for(const e of events)c.client.input.key(keys[e.key]??e.key.charCodeAt(0),e.action==='down');
   }else await c.api('/input',{session:c.session,token,events});
  }).catch(error);
 }
 const chord=keys=>send(keys.map(key=>({type:'key',key,action:'down'})).concat([...keys].reverse().map(key=>({type:'key',key,action:'up'}))));
 for(const [label,keys] of [['Enter',['Enter']],['⌫',['Backspace']],['Tab',['Tab']],['Esc',['Escape']],['Ctrl+A',['Control','a']],['←',['ArrowLeft']],['→',['ArrowRight']]]){
  const b=document.createElement('button');b.type='button';b.textContent=label;b.onpointerdown=e=>e.preventDefault();b.onclick=()=>chord(keys);panel.insertBefore(b,note);
 }
 button.onclick=()=>{
  if(!view()?.lease)return error(Error('Take human control first'));
  panel.hidden=!panel.hidden;
  if(!panel.hidden){if(view().native){typing.hidden=true;document.querySelector('#media textarea')?.focus();note.textContent='Native SDK keyboard. Extra keys above remain available; native multi-touch is disabled in this POC.';}else{typing.hidden=false;typing.focus();}}
 };
 function text(){if(!typing.value)return;const value=typing.value;typing.value='';if(/[^\x09\x0a\x20-\x7e]/.test(value))return error(Error('This demo text route supports printable ASCII, Tab and Enter.'));
  send([{type:'text',text:value}]);
 }
 typing.addEventListener('input',e=>{if(!e.isComposing)text();});typing.addEventListener('compositionend',text);
 typing.addEventListener('beforeinput',e=>{if(e.inputType==='deleteContentBackward'){e.preventDefault();chord(['Backspace']);}if(e.inputType==='insertLineBreak'){e.preventDefault();chord(['Enter']);}});
 typing.addEventListener('keydown',e=>{if(['Enter','Backspace','Tab','Escape'].includes(e.key)){e.preventDefault();chord([e.key]);}e.stopPropagation();});
 window.__sidekicksMobile={send,chord,get panel(){return panel;}};
});
