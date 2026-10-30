"""Independent, read-only X11 query for disposable Sidekicks desktops."""
import ctypes as C
import json
import os

lib = C.CDLL('libX11.so.6')
lib.XOpenDisplay.argtypes = [C.c_char_p]
lib.XOpenDisplay.restype = C.c_void_p
lib.XDefaultRootWindow.argtypes = [C.c_void_p]
lib.XDefaultRootWindow.restype = C.c_ulong
lib.XQueryPointer.argtypes = [C.c_void_p, C.c_ulong, C.POINTER(C.c_ulong), C.POINTER(C.c_ulong), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_uint)]
lib.XQueryKeymap.argtypes = [C.c_void_p, C.c_char_p]
lib.XKeysymToKeycode.argtypes = [C.c_void_p, C.c_ulong]
lib.XKeysymToKeycode.restype = C.c_ubyte
lib.XCloseDisplay.argtypes = [C.c_void_p]
display = lib.XOpenDisplay(os.environ.get('DISPLAY', ':99').encode())
if not display:
    raise RuntimeError('Synthetic X11 display unavailable')
try:
    root, child = C.c_ulong(), C.c_ulong()
    rx, ry, wx, wy = C.c_int(), C.c_int(), C.c_int(), C.c_int()
    mask = C.c_uint()
    lib.XQueryPointer(display, lib.XDefaultRootWindow(display), C.byref(root), C.byref(child), C.byref(rx), C.byref(ry), C.byref(wx), C.byref(wy), C.byref(mask))
    bitmap = C.create_string_buffer(32)
    lib.XQueryKeymap(display, bitmap)
    pressed = []
    for name, keysym in [('Shift', 0xffe1), ('Control', 0xffe3), ('Alt', 0xffe9), ('Meta', 0xffeb), ('q', 0x71)]:
        keycode = lib.XKeysymToKeycode(display, keysym)
        if bitmap.raw[keycode // 8] & (1 << (keycode % 8)):
            pressed.append(name)
    print(json.dumps({'source': 'independent ctypes XQueryKeymap/XQueryPointer', 'pointer': {'x': rx.value, 'y': ry.value}, 'pressedKeys': pressed, 'pressedButtons': [b for b in range(1, 6) if mask.value & (1 << (7 + b))]}))
finally:
    lib.XCloseDisplay(display)
