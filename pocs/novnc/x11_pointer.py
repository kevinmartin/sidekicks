"""Read actual guest button state when releasing mixed RFB/XTest input."""
import ctypes as C

X = C.CDLL('libX11.so.6')
X.XOpenDisplay.argtypes = [C.c_char_p]
X.XOpenDisplay.restype = C.c_void_p
X.XDefaultRootWindow.argtypes = [C.c_void_p]
X.XDefaultRootWindow.restype = C.c_ulong
X.XQueryPointer.argtypes = [C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_ulong),
    C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_uint)]
X.XCloseDisplay.argtypes = [C.c_void_p]

def pressed_buttons():
    display = X.XOpenDisplay(None)
    if not display:
        return set()
    try:
        root,child = C.c_ulong(),C.c_ulong()
        x,y,wx,wy,mask = C.c_int(),C.c_int(),C.c_int(),C.c_int(),C.c_uint()
        X.XQueryPointer(display,X.XDefaultRootWindow(display),C.byref(root),C.byref(child),
            C.byref(x),C.byref(y),C.byref(wx),C.byref(wy),C.byref(mask))
        return {button for button in (1,2,3) if mask.value & (1<<(7+button))}
    finally:
        X.XCloseDisplay(display)
