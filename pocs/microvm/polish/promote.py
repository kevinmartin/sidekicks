#!/usr/bin/env python3
"""Promote prepared Xfce templates after an exclusive, stage-verified handoff.

Default is read-only inspection. --apply stops only the three named POC guests;
old templates, runners, session disks and journals remain available for rollback.
No runner/kernel, host network setting or LAN listener is changed here.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import urllib.request

HERE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(HERE))
import microvm as vm

REVISION = os.environ.get("SIDEKICKS_POLISH_REVISION", "v1")
if REVISION not in ("v1", "v2"):
    raise ValueError("Supported prepared candidate revisions: v1, v2")
CANDIDATE = vm.BUILD / ("desktop-polish-" + REVISION)
BACKUP = vm.BUILD / "pre-desktop-polish-v1"
EVIDENCE = HERE / "artifacts/desktop-polish"
SESSIONS = [("a", 7903, 7793, "novnc"), ("neko", 7906, 7796, "neko"), ("nat", 7907, 7797, "neko-nat")]
TEMPLATES = {"base.ext4": "novnc/base.ext4", "base-neko.ext4": "neko/base.ext4", "base-nat.ext4": "base-nat.ext4"}


def lan_state():
    root = HERE.parent.parent
    config_path = root / ".run/lan/config.json"
    if not config_path.exists():
        return {"running": False, "config": None}
    config = json.loads(config_path.read_text())
    pid = config.get("pid")
    running = False
    if isinstance(pid, int) and pid > 1:
        result = subprocess.run(["/bin/ps", "-p", str(pid), "-o", "command="], capture_output=True, text=True)
        if result.stderr.strip():
            raise RuntimeError("Cannot inspect task LAN bridge PID: " + result.stderr.strip())
        running = str(root / "pocs/lan/bridge.mjs") in result.stdout
    return {"running": running, "config": config}


def sha(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        while data := stream.read(8 * 1024 * 1024):
            h.update(data)
    return h.hexdigest()


def clone(source, destination):
    if destination.exists():
        raise RuntimeError(f"Existing {destination}: inspect prior outcome before retry")
    subprocess.run(["/bin/cp", "-c", str(source), str(destination)], check=True)
    if destination.stat().st_size != source.stat().st_size:
        raise RuntimeError(f"Incomplete APFS clone: {destination}")


def inspect(verify_hashes=False):
    candidate = json.loads((CANDIDATE / "manifest.json").read_text())
    if candidate["status"] != "PREPARED_NOT_BOOT_VERIFIED":
        raise RuntimeError("Unexpected candidate preparation state")
    if candidate["nekoSource"] == "sha256:821d14fbc88b1ffb0700a1c332de0eeffdeb0c9b7553b087801328be895316e4":
        raise RuntimeError("This preserved v6 candidate failed Firefox Home acceptance. Prepare its fixed successor and select SIDEKICKS_POLISH_REVISION=v2; no guests were stopped")
    files = {}
    for target, relative in TEMPLATES.items():
        source = CANDIDATE / relative
        expected = candidate["files"][relative]
        if source.stat().st_size != expected["bytes"]:
            raise RuntimeError(f"Candidate size mismatch: {source}")
        if verify_hashes and sha(source) != expected["sha256"]:
            raise RuntimeError(f"Candidate digest mismatch: {source}")
        files[target] = {"source": str(source), **expected}
    sessions = []
    for name, viewer, api, profile in SESSIONS:
        manifest = vm.manifest(name)
        if (manifest["viewerPort"], manifest["apiPort"], manifest["profile"]) != (viewer, api, profile):
            raise RuntimeError(f"Unexpected current session mapping for {name}")
        if not vm.active(name):
            raise RuntimeError(f"Current {name} not running; inspect rather than assume a fresh deployment")
        with urllib.request.urlopen(manifest["api"] + "/control", timeout=3) as response:
            control = json.load(response)
        if control.get("owner") is not None or control.get("transition") not in (None, False):
            raise RuntimeError(f"Current {name} has an owner/transition; wait for exclusive handoff")
        sessions.append({"name": name, "manifest": manifest, "info": vm.rpc(name, {"op": "info"})})
    return {"candidate": candidate, "files": files, "sessions": sessions,
            "runners": {name: sha(vm.BUILD / name) for name in ("runner", "runner-neko", "runner-nat")},
            "kernel": sha(vm.BUILD / "vmlinux"), "lanBridge": lan_state(), "hostNetworkChanged": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true", help="Requires completed staged acceptance and exclusive handoff")
    parser.add_argument("--verify-hashes", action="store_true", help="Hash all candidate disks during read-only inspection")
    args = parser.parse_args()
    state = inspect(verify_hashes=args.apply or args.verify_hashes)
    if not args.apply:
        print(json.dumps({"operation": "READ_ONLY_INSPECTION", **state}, indent=2))
        return
    if BACKUP.exists():
        raise RuntimeError("Promotion backup already exists; inspect journal before retry")
    for name, _, _, _ in SESSIONS:
        if (vm.STATE / (name + "-pre-polish-v1")).exists():
            raise RuntimeError("Archived prior session exists; inspect before retry")
    for name in TEMPLATES:
        if (vm.BUILD / (name + ".polish-next")).exists():
            raise RuntimeError("Prepared next template already exists; inspect uncertain prior outcome before stopping guests")
    if shutil.disk_usage(vm.BUILD).free < 4 * 1024**3:
        raise RuntimeError("Keep at least4GiB free before promotion; no files were removed")
    BACKUP.mkdir()
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    journal = {"startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
               "status": "IN_PROGRESS", "before": state, "steps": []}
    journal_path = EVIDENCE / "promotion.json"

    def record(operation, value):
        journal["steps"].append({"at": datetime.datetime.now(datetime.timezone.utc).isoformat(), "operation": operation, "result": value})
        partial = journal_path.with_suffix(".json.partial")
        partial.write_text(json.dumps(journal, indent=2) + "\n")
        partial.replace(journal_path)

    try:
        for name in (*TEMPLATES, "runner", "runner-neko", "runner-nat", "vmlinux"):
            clone(vm.BUILD / name, BACKUP / name)
        (BACKUP / "before.json").write_text(json.dumps(state, indent=2) + "\n")
        record("backup", str(BACKUP))
        # Prepare all replacement files before the first service disruption.
        # This is still three per-file atomic publications, not a transaction.
        for name, relative in TEMPLATES.items():
            clone(CANDIDATE / relative, vm.BUILD / (name + ".polish-next"))
            record("prepare-template:" + name, relative)
        for name, _, _, _ in SESSIONS:
            record("stop:" + name, vm.stop(name))
            archived = vm.STATE / (name + "-pre-polish-v1")
            vm.directory(name).rename(archived)
            record("archive:" + name, str(archived))
        for name, relative in TEMPLATES.items():
            temporary = vm.BUILD / (name + ".polish-next")
            temporary.replace(vm.BUILD / name)
            record("template:" + name, relative)
        for name, viewer, api, profile in SESSIONS:
            created = vm.create(name, viewer, api, profile)
            template = "novnc/base.ext4" if profile == "novnc" else ("base-nat.ext4" if profile == "neko-nat" else "neko/base.ext4")
            created["desktopCandidate"] = {"revision": REVISION,
                "sourceImage": state["candidate"]["novncSource" if profile == "novnc" else "nekoSource"],
                "template": template, **state["candidate"]["files"][template]}
            manifest_path = vm.directory(name) / "session.json"
            manifest_path.with_suffix(".json.partial").write_text(json.dumps(created, indent=2) + "\n")
            manifest_path.with_suffix(".json.partial").replace(manifest_path)
            record("create:" + name, created)
            record("start:" + name, vm.start(name))
        after = {name: sha(vm.BUILD / name) for name in state["runners"]}
        if after != state["runners"] or sha(vm.BUILD / "vmlinux") != state["kernel"]:
            raise RuntimeError("Runner/kernel changed unexpectedly")
        network = vm.rpc("nat", {"op": "info"})["network"]
        lan = lan_state()
        stale = lan["running"] and (lan["config"] or {}).get("natGuest") != network.get("ipv4")
        journal["status"] = "DEPLOYED_REQUIRES_LAN_TARGET_REFRESH" if stale else "DEPLOYED_PENDING_INDEPENDENT_QA"
        record("complete", {"runnerKernelUnchanged": True, "lanBridgeChanged": False,
               "natNetwork": network, "lanBridge": lan, "lanNatTargetMatches": not stale,
               "requiredAction": "In the coordinated window, stop/start only scripts/lan.sh, then verify17907 selected UDP/media" if stale else "Independent production viewer/LAN checks remain required"})
    except BaseException as error:
        journal["status"] = "PARTIAL_REQUIRES_INSPECTION"
        record("error", str(error))
        raise
    print(json.dumps({"status": journal["status"], "journal": str(journal_path), "backup": str(BACKUP)}, indent=2))


if __name__ == "__main__":
    main()
