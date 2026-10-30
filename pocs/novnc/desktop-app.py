#!/usr/bin/env python3
"""Task-local, allowlisted launch-or-restore helpers for actual X11 windows."""
import fcntl
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlsplit

APP = Path(__file__).parent
PROFILE = Path('/home/desktop/.config/sidekicks/chromium')
HOME = 'http://127.0.0.1:8080/home'
CLASSES = {'browser':'sidekicksbrowser', 'terminal':'sidekicksterminal', 'files':'thunar'}

def output(*args):
    return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,check=False).stdout

def find_window(kind):
    clients = re.findall(r'0x[0-9a-fA-F]+',output('xprop','-root','_NET_CLIENT_LIST'))
    for wid in reversed(clients):
        props = output('xprop','-id',wid,'WM_CLASS','_NET_WM_WINDOW_TYPE','WM_WINDOW_ROLE')
        normal = '_NET_WM_WINDOW_TYPE_NORMAL' in props or '_NET_WM_WINDOW_TYPE(ATOM)' not in props
        classes = re.search(r'^WM_CLASS\(STRING\) = (.+)$',props,re.M)
        names = re.findall(r'"([^"\n]*)"',classes[1]) if classes else []
        matches=CLASSES[kind] in {name.casefold() for name in names}
        if kind=='terminal' and os.environ.get('SIDEKICKS_DESKTOP_STACK')=='xfce':
            matches=matches or ('xfce4-terminal' in {name.casefold() for name in names} and 'WM_WINDOW_ROLE(STRING) = "SidekicksTerminal"' in props)
        if not matches or not normal: continue
        geom = output('xdotool','getwindowgeometry','--shell',wid)
        width = re.search(r'^WIDTH=(\d+)',geom,re.M)
        height = re.search(r'^HEIGHT=(\d+)',geom,re.M)
        if width and height and int(width[1])>100 and int(height[1])>80:
            return wid

def browser_urls(values):
    """Native desktop file/URL associations only; never evaluated by a shell."""
    if len(values)>16: raise ValueError('too many browser targets')
    urls=[]
    for value in values:
        if not isinstance(value,str) or len(value)>8192 or any(ord(c)<32 for c in value):
            raise ValueError('invalid browser target')
        parsed=urlsplit(value)
        if parsed.scheme in {'http','https'} and parsed.hostname:
            urls.append(value)
        elif parsed.scheme=='file' and parsed.netloc in {'','localhost'} and parsed.path.startswith('/'):
            urls.append(value)
        elif not parsed.scheme and Path(value).is_absolute():
            urls.append(Path(value).as_uri())
        else:
            raise ValueError('browser target must be an http, https or local file URL')
    return urls

def prepare_browser_profile():
    # Never edit Preferences behind a running Chromium process. A dedicated
    # profile/class also leaves comparison browsers and their tabs alone.
    processes=output('ps','-eo','args').splitlines()
    if any('--user-data-dir='+str(PROFILE) in p and ('/chromium ' in p or p.startswith('chromium ')) for p in processes):
        return False
    default=PROFILE/'Default'
    default.mkdir(parents=True,exist_ok=True)
    prefs=default/'Preferences'
    data=json.loads(prefs.read_text()) if prefs.exists() else {}
    if not isinstance(data,dict): raise ValueError('managed browser Preferences is not an object')
    browser=data.setdefault('browser',{})
    session=data.setdefault('session',{})
    if not isinstance(browser,dict) or not isinstance(session,dict):
        raise ValueError('managed browser preference sections are invalid')
    browser.update(custom_chrome_frame=True,show_home_button=True)
    data.update(homepage=HOME,homepage_is_newtabpage=False)
    session.update(restore_on_startup=4,startup_urls=[HOME])
    temporary=prefs.with_name('Preferences.sidekicks-tmp')
    temporary.write_text(json.dumps(data))
    temporary.replace(prefs)
    return True

def browser_arguments(urls,existing=False):
    args=['chromium','--no-sandbox','--test-type','--disable-dev-shm-usage','--disable-gpu',
        '--disable-background-networking','--disable-background-timer-throttling',
        '--disable-renderer-backgrounding','--disable-backgrounding-occluded-windows',
        '--no-first-run','--no-default-browser-check','--password-store=basic',
        '--user-data-dir='+str(PROFILE),'--class=SidekicksBrowser',
        '--window-size=1120,548','--window-position=80,56',
        '--disable-extensions-except=/app/newtab-extension',
        '--load-extension=/app/newtab-extension']
    if not existing: args.append('--new-window')
    return args+(urls or [HOME])

def launch(kind,urls=()):
    if kind not in CLASSES or (urls and kind!='browser'):
        raise ValueError('only Browser accepts desktop URL targets')
    urls=browser_urls(urls)
    with open('/tmp/sidekicks-launch-'+kind+'.lock','a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        pending = Path('/tmp/sidekicks-launch-'+kind+'.json')
        existing = find_window(kind)
        if existing:
            pending.unlink(missing_ok=True)
            if urls:
                subprocess.run(browser_arguments(urls,existing=True),check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=3)
            subprocess.run(['wmctrl','-ia',existing],check=True)
            return {'ok':True,'app':kind,'state':'restored','window':existing}
        if pending.exists() and time.time()-pending.stat().st_mtime<15:
            return {'ok':True,'app':kind,'state':'launching'}
        if kind=='browser':
            prepare_browser_profile()
            args=browser_arguments(urls)
        elif kind=='terminal':
            if os.environ.get('SIDEKICKS_DESKTOP_STACK')=='xfce':
                # Bookworm's1.0.4 lacks the newer --class option. Identify only
                # our managed normal client by its supported unique WM role.
                args=['xfce4-terminal','--disable-server','--role=SidekicksTerminal',
                    '--geometry=88x24','--title=Sidekicks - disposable terminal']
            else:
                args = ['xterm','-u8','-class','SidekicksTerminal','-name','sidekicks-terminal',
                    '-geometry','88x24','-title','Sidekicks - disposable terminal']
        else:
            args = ['thunar','/home/desktop/Sidekicks']
        with open('/tmp/sidekicks-'+kind+'.log','ab') as log:
            process = subprocess.Popen(args,stdout=log,stderr=log,start_new_session=True)
        pending.write_text(json.dumps({'pid':process.pid,'startedAt':time.time()}))
        deadline = time.monotonic()+3
        while time.monotonic()<deadline:
            wid = find_window(kind)
            if wid:
                pending.unlink(missing_ok=True)
                return {'ok':True,'app':kind,'state':'opened','window':wid}
            time.sleep(.1)
        if process.poll() not in (None,0):
            pending.unlink(missing_ok=True)
            return {'ok':False,'app':kind,'state':'failed','error':'application did not open'}
        return {'ok':True,'app':kind,'state':'launching'}

if __name__=='__main__':
    if len(sys.argv)<2 or sys.argv[1] not in CLASSES:
        raise SystemExit('usage: desktop-app.py browser|terminal|files [Browser URL targets]')
    try:
        print(json.dumps(launch(sys.argv[1],sys.argv[2:])))
    except ValueError as error:
        raise SystemExit(str(error))
