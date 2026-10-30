#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$here/.build"
swiftc -module-cache-path "$here/.build/module-cache" "$here/Runner.swift" -o "$here/.build/runner.next"
codesign --force --sign - --timestamp=none --entitlements "$here/vz.entitlements" "$here/.build/runner.next"

mv "$here/.build/runner.next" "$here/.build/runner"
