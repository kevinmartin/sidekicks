"""Guest-only DHCP/DNS for the explicitly egress-enabled NAT profile.

Does not configure the macOS host or scan any network. NAT is not an egress
allowlist: the guest may reach host/LAN services, which tests must not probe.
"""
import fcntl
import ipaddress
import os
import re
from pathlib import Path
import socket
import struct
import subprocess
import time


def configure_nat():
    udev_log = open('/tmp/sidekicks-udevd.log', 'ab', buffering=0)
    subprocess.run(['/usr/lib/systemd/systemd-udevd', '--daemon', '--resolve-names=never'], stdout=udev_log, stderr=udev_log, check=True)
    subprocess.run(['/usr/bin/udevadm', 'trigger', '--action=add', '--subsystem-match=net'], stdout=udev_log, stderr=udev_log, check=True)
    subprocess.run(['/usr/bin/udevadm', 'settle', '--timeout=5'], stdout=udev_log, stderr=udev_log, check=True)
    interfaces = [p.name for p in Path('/sys/class/net').iterdir() if p.name != 'lo']
    if len(interfaces) != 1:
        raise RuntimeError('NAT profile requires exactly one NIC: ' + repr(interfaces))
    interface = interfaces[0]
    directory = Path('/run/systemd/network')
    directory.mkdir(parents=True, exist_ok=True)
    (directory / '01-sidekicks.network').write_text(
        '[Match]\nName=' + interface + '\n\n[Network]\nDHCP=ipv4\n'
        'IPv6AcceptRA=no\nLinkLocalAddressing=no\n\n[DHCPv4]\n'
        'UseDNS=yes\nUseHostname=no\nUseNTP=no\nUseRoutes=yes\n')
    log = open('/tmp/sidekicks-networkd.log', 'ab', buffering=0)
    proc = subprocess.Popen(['/usr/lib/systemd/systemd-networkd'], stdout=log, stderr=log)
    deadline = time.monotonic() + 20
    lease, address = {}, None
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise RuntimeError('networkd exited: ' + Path('/tmp/sidekicks-networkd.log').read_text()[-3000:])
        for path in Path('/run/systemd/netif/leases').glob('*'):
            lease = dict(line.split('=', 1) for line in path.read_text().splitlines() if '=' in line and not line.startswith('#'))
            if lease.get('ADDRESS'):
                address = str(ipaddress.IPv4Address(lease['ADDRESS']))
                break
        if address and lease.get('DNS') and lease.get('ROUTER'):
            break
        time.sleep(.1)
    else:
        raise RuntimeError('DHCP lease/DNS/default route timed out: ' + Path('/tmp/sidekicks-networkd.log').read_text()[-3000:])
    dns = [str(ipaddress.ip_address(x)) for x in lease['DNS'].split()]
    Path('/etc/resolv.conf').write_text(''.join('nameserver ' + x + '\n' for x in dns))
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        raw = fcntl.ioctl(sock, 0x8915, struct.pack('256s', interface.encode()[:15]))
        actual = socket.inet_ntoa(raw[20:24])
    if actual != address:
        raise RuntimeError('DHCP/interface address mismatch')
    config = Path('/etc/sidekicks/neko.yaml')
    text = config.read_text()
    entries = re.findall(r'(?m)^\s*nat1to1: \[([^\]\r\n]+)\]$', text)
    if len(entries) != 1: raise RuntimeError('expected one NAT1to1 address entry')
    ipaddress.IPv4Address(entries[0])
    text = re.sub(r'(?m)^(\s*)nat1to1: \[[^\]\r\n]+\]$', lambda m: m[1] + 'nat1to1: [' + address + ']', text)
    text = text.replace('epr: 59120-59139', 'epr: 59220-59239')
    config.write_text(text)
    return dict(mode='VZNATNetworkDeviceAttachment', interface=interface,
                ipv4=address, gateway=lease['ROUTER'], dns=dns,
                route=Path('/proc/net/route').read_text(), egress=True,
                egressIsolated=False, networkdPid=proc.pid,
                warning='NAT may reach host/LAN services; no private-service tests authorized')
