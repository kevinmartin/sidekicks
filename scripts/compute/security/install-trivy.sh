#!/bin/bash
# Workspace-only scanner. Explicit release + official published archive checksums.
set -euo pipefail
cd "$(dirname "$0")/../../.."
version=0.76.1
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) archive="trivy_${version}_macOS-ARM64.tar.gz"; digest=11a9b6cd2752011b54f01ea4bd0dbfa9f0c41dc6538d5a3f9d23955f471bf4e5 ;;
  Darwin-x86_64) archive="trivy_${version}_macOS-64bit.tar.gz"; digest=7aa762eacd582052d053b62dcf972a1ab058abe39a57ccae234ed8894945b6b7 ;;
  Linux-aarch64|Linux-arm64) archive="trivy_${version}_Linux-ARM64.tar.gz"; digest=e174dff2a6dba5e3ae41f8d0537a6324405bd08132497340304c6b20e0f16a1f ;;
  Linux-x86_64) archive="trivy_${version}_Linux-64bit.tar.gz"; digest=c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0 ;;
  *) echo 'Unsupported local scanner platform' >&2; exit 1 ;;
esac
mkdir -p .run/compute-tools .run/compute-trivy/home/config
scratch="$(mktemp -d "$PWD/.run/compute-trivy/install.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
curl --disable --fail --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
  --max-time 180 --max-filesize 157286400 \
  "https://github.com/aquasecurity/trivy/releases/download/v${version}/${archive}" -o "$scratch/archive.tgz"
if command -v sha256sum >/dev/null; then actual="$(sha256sum "$scratch/archive.tgz" | cut -d ' ' -f1)"; else actual="$(shasum -a 256 "$scratch/archive.tgz" | cut -d ' ' -f1)"; fi
[[ "$actual" == "$digest" ]] || { echo 'Trivy release checksum mismatch; refusing execution' >&2; exit 1; }
tar -xzf "$scratch/archive.tgz" -C "$scratch" trivy
chmod 0555 "$scratch/trivy"
# Execute only the checksum-verified binary, with no cloud/service mode.
env HOME="$PWD/.run/compute-trivy/home" XDG_CONFIG_HOME="$PWD/.run/compute-trivy/home/config" TRIVY_DISABLE_TELEMETRY=true "$scratch/trivy" --version | grep -F "Version: $version" >/dev/null
mv "$scratch/trivy" .run/compute-tools/trivy
printf '%s  %s\n' "$digest" "$archive" > .run/compute-trivy/installed-release.txt
env HOME="$PWD/.run/compute-trivy/home" XDG_CONFIG_HOME="$PWD/.run/compute-trivy/home/config" TRIVY_DISABLE_TELEMETRY=true .run/compute-tools/trivy --version
