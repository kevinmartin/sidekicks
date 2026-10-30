#!/usr/bin/python3
"""POC guest PID 1 and private vsock agent; optional NAT profile, no host mount."""
import base64
import fcntl
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import struct
import subprocess
import threading
import time

def run(*argv):
    subprocess.run(argv, check=True)

for target, kind in [("/proc", "proc"), ("/sys", "sysfs"), ("/dev", "devtmpfs"), ("/run", "tmpfs"), ("/tmp", "tmpfs")]:
    if not os.path.ismount(target): run("/bin/mount", "-t", kind, kind, target)
for path in ("/dev/pts", "/dev/shm", "/workspace"):
    Path(path).mkdir(exist_ok=True)
run("/bin/mount", "-t", "devpts", "devpts", "/dev/pts")
run("/bin/mount", "-t", "tmpfs", "-o", "size=512m,mode=1777", "tmpfs", "/dev/shm")
os.chmod("/tmp", 0o1777)
# /tmp is fresh tmpfs, so image-build directories do not survive boot.
# Xfce's ICE session socket directory has standard root ownership/sticky mode.
Path("/tmp/.ICE-unix").mkdir(exist_ok=True)
os.chown("/tmp/.ICE-unix", 0, 0)
os.chmod("/tmp/.ICE-unix", 0o1777)
os.chown("/workspace", 1000, 1000)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
request = struct.pack("16sH14x", b"lo", 0x49)
fcntl.ioctl(sock, 0x8914, request)  # SIOCSIFFLAGS: bring guest loopback up
sock.close()
os.environ.update(DISPLAY=":99", HOME="/home/desktop", USER="desktop", LANG="C.UTF-8", LC_ALL="C.UTF-8")
# Endpoint metadata comes from this VM's validated host boot configuration.
parameters = dict(item.split("=", 1) for item in Path("/proc/cmdline").read_text().split() if "=" in item)
for environment, parameter, default in (("SIDEKICKS_VIEWER_PORT", "sidekicks.viewer_port", "7903"), ("SIDEKICKS_API_PORT", "sidekicks.api_port", "7793")):
    port = int(parameters.get(parameter, default))
    if not 1024 <= port <= 65535: raise ValueError("invalid session endpoint metadata")
    os.environ[environment] = str(port)
os.environ['SIDEKICKS_PUBLIC_VIEWER_PORT'] = os.environ['SIDEKICKS_VIEWER_PORT']
os.environ['SIDEKICKS_PUBLIC_API_PORT'] = os.environ['SIDEKICKS_API_PORT']

PROFILE = parameters.get("sidekicks.profile", "novnc")
if PROFILE not in ("novnc", "neko", "neko-nat"): raise ValueError("unknown guest profile")
GUEST_USER = "neko" if PROFILE.startswith("neko") else "desktop"
NETWORK = {"mode": "none", "interfaces": ["lo"], "egress": False}
if PROFILE == "neko-nat":
    from microvm_network import configure_nat
    try: NETWORK = configure_nat()
    except Exception as error:
        NETWORK = {"mode": "VZNATNetworkDeviceAttachment", "error": str(error), "egress": "not-ready"}
        print("SIDEKICKS_NAT_SETUP_FAILED: " + str(error), flush=True)
if PROFILE.startswith("neko"):
    for path in ("/tmp/.X11-unix", "/tmp/runtime-neko"):
        Path(path).mkdir(exist_ok=True)
        os.chown(path, 1000, 1000)
    os.chmod("/tmp/.X11-unix", 0o1777)
    os.chmod("/tmp/runtime-neko", 0o700)
    os.environ.update(HOME="/home/neko", USER="neko", DISPLAY=":99.0", XDG_RUNTIME_DIR="/tmp/runtime-neko", PULSE_SERVER="unix:/tmp/pulseaudio.socket", NEKO_CONFIG="/etc/sidekicks/neko.yaml", NEKO_SERVER_BIND="127.0.0.1:8080")
    command = ["/usr/bin/supervisord", "-c", "/etc/sidekicks/supervisord.conf"]
else:
    # New full-session images have a distinct entrypoint. Keep old exported
    # images reproducible without depending on Docker CMD/ENV after export.
    desktop_entrypoint = "/app/entrypoint-xfce.sh" if Path("/app/entrypoint-xfce.sh").is_file() else "/app/entrypoint.sh"
    command = ["/usr/sbin/runuser", "-u", "desktop", "--", desktop_entrypoint]
udp_slots = threading.BoundedSemaphore(64)

desktop_log = open("/tmp/desktop-init.log", "ab", buffering=0)
desktop = subprocess.Popen(command, stdout=desktop_log, stderr=desktop_log)
children = {desktop.pid}
children_lock = threading.Lock()

def reap():
    # PID 1 also adopts orphaned grandchildren. Never steal a live RPC's wait status.
    while True:
        with children_lock:
            for status in Path('/proc').glob('[0-9]*/status'):
                try:
                    pid = int(status.parent.name)
                    fields = dict(line.split(':', 1) for line in status.read_text().splitlines() if ':' in line)
                    if pid not in children and fields.get('PPid', '').strip() == '1' and fields.get('State', '').strip().startswith('Z'):
                        os.waitpid(pid, os.WNOHANG)
                except (OSError, ValueError, ChildProcessError): pass
        time.sleep(1)

threading.Thread(target=reap, daemon=True).start()

def relay(a, b):
    def pump(src, dst):
        try:
            while chunk := src.recv(65536): dst.sendall(chunk)
        except OSError: pass
        finally:
            try: dst.shutdown(socket.SHUT_WR)
            except OSError: pass
    thread = threading.Thread(target=pump, args=(a, b), daemon=True)
    thread.start(); pump(b, a); thread.join()

def user_command(argv, timeout=20):
    if not isinstance(argv, list) or not argv or any(not isinstance(x, str) for x in argv):
        raise ValueError("argv must be a nonempty string list")
    # Exec is always in the guest, as the disposable desktop user, without a shell unless requested.
    with children_lock:
        proc = subprocess.Popen(["/usr/sbin/runuser", "-u", GUEST_USER, "--", *argv], cwd="/workspace", stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        children.add(proc.pid)
    try:
        try: stdout, stderr = proc.communicate(timeout=min(float(timeout), 30))
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL); proc.communicate(); raise
        return {"exitCode": proc.returncode, "stdout": stdout[:1048576].decode(errors="replace"), "stderr": stderr[:1048576].decode(errors="replace"), "truncated": len(stdout) > 1048576 or len(stderr) > 1048576}
    finally:
        with children_lock: children.discard(proc.pid)

def rpc(request):
    op = request["op"]
    if op == "info":
        return {"bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(), "kernel": os.uname().release, "cpus": os.cpu_count(), "meminfo": Path("/proc/meminfo").read_text(), "mounts": Path("/proc/mounts").read_text(), "desktopPid": desktop.pid, "profile": PROFILE, "network": NETWORK, "udpRange": [59220, 59239] if PROFILE == "neko-nat" else ([59120, 59139] if PROFILE == "neko" else None)}
    if op == "exec": return user_command(request["argv"], request.get("timeout", 20))
    if op in ("read", "write"):
        path = Path("/workspace") / request["path"]
        if not path.resolve().is_relative_to(Path("/workspace")): raise ValueError("files limited to guest /workspace")
        if op == "write":
            content = base64.b64decode(request["base64"], validate=True)
            if len(content) > 1048576: raise ValueError("file exceeds 1 MiB")
            path.write_bytes(content); os.chown(path, 1000, 1000)
            return {"bytes": len(content)}
        content = path.read_bytes()
        if len(content) > 1048576: raise ValueError("file exceeds 1 MiB")
        return {"base64": base64.b64encode(content).decode(), "bytes": len(content)}
    raise ValueError("unsupported operation")

def handle(client):
    try:
        line = bytearray()
        while not line.endswith(b"\n") and len(line) < 64:
            chunk = client.recv(1)
            if not chunk: return
            line.extend(chunk)
        allowed_tcp = (b"tcp:7902\n", b"tcp:7792\n") if PROFILE.startswith("neko") else (b"tcp:6080\n", b"tcp:8080\n")
        if line == b"udp-v1\n" and PROFILE == "neko":
            if not udp_slots.acquire(blocking=False): return
            try:
                client.settimeout(3)
                raw = bytearray()
                while len(raw) < 2:
                    part = client.recv(2 - len(raw))
                    if not part: return
                    raw.extend(part)
                from microvm_udp import guest_udp
                guest_udp(client, struct.unpack('!H', raw)[0])
            finally: udp_slots.release()
        elif line in allowed_tcp:
            # Host-forwarded HTTP has a distinct guest loopback source. Internal
            # desktop-page capabilities must not rely on forgeable Host alone.
            with socket.create_connection(("127.0.0.1", int(line[4:])), timeout=10, source_address=("127.0.0.2", 0)) as target:
                target.settimeout(None); relay(client, target)
        elif line == b"rpc\n":
            stream = client.makefile("rb")
            raw = stream.readline(1500000)
            if not raw.endswith(b"\n"): raise ValueError("bounded JSON line required")
            try: result = {"ok": True, "result": rpc(json.loads(raw))}
            except Exception as error: result = {"ok": False, "error": str(error)}
            client.sendall(json.dumps(result).encode() + b"\n")
    except (OSError, ValueError): pass
    finally: client.close()

server = socket.socket(socket.AF_VSOCK, socket.SOCK_STREAM)
server.bind((socket.VMADDR_CID_ANY, 8000)); server.listen(32)
print("SIDEKICKS_MICROVM_AGENT_READY", flush=True)
while True:
    client, _ = server.accept()
    threading.Thread(target=handle, args=(client,), daemon=True).start()
