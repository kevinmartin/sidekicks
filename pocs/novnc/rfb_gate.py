"""Small fail-closed RFB 3.8/None client-stream framing gate, not a general RFB server."""
import struct

class ProtocolError(ValueError):
    pass

class ClientFrames:
    def __init__(self):
        self.buf = bytearray()
        self.stage = 0

    def feed(self, data):
        self.buf.extend(data)
        if len(self.buf) > 1048576:
            raise ProtocolError('client buffer too large')
        frames = []
        while self.buf:
            if self.stage < 3:
                n = (12, 1, 1)[self.stage]
                if len(self.buf) < n:
                    break
                packet = bytes(self.buf[:n])
                if self.stage == 0 and packet != b'RFB 003.008\n':
                    raise ProtocolError('only RFB 3.8 is supported')
                if self.stage == 1 and packet != b'\x01':
                    raise ProtocolError('only loopback None security is supported')
                if self.stage == 2 and packet != b'\x01':
                    raise ProtocolError('viewer must request shared desktop')
                self.stage += 1
                frames.append((False, packet))
                del self.buf[:n]
                continue
            typ = self.buf[0]
            n = {0:20, 3:10, 4:8, 5:6, 150:10, 250:4, 255:12}.get(typ)
            if typ == 2:
                if len(self.buf) < 4: break
                n = 4 + 4*struct.unpack_from('!H', self.buf, 2)[0]
            elif typ == 6:
                if len(self.buf) < 8: break
                n = 8 + abs(struct.unpack_from('!i', self.buf, 4)[0])
            elif typ == 248:
                if len(self.buf) < 9: break
                n = 9 + self.buf[8]
            elif typ == 251:
                if len(self.buf) < 8: break
                n = 8 + 16*self.buf[6]
            if n is None or n > 1048576:
                raise ProtocolError(f'unsupported client message {typ}')
            if len(self.buf) < n: break
            packet = bytes(self.buf[:n])
            if typ == 255 and packet[1] != 0:
                raise ProtocolError('unknown extended input message')
            # Desktop resize and power controls also require ownership.
            frames.append((typ in {4,5,6,250,251,255}, packet))
            del self.buf[:n]
        return frames
