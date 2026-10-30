#!/usr/bin/env python3
"""Read-only observer for an explicitly handed-over disposable Linux desktop.

No X11 event injection, window commands, shell execution or filesystem writes.
Run via exact test-container stdin or private guest RPC, never against host UI.
"""
import ctypes as C
import datetime
import hashlib
import json
import os
import sys


def observe():
    if sys.platform != 'linux':
        raise RuntimeError('Guest-only observer: no host display access is allowed')
    x = C.CDLL('libX11.so.6')
    Display, Window, Atom = C.c_void_p, C.c_ulong, C.c_ulong
    class Attributes(C.Structure):
        _fields_ = [('x', C.c_int), ('y', C.c_int), ('width', C.c_int), ('height', C.c_int),
                    ('border_width', C.c_int), ('depth', C.c_int), ('visual', C.c_void_p),
                    ('root', Window), ('window_class', C.c_int), ('bit_gravity', C.c_int),
                    ('win_gravity', C.c_int), ('backing_store', C.c_int),
                    ('backing_planes', C.c_ulong), ('backing_pixel', C.c_ulong),
                    ('save_under', C.c_int), ('colormap', C.c_ulong), ('map_installed', C.c_int),
                    ('map_state', C.c_int), ('all_event_masks', C.c_long),
                    ('your_event_mask', C.c_long), ('do_not_propagate_mask', C.c_long),
                    ('override_redirect', C.c_int), ('screen', C.c_void_p)]
    x.XOpenDisplay.argtypes, x.XOpenDisplay.restype = [C.c_char_p], Display
    x.XDefaultRootWindow.argtypes, x.XDefaultRootWindow.restype = [Display], Window
    x.XInternAtom.argtypes, x.XInternAtom.restype = [Display, C.c_char_p, C.c_int], Atom
    x.XGetAtomName.argtypes, x.XGetAtomName.restype = [Display, Atom], C.c_void_p
    x.XFree.argtypes = [C.c_void_p]
    x.XGetWindowProperty.argtypes = [Display, Window, Atom, C.c_long, C.c_long, C.c_int, Atom,
        C.POINTER(Atom), C.POINTER(C.c_int), C.POINTER(C.c_ulong), C.POINTER(C.c_ulong),
        C.POINTER(C.POINTER(C.c_ubyte))]
    x.XGetWindowProperty.restype = C.c_int
    x.XGetWindowAttributes.argtypes = [Display, Window, C.POINTER(Attributes)]
    x.XTranslateCoordinates.argtypes = [Display, Window, Window, C.c_int, C.c_int,
        C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(Window)]
    x.XGetInputFocus.argtypes = [Display, C.POINTER(Window), C.POINTER(C.c_int)]
    x.XQueryTree.argtypes = [Display, Window, C.POINTER(Window), C.POINTER(Window),
                            C.POINTER(C.POINTER(Window)), C.POINTER(C.c_uint)]
    x.XQueryPointer.argtypes = [Display, Window, C.POINTER(Window), C.POINTER(Window),
        C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_int), C.POINTER(C.c_uint)]
    x.XQueryKeymap.argtypes = [Display, C.c_char_p]
    x.XCloseDisplay.argtypes = [Display]
    display = x.XOpenDisplay(os.environ.get('DISPLAY', ':99').encode())
    if not display:
        raise RuntimeError('Disposable guest X11 display unavailable')
    errors = []
    class XError(C.Structure):
        _fields_ = [('type', C.c_int), ('display', Display), ('resourceid', C.c_ulong),
                    ('serial', C.c_ulong), ('error_code', C.c_ubyte),
                    ('request_code', C.c_ubyte), ('minor_code', C.c_ubyte)]
    Handler = C.CFUNCTYPE(C.c_int, Display, C.POINTER(XError))
    @Handler
    def on_error(_, event):
        e = event.contents
        errors.append({'code': e.error_code, 'request': e.request_code, 'resource': e.resourceid})
        return 0
    x.XSetErrorHandler.argtypes, x.XSetErrorHandler.restype = [Handler], C.c_void_p
    x.XSetErrorHandler(on_error)
    atoms = {}
    def atom(name):
        if name not in atoms:
            atoms[name] = x.XInternAtom(display, name.encode(), 1)
        return atoms[name]
    def atom_name(value):
        if not value:
            return None
        ptr = x.XGetAtomName(display, value)
        if not ptr:
            return None
        try:
            return C.string_at(ptr).decode('utf8', 'replace')
        finally:
            x.XFree(ptr)
    def prop(window, name, text=False):
        a = atom(name)
        if not a:
            return None if text else []
        actual, fmt, n, left = Atom(), C.c_int(), C.c_ulong(), C.c_ulong()
        pointer = C.POINTER(C.c_ubyte)()
        status = x.XGetWindowProperty(display, window, a, 0, 4096, 0, 0,
            C.byref(actual), C.byref(fmt), C.byref(n), C.byref(left), C.byref(pointer))
        if status or not pointer:
            return None if text else []
        try:
            if fmt.value == 8:
                raw = C.string_at(pointer, n.value)
                return raw.decode('utf8', 'replace').rstrip('\0') if text else list(raw)
            if fmt.value == 32 and n.value <= 4096:
                values = C.cast(pointer, C.POINTER(C.c_ulong))
                return [int(values[i]) for i in range(n.value)]
            return None if text else []
        finally:
            x.XFree(pointer)
    def geometry(window, root):
        attrs = Attributes()
        if not x.XGetWindowAttributes(display, window, C.byref(attrs)):
            return None
        rx, ry, child = C.c_int(), C.c_int(), Window()
        translated = x.XTranslateCoordinates(display, window, root, 0, 0, C.byref(rx), C.byref(ry), C.byref(child))
        return {'x': rx.value if translated else attrs.x, 'y': ry.value if translated else attrs.y,
                'width': attrs.width, 'height': attrs.height, 'borderWidth': attrs.border_width,
                'mapState': {0:'unmapped', 1:'unviewable', 2:'viewable'}.get(attrs.map_state, attrs.map_state),
                'overrideRedirect': bool(attrs.override_redirect)}
    def tree(window):
        root, parent, children, count = Window(), Window(), C.POINTER(Window)(), C.c_uint()
        if not x.XQueryTree(display, window, C.byref(root), C.byref(parent), C.byref(children), C.byref(count)):
            return 0, []
        try:
            return parent.value, [int(children[i]) for i in range(min(count.value, 256))]
        finally:
            if children:
                x.XFree(children)
    def decorations(window, root, client, outer):
        if not client or not outer:
            return None
        parent, _ = tree(window)
        if not parent or parent == root:
            return None
        _, children = tree(parent)
        candidates = []
        pending = [(child, 0) for child in children]
        while pending:
            child, depth = pending.pop()
            b = geometry(child, root)
            if b and b['mapState']=='viewable' and 8 <= b['width'] <= 40 and 8 <= b['height'] <= 40 and abs(b['width']-b['height']) <= 12 and b['x'] > outer['x']+outer['width']/2 and b['y'] >= outer['y']-20 and b['y']+b['height'] <= client['y']:
                candidates.append({'id':child, **b})
            if depth < 2:
                _, nested = tree(child)
                pending.extend((item,depth+1) for item in nested)
        return {'frameWindow':parent,'buttonsRightToLeft':sorted(candidates,key=lambda b:b['x'],reverse=True),
                'meaning':'Read-only mapped titlebar child rectangles; actions must still use viewer input'}
    try:
        root = x.XDefaultRootWindow(display)
        active = prop(root, '_NET_ACTIVE_WINDOW')
        focus, revert = Window(), C.c_int()
        x.XGetInputFocus(display, C.byref(focus), C.byref(revert))
        clients = prop(root, '_NET_CLIENT_LIST_STACKING') or prop(root, '_NET_CLIENT_LIST')
        windows = []
        for wid in clients:
            client = geometry(wid, root)
            extents = prop(wid, '_NET_FRAME_EXTENTS')
            outer = None
            if client and len(extents) == 4:
                left, right, top, bottom = extents
                outer = {'x':client['x']-left, 'y':client['y']-top,
                         'width':client['width']+left+right, 'height':client['height']+top+bottom}
            windows.append({'id':wid, 'idHex':hex(wid), 'pid':next(iter(prop(wid, '_NET_WM_PID')), None),
                'title':prop(wid, '_NET_WM_NAME', True) or prop(wid, 'WM_NAME', True),
                'wmClass':(prop(wid, 'WM_CLASS', True) or '').split('\0'),
                'states':[atom_name(a) for a in prop(wid, '_NET_WM_STATE')],
                'types':[atom_name(a) for a in prop(wid, '_NET_WM_WINDOW_TYPE')],
                'wmState':next(iter(prop(wid, 'WM_STATE')), None),
                'frameExtents':extents, 'client':client, 'outer':outer,
                'decorations':decorations(wid, root, client, outer)})
        root_return, child = Window(), Window()
        rx, ry, wx, wy, mask = C.c_int(), C.c_int(), C.c_int(), C.c_int(), C.c_uint()
        valid_pointer = bool(x.XQueryPointer(display, root, C.byref(root_return), C.byref(child),
            C.byref(rx), C.byref(ry), C.byref(wx), C.byref(wy), C.byref(mask)))
        bitmap = C.create_string_buffer(32)
        x.XQueryKeymap(display, bitmap)
        cursor = {'status':'UNAVAILABLE'}
        try:
            fixes = C.CDLL('libXfixes.so.3')
            class CursorImage(C.Structure):
                _fields_ = [('x',C.c_short), ('y',C.c_short), ('width',C.c_ushort),
                    ('height',C.c_ushort), ('xhot',C.c_ushort), ('yhot',C.c_ushort),
                    ('cursor_serial',C.c_ulong), ('pixels',C.POINTER(C.c_ulong))]
            fixes.XFixesGetCursorImage.argtypes, fixes.XFixesGetCursorImage.restype = [Display], C.POINTER(CursorImage)
            image = fixes.XFixesGetCursorImage(display)
            if image:
                try:
                    c = image.contents
                    count = c.width*c.height
                    if count > 262144:
                        raise RuntimeError('Cursor dimensions exceed bounded observer limit')
                    pixels = [c.pixels[i] & 0xffffffff for i in range(count)]
                    opaque = [i for i,p in enumerate(pixels) if (p >> 24) & 255]
                    cursor = {'status':'AVAILABLE', 'x':c.x, 'y':c.y, 'width':c.width, 'height':c.height,
                        'hotspot':{'x':c.xhot,'y':c.yhot}, 'serial':c.cursor_serial,
                        'nontransparentPixels':len(opaque),
                        'shapeSHA256':hashlib.sha256(b''.join(p.to_bytes(4,'little') for p in pixels)).hexdigest()}
                finally:
                    x.XFree(C.cast(image, C.c_void_p))
        except Exception as error:
            cursor = {'status':'UNAVAILABLE', 'reason':str(error)}
        _, root_children = tree(root)
        popup_windows = []
        for wid in root_children:
            g = geometry(wid, root)
            if g and g['overrideRedirect'] and g['mapState']=='viewable' and 60 <= g['width'] <= 600 and 60 <= g['height'] <= 600:
                popup_windows.append({'id':wid, 'title':prop(wid, '_NET_WM_NAME', True) or prop(wid, 'WM_NAME', True), 'wmClass':(prop(wid,'WM_CLASS',True) or '').split('\0'), 'client':g})
        return {'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'source':'read-only libX11/EWMH/XFixes; adapted from independent QA observer for Xfwm buttons/popups; no injected actions', 'popupWindows':popup_windows,
            'root':geometry(root, root), 'workarea':prop(root, '_NET_WORKAREA'),
            'activeWindow':next(iter(active), None), 'inputFocus':focus.value,
            'stackingBottomToTop':clients, 'windows':windows,
            'pointer':{'valid':valid_pointer, 'x':rx.value, 'y':ry.value, 'mask':mask.value,
                       'pressedButtons':[b for b in range(1,6) if mask.value & (1 << (7+b))]},
            'allPressedKeycodes':[code for code in range(8,256) if bitmap.raw[code//8] & (1 << (code%8))],
            'cursor':cursor, 'observationRaces':errors}
    finally:
        x.XCloseDisplay(display)


if __name__ == '__main__':
    if '--describe' in sys.argv:
        print(json.dumps({'mode':'OFFLINE_DESCRIPTION_ONLY', 'readOnly':True,
            'fields':['root/workarea','EWMH windows/geometry/state/class/PID','active/focus',
                      'XQueryPointer/keymap','XFixes cursor shape/hotspot'],
            'guestOnly':True, 'requiresExplicitRuntimeHandoff':True}))
    else:
        print(json.dumps(observe()))
