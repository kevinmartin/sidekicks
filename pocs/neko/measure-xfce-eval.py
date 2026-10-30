#!/usr/bin/env python3
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])


def manifest(filename):
    rows = {}
    for line in (root / 'packages' / filename).read_text().splitlines():
        package, version, size = line.split('\t')
        rows[package] = {'version': version, 'installedKiB': int(size)}
    return rows


before = manifest('packages-before.tsv')
core = manifest('packages-after-core.tsv')
session = manifest('packages-after-session.tsv')
images = {name: json.loads((root / f'{name}-image.json').read_text())[0] for name in ['base', 'core', 'session']}


def difference(previous, after):
    return {
        'installedDeltaKiB': sum(p['installedKiB'] for p in after.values()) - sum(p['installedKiB'] for p in previous.values()),
        'addedPackages': {name: p for name, p in after.items() if name not in previous},
        'changedPackages': {name: {'before': previous[name], 'after': p} for name, p in after.items() if name in previous and p != previous[name]},
        'removedPackages': sorted(set(previous)-set(after)),
    }


report = {
    'status': 'DEPENDENCIES_BUILT_XFCE_RUNTIME_UNTESTED',
    'scope': 'Official Debian package layers over accepted Neko UX image; accepted entrypoint/config retained.',
    'architecture': images['core']['Architecture'],
    'images': {name: {'id': p['Id'], 'tags': p['RepoTags'], 'bytes': p['Size']} for name, p in images.items()},
    'coreVersusBase': {**difference(before, core), 'imageLayerDeltaBytes': images['core']['Size']-images['base']['Size']},
    'sessionVersusCore': {**difference(core, session), 'imageLayerDeltaBytes': images['session']['Size']-images['core']['Size']},
    'sessionVersusBase': {**difference(before, session), 'imageLayerDeltaBytes': images['session']['Size']-images['base']['Size']},
    'transportBinaryHashesUnchanged': True,
    'runtimeStarted': False,
    'resourceMeasurements': 'UNTESTED: no CPU/RSS/frame-time/startup inference from package footprint.',
    'sizeCaveat': 'Docker Size is uncompressed layer size including package metadata. dpkg Installed-Size is package metadata. Both retain unused original Openbox/tint2/picom/PCManFM/Firefox/xterm files in the base.',
}
(root / 'footprint.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps({
    'status': report['status'], 'images': report['images'],
    'coreDeltaBytes': report['coreVersusBase']['imageLayerDeltaBytes'],
    'coreInstalledKiB': report['coreVersusBase']['installedDeltaKiB'],
    'optionalSessionDeltaBytes': report['sessionVersusCore']['imageLayerDeltaBytes'],
    'optionalSessionInstalledKiB': report['sessionVersusCore']['installedDeltaKiB'],
    'basePackagesChanged': report['sessionVersusBase']['changedPackages'],
    'transportBinaryHashesUnchanged': True,
},indent=2))
