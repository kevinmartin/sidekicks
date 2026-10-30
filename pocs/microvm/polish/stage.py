#!/usr/bin/env python3
"""Create/boot only fresh polish-a and polish-nat from prepared candidates.

Canonical templates and sessions stay unchanged. Uses the current signed
runner/kernel and existing private session boundary. Refuses uncertain retries.
"""
import argparse
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(HERE))
import microvm as vm
from promote import CANDIDATE, clone, sha

TARGETS = {"novnc": ("polish-a", 17803, 17793, "novnc", "novnc/base.ext4"),
           "nat": ("polish-nat", 17807, 17797, "neko-nat", "base-nat.ext4")}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=TARGETS)
    parser.add_argument("--start", action="store_true", help="Create a new candidate session and start it")
    args = parser.parse_args()
    name, viewer, api, profile, relative = TARGETS[args.target]
    source = CANDIDATE / relative
    candidate = json.loads((CANDIDATE / "manifest.json").read_text())
    expected = candidate["files"][relative]
    if source.stat().st_size != expected["bytes"]:
        raise RuntimeError("Incomplete prepared template")
    plan = {"id": name, "profile": profile, "source": str(source),
            "viewer": f"http://127.0.0.1:{viewer}/", "api": f"http://127.0.0.1:{api}",
            "canonicalTemplatesChanged": False}
    if not args.start:
        print(json.dumps({"status": "READ_ONLY_PLAN", **plan}, indent=2))
        return
    if vm.directory(name).exists():
        raise RuntimeError("Stage already exists: inspect it; do not duplicate a verifier or VM")
    if sha(source) != expected["sha256"]:
        raise RuntimeError("Prepared template digest mismatch")
    # create() publishes the standard manifest and a cheap APFS clone of the
    # canonical disk. Before any boot, atomically replace ONLY this fresh
    # never-started stage clone with the exact candidate filesystem.
    manifest = vm.create(name, viewer, api, profile)
    temporary = vm.directory(name) / "candidate.ext4"
    clone(source, temporary)
    temporary.replace(vm.directory(name) / "disk.ext4")
    manifest["desktopCandidate"] = {"source": str(source), **expected}
    (vm.directory(name) / "session.json").write_text(json.dumps(manifest, indent=2) + "\n")
    timing = vm.start(name)
    print(json.dumps({"status": "BOOTED_PENDING_INDEPENDENT_QA", **plan, "startup": timing}, indent=2))


if __name__ == "__main__":
    main()
