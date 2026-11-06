#!/bin/bash
# Same observed-source fingerprint used by candidate builds and verification.
set -eEuo pipefail
cd "$(dirname "$0")/../.."
hash_stream() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | awk '{print $1}'
  else shasum -a 256 | awk '{print $1}'
  fi
}
hash_file() { hash_stream < "$1"; }
hash_paths() { while IFS= read -r -d '' name; do
  printf 'path\0%s\0' "$name"
  if [[ -L "$name" ]]; then
    printf 'symlink\0'; readlink "$name" || exit 1
  elif [[ -f "$name" ]]; then
    printf 'file\0'; hash_file "$name" || exit 1
  else
    echo 'Untracked source changed during fingerprint' >&2; exit 1
  fi
done; }
{
  printf 'revision\0'; git rev-parse HEAD || exit 1
  printf 'tracked-diff\0'; git --no-pager diff --binary --no-ext-diff --no-textconv HEAD -- || exit 1
  printf 'untracked\0'
  git ls-files -z --others --exclude-standard | hash_paths || exit 1
  printf 'ignored-image-inputs\0'
  git ls-files -z --others --ignored --exclude-standard -- infrastructure/images/clean infrastructure/images/assets/desktop infrastructure/images/desktop-neko/upstream-modules | hash_paths || exit 1
} | hash_stream
