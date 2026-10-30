#!/usr/bin/env python3
"""Start only the selected local image; preserve old containers on explicit replace."""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent
CONTAINER = 'sidekicks-neko'


def docker(*args, check=True):
    result = subprocess.run(['docker', *args], text=True, capture_output=True)
    if check and result.returncode:
        raise RuntimeError(result.stderr.strip() or 'Docker command failed')
    return result


def inspect(kind, name):
    result = docker(kind, 'inspect', name, check=False)
    if result.returncode:
        return None
    return json.loads(result.stdout)[0]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--replace', action='store_true', help='Stop and rename the task-owned old container before creating its replacement.')
    args = parser.parse_args()
    release = json.loads((ROOT / 'current-image.json').read_text())
    selected = os.environ.get('SIDEKICKS_NEKO_IMAGE', release['imageTag'])
    if not (selected.startswith('sidekicks-neko:') or selected.startswith('sha256:')):
        raise RuntimeError('Select an existing task-owned sidekicks-neko image or its exact local sha256 ID.')
    if os.environ.get('SIDEKICKS_NEKO_BUILD', '0') != '0':
        raise RuntimeError('start.sh does not build. Use build-current.sh explicitly; see XFCE.md. Frozen images are never overwritten.')
    image = inspect('image', selected)
    if image is None:
        raise RuntimeError('Selected local image is missing: ' + selected + '. No download or fallback was attempted. See pocs/neko/XFCE.md for the verified rebuild chain.')
    if selected in (release['imageTag'], release['imageId']) and image['Id'] != release['imageId']:
        raise RuntimeError('The frozen current tag no longer matches current-image.json; refusing to start an unverified replacement.')
    if image['Architecture'] != 'arm64' or image['Os'] != 'linux':
        raise RuntimeError('This tested launcher requires a native linux/arm64 image.')
    old = inspect('container', CONTAINER)
    if old:
        if old['Config'].get('Labels', {}).get('component') != 'neko':
            raise RuntimeError('Refusing to alter a container without component=neko.')
        if old['Image'] != image['Id']:
            if not args.replace:
                raise RuntimeError('Existing container uses a different image. For a coordinated cutover, rerun start.sh --replace; it stops and renames the old container for rollback.')
            stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ').lower()
            preserved = CONTAINER + '-preserved-' + stamp
            docker('stop', '-t', '10', CONTAINER)
            docker('rename', CONTAINER, preserved)
            print('Preserved previous container as ' + preserved)
            old = None
    if old:
        docker('start', CONTAINER)
    else:
        docker('run', '-d', '--pull=never', '--name', CONTAINER,
               '--label', 'app=sidekicks', '--label', 'component=neko',
               '--platform', 'linux/arm64', '--cpus=2', '--memory=2g',
               '--shm-size=512m', '--pids-limit=512',
               '--security-opt=no-new-privileges:true',
               '-p', '127.0.0.1:7792:7792', '-p', '127.0.0.1:7902:7902',
               '-p', '127.0.0.1:59020-59039:59020-59039/udp', image['Id'])
    print('Selected image: ' + image['Id'])
    print('Neko viewer: http://127.0.0.1:7902/')
    print('Native diagnostic: http://127.0.0.1:7902/?mode=native')
    print('Adapter: http://127.0.0.1:7792/health')


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
