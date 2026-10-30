"""Read actual X11 held modifier/button state for synthetic QA; emits no input."""
import ctypes as C
import json
import os
x=C.CDLL('libX11.so.6')
x.XOpenDisplay.argtypes=[C.c_char_p];x.XOpenDisplay.restype=C.c_void_p
x.XDefaultRootWindow.argtypes=[C.c_void_p];x.XDefaultRootWindow.restype=C.c_ulong
x.XQueryPointer.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_ulong),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_uint)]
x.XQueryKeymap.argtypes=[C.c_void_p,C.c_char_p]
x.XKeysymToKeycode.argtypes=[C.c_void_p,C.c_ulong];x.XKeysymToKeycode.restype=C.c_ubyte
x.XCloseDisplay.argtypes=[C.c_void_p]
d=x.XOpenDisplay(os.environ.get('DISPLAY',':99').encode())
if not d: raise RuntimeError('X11 display unavailable')
root=C.c_ulong();child=C.c_ulong();rx=C.c_int();ry=C.c_int();wx=C.c_int();wy=C.c_int();mask=C.c_uint()
x.XQueryPointer(d,x.XDefaultRootWindow(d),C.byref(root),C.byref(child),C.byref(rx),C.byref(ry),C.byref(wx),C.byref(wy),C.byref(mask))
keys=C.create_string_buffer(32);x.XQueryKeymap(d,keys)
held={}
for name,keysym in [('Shift_L',0xffe1),('Control_L',0xffe3),('Alt_L',0xffe9),('Super_L',0xffeb),('a',0x61)]:
    code=x.XKeysymToKeycode(d,keysym)
    held[name]=bool(keys.raw[code//8] & (1<<(code%8)))
print(json.dumps({'source':'XQueryPointer + XQueryKeymap','x':rx.value,'y':ry.value,'buttons':{str(b):bool(mask.value&(1<<(7+b))) for b in range(1,6)},'keys':held,'mask':mask.value}))
x.XCloseDisplay(d)
