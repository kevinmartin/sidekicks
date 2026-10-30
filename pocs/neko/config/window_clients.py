"""Read EWMH-managed clients without enumerating every X11 utility window."""
import re
import subprocess


def clients():
    root = subprocess.run(["xprop", "-root", "_NET_CLIENT_LIST"], capture_output=True, text=True, check=True).stdout
    for window in re.findall(r"0x[0-9a-fA-F]+", root):
        props = subprocess.run(["xprop", "-id", window, "WM_CLASS", "_NET_WM_WINDOW_TYPE", "_NET_WM_NAME"], capture_output=True, text=True)
        if props.returncode:
            continue  # Client closed between the two read-only queries.
        names = re.search(r'WM_CLASS\(STRING\) = "([^"]*)", "([^"]*)"', props.stdout)
        # EWMH defaults to NORMAL when the property is absent (legacy xterm).
        # Explicit utility/dialog/dock types never pass the normal-app filter.
        normal = "_NET_WM_WINDOW_TYPE_NORMAL" in props.stdout or bool(re.search(r"_NET_WM_WINDOW_TYPE:\s+not found", props.stdout))
        yield {"id": window, "class": names.group(2).lower() if names else "", "normal": normal, "dock": "_NET_WM_WINDOW_TYPE_DOCK" in props.stdout, "properties": props.stdout}
