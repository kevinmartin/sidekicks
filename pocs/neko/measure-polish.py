#!/usr/bin/env python3
"""Compare exported build manifests; never starts or queries a live desktop."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])


def manifest(name):
    rows = {}
    for line in (root / 'packages' / name).read_text().splitlines():
        package, version, size = line.split('\t')
        rows[package] = {'version': version, 'installedKiB': int(size)}
    return rows


before = manifest('packages-before.tsv')
thunar = manifest('packages-after-thunar.tsv')
after = manifest('packages-after.tsv')
added = {name: value for name, value in after.items() if name not in before}
changed = {name: {'before': before[name], 'after': value} for name, value in after.items() if name in before and value != before[name]}
base = json.loads((root / 'base-image.json').read_text())[0]
candidate = json.loads((root / 'candidate-image.json').read_text())[0]
report = {
    'status': 'BUILT_RUNTIME_UNTESTED',
    'baseImage': base['Id'], 'candidateImage': candidate['Id'],
    'candidateTags': candidate['RepoTags'], 'architecture': candidate['Architecture'],
    'imageBytesBefore': base['Size'], 'imageBytesAfter': candidate['Size'],
    'imageLayerDeltaBytes': candidate['Size'] - base['Size'],
    'dpkgInstalledDeltaKiB': sum(p['installedKiB'] for p in after.values()) - sum(p['installedKiB'] for p in before.values()),
    'thunarAndRequiredDependenciesDeltaKiB': sum(p['installedKiB'] for p in thunar.values()) - sum(p['installedKiB'] for p in before.values()),
    'subsequentResourceAndDefaultHandlerUtilitiesDeltaKiB': sum(p['installedKiB'] for p in after.values()) - sum(p['installedKiB'] for p in thunar.values()),
    'thunarAddedPackages': sorted(set(thunar)-set(before)),
    'addedPackages': added, 'changedPackages': changed,
    'runtimeCalls': False, 'themeSelected': 'shared original Sidekicks Adwaita overlay',
    'caveat': 'Uncompressed image layer delta includes package metadata and staged config; dpkg Installed-Size is package metadata, not runtime memory or compressed download size.',
}
(root / 'footprint.json').write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps({key: value for key, value in report.items() if key not in {'addedPackages', 'changedPackages'}}, indent=2))
