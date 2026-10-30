import asyncio
import collections
import datetime as dt
import io
import json
import math
import os
from pathlib import Path
import platform
import secrets
import struct
import tempfile
import time
from urllib.parse import urlparse
from aiohttp import web, WSMsgType
from PIL import Image
from rfb_gate import ClientFrames, ProtocolError
from x11_pointer import pressed_buttons

ROOT = Path(__file__).parent
LOCK = asyncio.Lock()
OWNER = None
TRANSITION = None
LAST_TRANSITION = None
KEYS = set()
BUTTONS = set()
VIEWERS = {}
COUNTERS = collections.Counter()
FIXTURE_EVENTS = collections.deque(maxlen=40)
PUBLIC_VIEWER_PORT = int(os.environ.get('SIDEKICKS_PUBLIC_VIEWER_PORT','7901'))
PUBLIC_API_PORT = int(os.environ.get('SIDEKICKS_PUBLIC_API_PORT','7791'))
ALLOWED_PORTS = {6080,8080,7790,PUBLIC_API_PORT,PUBLIC_VIEWER_PORT}
DESKTOP_TOKEN = secrets.token_urlsafe(32)
GUEST_ORIGIN = 'http://127.0.0.1:8080'

def internal_guest(request, require_origin=False):
    origin = request.headers.get('Origin')
    return (request.remote == '127.0.0.1' and
            request.headers.get('Host') == '127.0.0.1:8080' and
            (origin == GUEST_ORIGIN if require_origin else origin in {None,GUEST_ORIGIN}))

async def desktop_home(request):
    if not internal_guest(request):
        raise web.HTTPForbidden(text='desktop Home is available only inside this guest')
    html = (ROOT/'desktop-assets'/'home.html').read_text()
    html = html.replace('__SIDEKICKS_DESKTOP_TOKEN__',DESKTOP_TOKEN)
    return web.Response(text=html,content_type='text/html',headers={
        'X-Frame-Options':'DENY',
        'Content-Security-Policy':"frame-ancestors 'none'; base-uri 'self'"})

async def desktop_launch(request):
    if not internal_guest(request,require_origin=True) or not secrets.compare_digest(request.headers.get('X-Sidekicks-Desktop','').encode(),DESKTOP_TOKEN.encode()):
        return web.json_response({'error':'internal desktop capability and Origin required'},status=403)
    body = await request.json()
    app = body.get('app') if isinstance(body,dict) else None
    if not isinstance(app,str) or app not in {'browser','terminal','files'}:
        return web.json_response({'error':'app must be browser, terminal or files'},status=400)
    async with LOCK:
        await expire()
        if not OWNER:
            return web.json_response({'error':'take control before launching an app'},status=409)
        result = await run('python3',str(ROOT/'desktop-app.py'),app,timeout=4)
    data = json.loads(result)
    if data.get('state') in {'launching','failed'}:
        return web.json_response({**data,'error':data.get('error','application is still starting; try its dock icon shortly')},status=503)
    return web.json_response(data)

async def desktop_state(request):
    path = Path('/tmp/sidekicks-desktop-ready.json')
    if not path.exists():
        return web.json_response({'ready':False,'theme':'sidekicks-sky','homeUrl':GUEST_ORIGIN+'/home','detail':'desktop starting'},status=503)
    data = json.loads(path.read_text())
    if data.get('stack') == 'xfce':
        # The startup marker establishes that the first browser was mapped. It
        # cannot establish that the desktop session is still alive after logout.
        # Browser closure is deliberately independent from desktop readiness.
        names = {'xfwm4','xfsettingsd','xfce4-panel','xfdesktop','xfce4-session'}
        current = {name:[] for name in sorted(names)}
        for entry in Path('/proc').iterdir():
            if not entry.name.isdecimal():
                continue
            try:
                status = (entry/'status').read_text().splitlines()
                fields = dict(line.split(':',1) for line in status if ':' in line)
                name = fields.get('Name','').strip()
                if (name in names and fields.get('State','').strip()[:1] not in {'Z','X'}
                        and int(fields['Uid'].split()[0]) == os.getuid()):
                    current[name].append(entry.name)
            except (OSError,ValueError,KeyError):
                continue  # A process may exit while /proc is being sampled.
        missing = [name for name,pids in current.items() if not pids]
        data = {**data,'processes':current,'checkedAt':time.time()}
        if missing:
            data.update(ready=False,detail='Xfce session components missing; restart the desktop to recover',missing=missing)
    return web.json_response(data,status=200 if data.get('ready') else 503)

def now_iso(t):
    return dt.datetime.fromtimestamp(t, dt.timezone.utc).isoformat()

def public_owner():
    return {'owner': OWNER['owner'] if OWNER else None, 'session': OWNER['session'] if OWNER else None,
            'expiresAt': now_iso(OWNER['expires']) if OWNER else None, 'agentPaused': not OWNER or OWNER['owner'] != 'agent',
            'state':'settling' if TRANSITION else ('ready' if OWNER else 'idle'), 'transition':TRANSITION, 'lastTransition':LAST_TRANSITION}

async def run(*args, timeout=4):
    proc = await asyncio.create_subprocess_exec(*args, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    try:
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout)
    except asyncio.TimeoutError:
        proc.kill()
        await proc.wait()
        raise web.HTTPServiceUnavailable(text='desktop command timed out')
    if proc.returncode:
        raise web.HTTPServiceUnavailable(text=f'desktop command failed: {stderr.decode(errors="replace")[:200]}')
    return stdout

async def release_all():
    # Track every allowed RFB and normalized pressed key/button, including non-modifiers.
    for key in list(KEYS):
        await run('xdotool', 'keyup', key)
    # A view-only RFB mask can be zero while XTest holds a real button. Query
    # X11 as well so transport bookkeeping cannot hide a held native button.
    for button in BUTTONS | pressed_buttons():
        await run('xdotool', 'mouseup', str(button))
    KEYS.clear()
    BUTTONS.clear()

async def clear_owner(reason):
    global OWNER, TRANSITION, LAST_TRANSITION
    prior = OWNER
    OWNER = None
    start = time.perf_counter()
    TRANSITION = {'reason':reason,'injectionCutoffAt':now_iso(time.time()),'injectionCommandCount':COUNTERS['injection_commands']}
    try:
        await release_all()
        # Bounded generic settling reduces downstream backlog; it is not an
        # application fence and never requires a synthetic fixture to be open.
        if prior:
            await asyncio.sleep(.25 if prior['owner']=='agent' else .05)
        LAST_TRANSITION = {**TRANSITION,'settlingMs':round((time.perf_counter()-start)*1000,3),
            'method':'paced dispatch + bounded time settle','applicationQueueFlushed':False,'readyAt':now_iso(time.time())}
    finally:
        TRANSITION = None
    COUNTERS['ownership_'+reason] += 1

async def expire():
    if OWNER and OWNER['expires'] <= time.time():
        await clear_owner('expired')

def valid(token, session):
    return OWNER and OWNER['expires'] > time.time() and isinstance(token,str) and token.isascii() and secrets.compare_digest(token, OWNER['token']) and session == OWNER['session']

@web.middleware
async def safety(request, handler):
    origin = request.headers.get('Origin')
    host = request.headers.get('Host','').split(':')[0]
    if host not in {'127.0.0.1','localhost','[::1]'}:
        raise web.HTTPForbidden(text='loopback host required')
    if origin:
        try:
            parsed = urlparse(origin)
            allowed = parsed.scheme == 'http' and parsed.hostname in {'localhost','127.0.0.1'} and parsed.port in ALLOWED_PORTS
        except ValueError:
            allowed = False
        if not allowed:
            raise web.HTTPForbidden(text='loopback demo Origin required')
    if request.method not in {'GET','OPTIONS'} and request.content_type != 'application/json':
        raise web.HTTPUnsupportedMediaType(text='application/json required')
    response = await handler(request)
    if origin:
        response.headers['Access-Control-Allow-Origin'] = origin
        response.headers['Vary'] = 'Origin'
        response.headers['Access-Control-Allow-Headers'] = 'Content-Type'
        response.headers['Access-Control-Allow-Methods'] = 'GET,POST,OPTIONS'
    response.headers['Cache-Control'] = 'no-store'
    response.headers['X-Content-Type-Options'] = 'nosniff'
    return response

async def health(request):
    try:
        await run('xdotool','getdisplaygeometry')
        reader,writer = await asyncio.wait_for(asyncio.open_connection('127.0.0.1',5900),1)
        banner = await asyncio.wait_for(reader.readexactly(12),1)
        writer.close()
        await writer.wait_closed()
    except Exception as e:
        return web.json_response({'ok':False,'id':'novnc','status':'not-ready','detail':str(e)}, status=503)
    return web.json_response({'ok':True,'id':'novnc','status':'ready','runtime':{'platform':'linux/'+('arm64' if platform.machine()=='aarch64' else 'amd64'),'emulated':False},'desktop':{'width':1280,'height':720},'rfb':banner.decode().strip(),'viewers':len(VIEWERS),'control':public_owner(),'counters':dict(COUNTERS)})

async def capabilities(request):
    names = ['keyboard','click','rightClick','doubleClick','drag','scroll','modifiers','zoomEmulated','screenshot','theme','humanTakeover']
    caps = {n:{'status':'implemented','detail':'See pocs/novnc/status.json for executed validation and evidence.'} for n in names}
    for name in ['pinchNative','swipeNative','audio']:
        caps[name] = {'status':'unsupported','detail':'RFB/X11 carries pointer/key events; no native multitouch or audio channel in this POC.'}
    status_path = ROOT / 'status.json'
    if status_path.exists():
        status = json.loads(status_path.read_text())
        for name, detail in status.get('testedCapabilities',{}).items():
            caps[name] = detail
    return web.json_response({'id':'novnc','viewerUrl':f'http://127.0.0.1:{PUBLIC_VIEWER_PORT}/','capabilities':caps,'presets':{'text':{'compression':2,'quality':9,'note':'Requests high JPEG quality; not guaranteed lossless.'},'motion':{'compression':1,'quality':4,'note':'Requests lower JPEG quality/compression CPU; no H264 claim.'}}})

async def control(request):
    global OWNER
    if request.method=='GET' and TRANSITION:
        return web.json_response(public_owner())
    async with LOCK:
        await expire()
        if request.method == 'GET':
            return web.json_response(public_owner())
        body = await request.json()
        action, session, kind = body.get('action'), body.get('session'), body.get('owner')
        if not isinstance(session,str) or not 1 <= len(session) <= 128:
            raise web.HTTPBadRequest(text='session must be 1..128 characters')
        if action == 'acquire':
            if kind not in {'human','agent'}:
                raise web.HTTPBadRequest(text='owner must be human or agent')
            lease = body.get('leaseMs',30000)
            if not isinstance(lease,(int,float)) or not math.isfinite(lease) or not 1000 <= lease <= 60000:
                raise web.HTTPBadRequest(text='leaseMs must be 1000..60000')
            if OWNER:
                if valid(body.get('token'),session):
                    OWNER['expires'] = time.time()+lease/1000
                    return web.json_response({'ok':True,'token':OWNER['token'],**public_owner()})
                if kind != 'human' or OWNER['owner'] == 'human':
                    raise web.HTTPConflict(text='another owner holds the desktop')
                await clear_owner('human_preemption')
            OWNER = {'owner':kind,'session':session,'token':secrets.token_urlsafe(24),'expires':time.time()+lease/1000}
            return web.json_response({'ok':True,'token':OWNER['token'],**public_owner()})
        if action in {'release','stop'}:
            # An unauthenticated emergency stop may only relinquish control, never acquire it.
            if action == 'release' and not valid(body.get('token'),session):
                raise web.HTTPForbidden(text='lease token/session required')
            await clear_owner(action)
            return web.json_response({'ok':True,**public_owner()})
        raise web.HTTPBadRequest(text='unknown control action')

KEYMAP = {'Enter':'Return','Control':'Control_L','Shift':'Shift_L','Alt':'Alt_L','Meta':'Super_L','ArrowUp':'Up','ArrowDown':'Down','ArrowLeft':'Left','ArrowRight':'Right','Escape':'Escape','Backspace':'BackSpace','Tab':'Tab','Delete':'Delete','Home':'Home','End':'End','PageUp':'Prior','PageDown':'Next',' ':'space','+':'plus','-':'minus'}

def keyname(key):
    if not isinstance(key,str): raise web.HTTPBadRequest(text='key must be a string')
    if key in KEYMAP: return KEYMAP[key]
    if isinstance(key,str) and len(key)==1 and key.isascii() and key.isalnum(): return key
    if isinstance(key,str) and key in {'F'+str(i) for i in range(1,13)}: return key
    raise web.HTTPUnprocessableEntity(text='unsupported key')

def position(event):
    x,y = event.get('x'),event.get('y')
    if not all(isinstance(v,(int,float)) and math.isfinite(v) for v in [x,y]) or not (0<=x<1280 and 0<=y<720):
        raise web.HTTPBadRequest(text='desktop coordinates must be within 1280x720')
    return str(round(x)), str(round(y))

def normalize(event):
    if not isinstance(event,dict): raise web.HTTPBadRequest(text='event must be an object')
    typ = event.get('type')
    if typ == 'releaseAll': return [('release',)]
    if typ == 'text':
        text = event.get('text')
        if not isinstance(text,str) or len(text)>4096 or '\x00' in text: raise web.HTTPBadRequest(text='text limit 4096, no NUL')
        # Short chunks allow a human to preempt long text batches promptly.
        return [('text',text[i:i+4]) for i in range(0,len(text),4)]
    if typ == 'key':
        key = keyname(event.get('key'))
        if event.get('action') not in {'down','up'}: raise web.HTTPBadRequest(text='key action down|up required')
        return [('key',event['action'],key)]
    if typ in {'pointer','click','scroll'}:
        x,y = position(event)
        # --sync can wait forever when the pointer already occupies the target.
        commands = [('command','xdotool','mousemove',x,y)]
        button = {'left':1,'middle':2,'right':3}.get(event.get('button','left'))
        if not button: raise web.HTTPBadRequest(text='button left|middle|right required')
        if typ == 'pointer':
            action = event.get('action')
            if action not in {'move','down','up'}: raise web.HTTPBadRequest(text='pointer action move|down|up required')
            if action != 'move': commands.append(('button',action,button))
        if typ == 'click':
            count = event.get('count',1)
            if count not in (1,2): raise web.HTTPBadRequest(text='click count 1|2 required')
            commands.append(('command','xdotool','click','--repeat',str(count),'--delay','80',str(button)))
        if typ == 'scroll':
            for axis,positive,negative in [('deltaY',5,4),('deltaX',7,6)]:
                delta = event.get(axis,0)
                if not isinstance(delta,(int,float)) or not math.isfinite(delta) or abs(delta)>2400: raise web.HTTPBadRequest(text='wheel delta limit 2400')
                if delta:
                    count = min(20,max(1,round(abs(delta)/120)))
                    commands.append(('command','xdotool','click','--repeat',str(count),'--delay','10',str(positive if delta>0 else negative)))
        return commands
    raise web.HTTPUnprocessableEntity(text='unsupported event type')

async def input_events(request):
    body = await request.json()
    if not isinstance(body,dict): raise web.HTTPBadRequest(text='JSON object required')
    try:
        events = body.get('events')
        if not isinstance(events,list) or not 1<=len(events)<=100:
            raise web.HTTPBadRequest(text='events must contain 1..100 entries')
        # Validate the entire batch before executing any command.
        commands = [command for event in events for command in normalize(event)]
    except (web.HTTPException,TypeError,ValueError) as error:
        async with LOCK:
            await expire()
            # Only the authenticated current owner's rejected batch may stop it.
            if valid(body.get('token'),body.get('session')):
                await clear_owner('invalid_input')
        if isinstance(error,web.HTTPException): raise
        raise web.HTTPBadRequest(text='malformed event fields') from error
    async with LOCK:
        await expire()
        if not valid(body.get('token'),body.get('session')):
            COUNTERS['rejected_agent_input'] += 1
            raise web.HTTPConflict(text='active token/session required; agent paused')
    start = time.perf_counter()
    for command in commands:
        async with LOCK:
            await expire()
            if not valid(body.get('token'),body.get('session')):
                raise web.HTTPConflict(text='ownership changed; remaining actions cancelled')
            try:
                if command[0]=='release': await release_all()
                elif command[0]=='text':
                    await run('xdotool','type','--clearmodifiers','--delay','20','--',command[1])
                elif command[0]=='key':
                    _,action,key = command
                    await run('xdotool','keydown' if action=='down' else 'keyup',key)
                    (KEYS.add if action=='down' else KEYS.discard)(key)
                elif command[0]=='button':
                    _,action,button = command
                    await run('xdotool','mousedown' if action=='down' else 'mouseup',str(button))
                    (BUTTONS.add if action=='down' else BUTTONS.discard)(button)
                else: await run(*command[1:])
                COUNTERS['injection_commands'] += 1
                COUNTERS['last_injection_unix_ms'] = round(time.time()*1000)
            except web.HTTPException:
                await clear_owner('command_failure')
                raise
        await asyncio.sleep(0) # Ownership transfer can cancel the next command.
    COUNTERS['normalized_events'] += len(events)
    return web.json_response({'ok':True,'events':len(events),'executionMs':round((time.perf_counter()-start)*1000,3)})

async def screenshot(request):
    try:
        w,h = int(request.query.get('maxWidth',1280)),int(request.query.get('maxHeight',720))
    except ValueError:
        raise web.HTTPBadRequest(text='integer screenshot bounds required')
    if not 1<=w<=1920 or not 1<=h<=1080: raise web.HTTPBadRequest(text='bounds exceed 1920x1080')
    start = time.perf_counter()
    fd,path = tempfile.mkstemp(suffix='.png',prefix='sidekicks-')
    os.close(fd)
    try:
        await run('scrot','--overwrite',path)
        capture_ms = (time.perf_counter()-start)*1000
        with Image.open(path) as im:
            im.thumbnail((w,h))
            out = io.BytesIO()
            im.save(out,format='PNG')
            width,height = im.size
        data = out.getvalue()
    finally:
        os.unlink(path)
    if len(data)>4*1024*1024: raise web.HTTPServiceUnavailable(text='capture exceeds payload budget')
    total_ms = (time.perf_counter()-start)*1000
    return web.Response(body=data,content_type='image/png',headers={'X-Source-Width':'1280','X-Source-Height':'720','X-Image-Width':str(width),'X-Image-Height':str(height),'X-Capture-Ms':str(round(capture_ms,3)),'X-Capture-Encode-Ms':str(round(total_ms,3))})

def track_rfb(packet):
    typ = packet[0]
    if typ == 4:
        key = '0x'+format(struct.unpack_from('!I',packet,4)[0],'x')
        (KEYS.add if packet[1] else KEYS.discard)(key)
    elif typ == 255:
        key = '0x'+format(struct.unpack_from('!I',packet,4)[0],'x')
        (KEYS.add if struct.unpack_from('!H',packet,2)[0] else KEYS.discard)(key)
    elif typ == 5:
        for button in range(1,4):
            (BUTTONS.add if packet[1] & (1<<(button-1)) else BUTTONS.discard)(button)

async def websocket(request):
    ws = web.WebSocketResponse(protocols=('binary',),max_msg_size=1048576,heartbeat=15)
    await ws.prepare(request)
    session = request.query.get('session','')
    viewer = {'token':None,'session':session,'connectedAt':time.time(),'inputAccepted':0,'inputDropped':0,'bytesOut':0}
    viewer_id = secrets.token_hex(8)
    VIEWERS[viewer_id] = viewer
    reader,writer = await asyncio.open_connection('127.0.0.1',5900)
    parser = ClientFrames()
    async def outbound():
        while True:
            data = await reader.read(131072)
            if not data: break
            viewer['bytesOut'] += len(data)
            await ws.send_bytes(data)
        await ws.close()
    pump = asyncio.create_task(outbound())
    try:
        async for message in ws:
            if message.type == WSMsgType.TEXT:
                # Bind lease over the socket, avoiding tokens in URL/access logs.
                body = json.loads(message.data)
                if body.get('type')=='lease' and body.get('session')==session:
                    viewer['token'] = body.get('token')
                continue
            if message.type != WSMsgType.BINARY: continue
            for is_input,packet in parser.feed(message.data):
                async with LOCK:
                    await expire()
                    allowed = not is_input or (valid(viewer['token'],session) and OWNER['owner']=='human')
                    if allowed:
                        writer.write(packet)
                        await writer.drain()
                        if is_input:
                            track_rfb(packet)
                            viewer['inputAccepted'] += 1
                            COUNTERS['viewer_input_accepted'] += 1
                    else:
                        viewer['inputDropped'] += 1
                        COUNTERS['viewer_input_dropped'] += 1
    except (ProtocolError,ValueError,ConnectionError) as e:
        COUNTERS['viewer_protocol_errors'] += 1
        await ws.close(code=1008,message=str(e).encode()[:100])
    finally:
        async with LOCK:
            if valid(viewer['token'],session) and OWNER['owner']=='human':
                await clear_owner('viewer_disconnect')
        pump.cancel()
        writer.close()
        await writer.wait_closed()
        VIEWERS.pop(viewer_id,None)
    return ws

async def metrics(request):
    return web.json_response({'counters':dict(COUNTERS),'viewers':[{k:v for k,v in viewer.items() if k!='token'} for viewer in VIEWERS.values()],'control':public_owner()})

async def fixture_event(request):
    if request.method=='POST':
        body = await request.json()
        FIXTURE_EVENTS.append({'serverTime':time.time(),**body})
        return web.json_response({'ok':True})
    if request.query.get('latest')=='1':
        return web.json_response(list(FIXTURE_EVENTS)[-1:])
    return web.json_response(list(FIXTURE_EVENTS))

async def file(request):
    return web.FileResponse(ROOT / 'web' / request.match_info.get('name','index.html'))

async def fixture(request):
    text = (ROOT / 'web' / 'shared-fixture.html').read_text()
    hook = '''<script>
    (()=>{const badge=document.createElement('div');badge.id='frame-probe';Object.assign(badge.style,{position:'fixed',right:'18px',top:'18px',width:'180px',height:'44px',background:'rgb(255,0,255)',color:'#fff',zIndex:999,font:'bold 13px monospace',textAlign:'center',padding:'6px'});let n=0;badge.textContent='SK-'+Date.now().toString(36)+' / 0';document.body.append(badge);const marker=badge.textContent.split(' / ')[0];document.getElementById('click-target').addEventListener('click',()=>{n++;badge.style.background=n%2?'rgb(0,255,0)':'rgb(255,0,255)';badge.style.color=n%2?'black':'white';badge.textContent=marker+' / '+n});document.addEventListener('pointerdown',e=>{window.__lastPointer={x:e.clientX,y:e.clientY,buttons:e.buttons,time:performance.now()}},true);let pending=false;function capture(){if(pending)return;pending=true;setTimeout(()=>{pending=false;fetch('/fixture-events',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({type:'snapshot',marker,probe:n,snapshot:window.__sidekicksFixture.snapshot(),lastPointer:window.__lastPointer||null,geometry:{offset:{x:screenX+(outerWidth-innerWidth)/2,y:screenY+outerHeight-innerHeight},rects:Object.fromEntries(['typing','click-target','drag-token','drop-zone','scroll-box','gesture-pad','frame-probe'].map(id=>{const r=document.getElementById(id).getBoundingClientRect();return[id,{x:r.x,y:r.y,width:r.width,height:r.height}]}))}})}).catch(()=>{})},10)}window.addEventListener('resize',capture);setTimeout(capture,1000);for(const e of ['click','dblclick','contextmenu','keyup','input','pointerup','scroll'])document.addEventListener(e,capture,true);capture();})();
    </script>'''
    return web.Response(text=text.replace('</body>',hook+'</body>'),content_type='text/html')

async def options(request):
    return web.Response(status=204)

async def expiry_task(app):
    async def loop():
        while True:
            await asyncio.sleep(.2)
            async with LOCK: await expire()
    task = asyncio.create_task(loop())
    yield
    task.cancel()
    await asyncio.gather(task,return_exceptions=True)

async def main():
    app = web.Application(middlewares=[safety],client_max_size=1048576)
    app.add_routes([web.get('/health',health),web.get('/capabilities',capabilities),web.get('/control',control),web.post('/control',control),web.post('/input',input_events),web.get('/screenshot',screenshot),web.get('/metrics',metrics),web.get('/websockify',websocket),web.get('/home',desktop_home),web.post('/desktop/launch',desktop_launch),web.get('/desktop/state',desktop_state),web.get('/fixture',fixture),web.get('/fixture/',fixture),web.get('/fixture-events',fixture_event),web.post('/fixture-events',fixture_event),web.get('/',file),web.options('/{tail:.*}',options)])
    app.router.add_static('/novnc/', '/opt/novnc',show_index=False)
    app.router.add_static('/web/', ROOT/'web',show_index=False)
    app.router.add_static('/desktop-assets/', ROOT/'desktop-assets',show_index=False)
    app.cleanup_ctx.append(expiry_task)
    runner = web.AppRunner(app)
    await runner.setup()
    await web.TCPSite(runner,'0.0.0.0',6080).start()
    await web.TCPSite(runner,'0.0.0.0',8080).start()
    await asyncio.Event().wait()

if __name__=='__main__': asyncio.run(main())
