#!/usr/bin/env python3
"""Loopback datagrams framed over one private vsock stream per peer/target.

No destination address is accepted from the guest or payload. The host only
replies to a loopback endpoint which first sent to a reserved listener. The
guest only connects UDP to 127.0.0.1 in its configured Neko media range.
Reliable vsock streams add ordering/head-of-line behavior; this is not native
VM networking or an end-to-end loss-preserving UDP transport.
"""
import argparse
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import struct
import time

PORTS = range(59120, 59140)
MAX_DATAGRAM = 65507
MAX_BUFFER = 262144
MAX_PEERS = 64
IDLE_SECONDS = 60


def frame(payload):
    if len(payload) > MAX_DATAGRAM:
        raise ValueError('oversize datagram')
    return struct.pack('!H', len(payload)) + payload


def extract(buffer):
    packets = []
    while len(buffer) >= 2:
        size = struct.unpack('!H', buffer[:2])[0]
        if size > MAX_DATAGRAM:
            raise ValueError('oversize frame')
        if len(buffer) < size + 2:
            break
        packets.append(bytes(buffer[2:size+2]))
        del buffer[:size+2]
    return packets


def guest_udp(client, port):
    """Called only from the Neko profile's private vsock handler."""
    if port not in PORTS:
        raise ValueError('UDP destination outside Neko range')
    target = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    target.bind(('127.0.0.1', 0))
    target.connect(('127.0.0.1', port))
    client.setblocking(False)
    target.setblocking(False)
    incoming, outgoing = bytearray(), bytearray()
    last = time.monotonic()
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(client, selectors.EVENT_READ, 'stream')
            selector.register(target, selectors.EVENT_READ, 'udp')
            while time.monotonic() - last < IDLE_SECONDS:
                selector.modify(client, selectors.EVENT_READ | (selectors.EVENT_WRITE if outgoing else 0), 'stream')
                for key, flags in selector.select(1):
                    if key.data == 'stream':
                        if flags & selectors.EVENT_READ:
                            data = client.recv(65536)
                            if not data:
                                return
                            incoming.extend(data)
                            for packet in extract(incoming):
                                try:
                                    target.send(packet)
                                except (BlockingIOError, ConnectionRefusedError):
                                    pass  # UDP loss is allowed; never grow a send queue.
                            last = time.monotonic()
                        if flags & selectors.EVENT_WRITE:
                            count = client.send(outgoing)
                            del outgoing[:count]
                    else:
                        try:
                            packet = target.recv(MAX_DATAGRAM + 1)
                        except ConnectionRefusedError:
                            continue
                        encoded = frame(packet)
                        if len(outgoing) + len(encoded) <= MAX_BUFFER:
                            outgoing.extend(encoded)
                        last = time.monotonic()
    finally:
        target.close()


def host(uds_path, metrics_path, runner_pid=None):
    selector = selectors.DefaultSelector()
    listeners, peers = {}, {}
    stats = dict(hostReceived=0, guestReceived=0, hostBytes=0, guestBytes=0,
                 dropped=0, expired=0, opened=0, failures=0)
    running = True

    def stop(*_):
        nonlocal running
        running = False

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)

    def remove(identity, expired=False):
        peer = peers.pop(identity, None)
        if peer:
            selector.unregister(peer['stream'])
            peer['stream'].close()
            if expired:
                stats['expired'] += 1

    def record():
        report = dict(stats, activePeers=len(peers), bindAddress='127.0.0.1',
                      ports=list(PORTS), maxPeers=MAX_PEERS, maxBufferedBytesPerPeer=MAX_BUFFER,
                      idleSeconds=IDLE_SECONDS, transport='UDP endpoints / framed reliable vsock streams',
                      updatedAt=time.time(), running=running)
        temporary = metrics_path.with_suffix('.partial')
        temporary.write_text(json.dumps(report, indent=2) + '\n')
        temporary.replace(metrics_path)

    try:
        # Bind every endpoint before entering the loop. Any conflict fails closed.
        for port in PORTS:
            udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            udp.bind(('127.0.0.1', port))
            udp.setblocking(False)
            listeners[port] = udp
            selector.register(udp, selectors.EVENT_READ, ('udp', port))
        last_report = 0
        while running:
            if runner_pid is not None:
                try: os.kill(runner_pid, 0)
                except ProcessLookupError: break
            now = time.monotonic()
            for identity, peer in list(peers.items()):
                if now - peer['last'] > IDLE_SECONDS:
                    remove(identity, expired=True)
            for key, flags in selector.select(.2):
                kind, identity = key.data
                if kind == 'udp':
                    port = identity
                    packet, source = key.fileobj.recvfrom(MAX_DATAGRAM + 1)
                    if source[0] != '127.0.0.1' or len(packet) > MAX_DATAGRAM:
                        stats['dropped'] += 1
                        continue
                    identity = (port, source[1])
                    if identity not in peers:
                        if len(peers) >= MAX_PEERS:
                            stats['dropped'] += 1
                            continue
                        stream = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                        try:
                            stream.settimeout(1)
                            stream.connect(str(uds_path))
                            stream.sendall(struct.pack('!H', port))
                            stream.setblocking(False)
                        except OSError:
                            stream.close(); stats['failures'] += 1
                            continue
                        peers[identity] = dict(stream=stream, incoming=bytearray(), outgoing=bytearray(), last=now)
                        selector.register(stream, selectors.EVENT_READ, ('stream', identity))
                        stats['opened'] += 1
                    peer = peers[identity]
                    encoded = frame(packet)
                    if len(peer['outgoing']) + len(encoded) > MAX_BUFFER:
                        stats['dropped'] += 1
                    else:
                        peer['outgoing'].extend(encoded)
                        stats['hostReceived'] += 1; stats['hostBytes'] += len(packet)
                        peer['last'] = now
                        selector.modify(peer['stream'], selectors.EVENT_READ | selectors.EVENT_WRITE, ('stream', identity))
                else:
                    peer = peers.get(identity)
                    if not peer:
                        continue
                    try:
                        if flags & selectors.EVENT_READ:
                            data = peer['stream'].recv(65536)
                            if not data:
                                remove(identity); continue
                            peer['incoming'].extend(data)
                            for packet in extract(peer['incoming']):
                                try:
                                    listeners[identity[0]].sendto(packet, ('127.0.0.1', identity[1]))
                                    stats['guestReceived'] += 1; stats['guestBytes'] += len(packet)
                                except OSError:
                                    # Host OS may impose a smaller UDP payload limit.
                                    stats['dropped'] += 1
                            peer['last'] = now
                        if flags & selectors.EVENT_WRITE:
                            count = peer['stream'].send(peer['outgoing'])
                            del peer['outgoing'][:count]
                        selector.modify(peer['stream'], selectors.EVENT_READ | (selectors.EVENT_WRITE if peer['outgoing'] else 0), ('stream', identity))
                    except (OSError, ValueError):
                        stats['failures'] += 1; remove(identity)
            if now - last_report >= 1:
                record(); last_report = now
    finally:
        running = False
        for identity in list(peers): remove(identity)
        for udp in listeners.values(): udp.close()
        selector.close()
        record()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('uds', type=Path)
    parser.add_argument('metrics', type=Path)
    parser.add_argument('runner_pid', type=int, nargs='?')
    args = parser.parse_args()
    host(args.uds, args.metrics, args.runner_pid)
