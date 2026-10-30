#!/usr/bin/env python3
"""Task-local session lifecycle. Run from a terminal with VZ approval."""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.request

HERE = Path(__file__).resolve().parent
BUILD = HERE / ".build"
STATE = HERE / ".state"
CAPABILITIES = {"perSessionVM": True, "exec": True, "files": True, "capture": True, "viewerTCP": True, "networkEgress": False, "udp": False, "snapshots": False, "gpu": False, "browserSandbox": False, "cloudValidated": False, "appleContainerCLI": False}

def directory(name):
    if not re.fullmatch(r"[a-z][a-z0-9-]{0,11}", name): raise ValueError("session name must match [a-z][a-z0-9-]{0,11}")
    return STATE / name

def manifest(name):
    return json.loads((directory(name) / "session.json").read_text())

def active(name):
    path = directory(name) / "pid"
    if not path.exists(): return False
    pid = int(path.read_text())
    # Do not trust a stale PID file or signal another process.
    result = subprocess.run(["/bin/ps", "-p", str(pid), "-o", "command="], capture_output=True, text=True)
    if result.stderr.strip(): raise RuntimeError("cannot verify runner PID in this execution context: " + result.stderr.strip())
    command = result.stdout.strip()
    return str(BUILD / manifest(name).get("runner", "runner")) in command and str(directory(name) / "disk.ext4") in command

def rpc(name, request, timeout=35):
    with socket.socket(socket.AF_UNIX) as client:
        client.settimeout(timeout)
        client.connect(str(directory(name) / "rpc.sock"))
        client.sendall(json.dumps(request).encode() + b"\n")
        stream = client.makefile("rb")
        line = stream.readline(3000000)
        if not line: raise RuntimeError("guest agent closed connection; inspect console.log")
        response = json.loads(line)
        if not response["ok"]: raise RuntimeError(response["error"])
        return response["result"]

def create(name, viewer, api, profile="novnc"):
    if profile not in ("novnc", "neko", "neko-nat"): raise ValueError("profile must be novnc, neko or neko-nat")
    template = "base-nat.ext4" if profile == "neko-nat" else ("base-neko.ext4" if profile == "neko" else "base.ext4")
    runner = "runner-nat" if profile == "neko-nat" else ("runner-neko" if profile == "neko" else "runner")
    if viewer == api or any(port < 1024 or port > 65535 for port in (viewer, api)): raise ValueError("distinct unprivileged ports required")
    if {viewer, api} & {7790, 7791, 7792, 7901, 7902}: raise ValueError("existing POC ports are reserved")
    if not (BUILD / template).is_file() or not (BUILD / runner).is_file(): raise RuntimeError("run prepare.sh first")
    path = directory(name)
    path.mkdir(parents=True, mode=0o700, exist_ok=False)
    os.chmod(STATE, 0o700)
    try:
        # APFS copy-on-write clone; no host path is exposed to the guest.
        subprocess.run(["/bin/cp", "-c", str(BUILD / template), str(path / "disk.ext4")], check=True)
        if (path / "disk.ext4").stat().st_size != (BUILD / template).stat().st_size:
            raise RuntimeError("incomplete disk clone; template/session sizes differ")
        data = {"id": name, "profile": profile, "runner": runner, "backend": "direct-virtualization-framework", "cpus": 2, "memoryBytes": 2147483648, "viewer": f"http://127.0.0.1:{viewer}/", "api": f"http://127.0.0.1:{api}", "viewerPort": viewer, "apiPort": api, "sharedDirectories": [], "networkDevices": ["VZNATNetworkDeviceAttachment"] if profile == "neko-nat" else [], "capabilities": {**CAPABILITIES, "networkEgress": profile == "neko-nat", "udp": profile.startswith("neko"), "udpTransport": "native-guest-IP" if profile == "neko-nat" else ("framed-reliable-vsock" if profile == "neko" else None)}}
        (path / "session.json").write_text(json.dumps(data, indent=2) + "\n")
        return data
    except BaseException:
        shutil.rmtree(path)
        raise

def stop_relay(name):
    path = directory(name)
    relay_file = path / "udp-pid"
    if not relay_file.exists(): return
    relay_pid = int(relay_file.read_text())
    for attempt in range(41):
        result = subprocess.run(["/bin/ps", "-p", str(relay_pid), "-o", "command="], capture_output=True, text=True)
        if result.stderr.strip(): raise RuntimeError("cannot verify UDP relay PID: " + result.stderr.strip())
        command = result.stdout
        if str(HERE / "microvm_udp.py") not in command or str(path / "udp.sock") not in command:
            relay_file.unlink(missing_ok=True); return
        if attempt == 0: os.kill(relay_pid, signal.SIGTERM)
        if attempt == 40: raise RuntimeError("UDP relay stop pending; refusing silent orphan")
        time.sleep(.05)


def start(name):
    path = directory(name); data = manifest(name)
    if active(name): raise RuntimeError("session already running")
    stop_relay(name)
    if len(str(path / "rpc.sock").encode()) >= 103: raise ValueError("checkout path too long for local Unix control socket")
    (path / "rpc.sock").unlink(missing_ok=True)
    (path / "udp.sock").unlink(missing_ok=True)
    (path / "udp-metrics.json").unlink(missing_ok=True)
    begin = time.monotonic()
    console_path = path / "console.log"
    console_offset = console_path.stat().st_size if console_path.exists() else 0
    log = open(path / "runner.log", "ab", buffering=0)
    command = [str(BUILD / data.get("runner", "runner")), str(BUILD / "vmlinux"), str(path / "disk.ext4"), str(path), str(data["viewerPort"]), str(data["apiPort"])]
    if data.get("profile", "").startswith("neko"): command.append(data["profile"])
    proc = subprocess.Popen(command, stdout=log, stderr=log, start_new_session=True)
    (path / "pid").write_text(str(proc.pid))
    agent_ready = None
    health_ready = None
    deadline = begin + 40
    while time.monotonic() < deadline:
        if console_path.exists() and "Kernel panic" in console_path.read_bytes()[console_offset:][-8192:].decode(errors="replace"):
            raise RuntimeError("guest boot panicked; inspect console.log and stop this session before retry")
        if proc.poll() is not None: raise RuntimeError(f"runner exited {proc.returncode}; inspect {path}/runner.log and console.log")
        try:
            if agent_ready is None:
                info = rpc(name, {"op": "info"}, timeout=0.5); agent_ready = time.monotonic() - begin
                if data.get("profile") == "neko-nat" and info.get("network", {}).get("error"):
                    raise SystemError("guest NAT setup failed: " + info["network"]["error"])
            with urllib.request.urlopen(data["api"] + "/health", timeout=0.5) as result:
                if result.status == 200:
                    if health_ready is None: health_ready = time.monotonic() - begin
                    desktop = None
                    try:
                        with urllib.request.urlopen(data["api"] + "/desktop/state", timeout=.5) as ready:
                            desktop = json.load(ready)
                    except urllib.error.HTTPError as error:
                        if error.code != 404: raise
                    if desktop is not None:
                        if not desktop.get("ready"): raise RuntimeError("desktop is still starting")
                        timing = {"agentReadySeconds": agent_ready, "desktopHealthSeconds": health_ready,
                                  "desktopReadySeconds": time.monotonic() - begin,
                                  "readinessSource": "/desktop/state", "desktop": desktop,
                                  "pid": proc.pid, "bootId": info["bootId"], "kernel": info["kernel"], "network": info.get("network")}
                        break
                    # Historical pre-UX images still use the deliberate fixture startup.
                    fixture_route = "/fixture-state" if data.get("profile", "").startswith("neko") else "/fixture-events?latest=1"
                    with urllib.request.urlopen(data["api"] + fixture_route, timeout=0.5) as fixture:
                        content = json.load(fixture)
                        snapshots = [content] if data.get("profile", "").startswith("neko") and content.get("marker") else ([event for event in content if event.get("type") == "snapshot"] if isinstance(content, list) else [])
                    if snapshots:
                        timing = {"agentReadySeconds": agent_ready, "desktopHealthSeconds": health_ready, "fixtureReadySeconds": time.monotonic() - begin, "readinessSource": "legacy-fixture", "pid": proc.pid, "bootId": info["bootId"], "kernel": info["kernel"], "fixtureMarker": snapshots[-1]["marker"], "network": info.get("network")}
                        break
        except (OSError, ValueError, RuntimeError): pass
        time.sleep(0.1)
    else:
        raise RuntimeError("desktop readiness timed out; session remains available for diagnosis")
    if data.get("profile") == "neko":
        relay_log = open(path / "udp-relay.log", "ab", buffering=0)
        relay = subprocess.Popen([sys.executable, str(HERE / "microvm_udp.py"), str(path / "udp.sock"), str(path / "udp-metrics.json"), str(proc.pid)], stdout=relay_log, stderr=relay_log, start_new_session=True)
        (path / "udp-pid").write_text(str(relay.pid))
        for _ in range(30):
            if relay.poll() is not None: raise RuntimeError("UDP relay exited; inspect udp-relay.log; guest remains for diagnosis")
            metrics_file = path / "udp-metrics.json"
            if metrics_file.exists():
                metrics = json.loads(metrics_file.read_text())
                if metrics.get("running") is True and metrics.get("ports") == list(range(59120, 59140)): break
                if metrics.get("running") is False: raise RuntimeError("UDP relay failed initial bind; inspect udp-relay.log")
            time.sleep(.1)
        else: raise RuntimeError("UDP relay readiness timed out; inspect/stop exact session before retry")
        timing["udpRelayPid"] = relay.pid
    (path / "last-start.json").write_text(json.dumps(timing, indent=2) + "\n")
    return timing

def stop(name, crash=False):
    if not active(name):
        stop_relay(name)
        return {"running": False, "alreadyStopped": True}
    path = directory(name); pid = int((path / "pid").read_text())
    if not crash:
        # Flush guest filesystem data before VZ's stop (power-off, not suspend).
        result = rpc(name, {"op": "exec", "argv": ["/bin/sync"]})
        if result["exitCode"] != 0: raise RuntimeError("guest sync failed; use explicit crash only for a containment test")
    stop_relay(name)
    os.kill(pid, signal.SIGKILL if crash else signal.SIGTERM)
    for _ in range(200):
        if not active(name):
            (path / "pid").unlink(missing_ok=True); (path / "rpc.sock").unlink(missing_ok=True); (path / "udp.sock").unlink(missing_ok=True)
            return {"running": False, "crashed": crash, "filesystemFlushed": not crash}
        time.sleep(0.05)
    raise RuntimeError("stop pending; inspect the exact runner PID before any retry")

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["create", "start", "stop", "crash", "destroy", "status", "exec", "rpc", "read", "write", "capture", "endpoints"])
    parser.add_argument("name")
    parser.add_argument("arguments", nargs=argparse.REMAINDER)
    args = parser.parse_args(); op = args.operation; name = args.name; rest = args.arguments
    if rest[:1] == ["--"]: rest = rest[1:]
    if op == "create": result = create(name, int(rest[0]) if rest else 7903, int(rest[1]) if len(rest) > 1 else 7793, rest[2] if len(rest) > 2 else "novnc")
    elif op == "start": result = start(name)
    elif op in ("stop", "crash"): result = stop(name, crash=op == "crash")
    elif op == "destroy":
        if active(name): raise RuntimeError("stop the session before destroying its disk")
        stop_relay(name)
        manifest(name); shutil.rmtree(directory(name)); result = {"destroyed": name}
    elif op == "status": result = {**manifest(name), "running": active(name)}
    elif op == "endpoints": result = {key: manifest(name)[key] for key in ("viewer", "api", "capabilities")}
    elif op == "exec": result = rpc(name, {"op": "exec", "argv": rest})
    elif op == "rpc": result = rpc(name, json.loads(rest[0]))
    elif op == "write": result = rpc(name, {"op": "write", "path": rest[1], "base64": base64.b64encode(Path(rest[0]).read_bytes()).decode()})
    elif op == "read":
        result = rpc(name, {"op": "read", "path": rest[0]}); Path(rest[1]).write_bytes(base64.b64decode(result.pop("base64")))
    elif op == "capture":
        with urllib.request.urlopen(manifest(name)["api"] + "/screenshot?maxWidth=1280&maxHeight=720", timeout=10) as response:
            image = response.read(4194305)
            if len(image) > 4194304: raise RuntimeError("capture exceeds 4 MiB")
            Path(rest[0]).write_bytes(image); result = {"path": rest[0], "bytes": len(image), "contentType": response.headers.get("Content-Type")}
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    try: main()
    except Exception as error: print(str(error), file=sys.stderr); sys.exit(1)
