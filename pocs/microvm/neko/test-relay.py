#!/usr/bin/env python3
"""Exercise actual UDP sockets and stream framing without a VM."""
import importlib.util
from pathlib import Path
import socket,struct,threading,unittest
spec=importlib.util.spec_from_file_location('relay',Path(__file__).parents[1]/'microvm_udp.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class Framing(unittest.TestCase):
 def test_fragment_coalesce_empty_and_max(self):
  packets=[b'',b'a',bytes(range(256)),b'x'*m.MAX_DATAGRAM]
  raw=b''.join(map(m.frame,packets));buf=bytearray();out=[]
  for i in range(0,len(raw),317):
   buf.extend(raw[i:i+317]);out.extend(m.extract(buf))
  self.assertEqual(out,packets);self.assertEqual(buf,b'')
 def test_reject_large(self):
  with self.assertRaises(ValueError):m.frame(b'x'*(m.MAX_DATAGRAM+1))
  with self.assertRaises(ValueError):m.extract(bytearray(struct.pack('!H',65535)))
 def test_reject_arbitrary_destination(self):
  a,b=socket.socketpair()
  try:
   with self.assertRaises(ValueError):m.guest_udp(a,9)
  finally:a.close();b.close()
 def test_peer_separation_real_udp(self):
  target=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);target.bind(('127.0.0.1',59139));target.settimeout(2)
  pairs=[socket.socketpair(),socket.socketpair()]
  threads=[threading.Thread(target=m.guest_udp,args=(a,59139),daemon=True) for a,b in pairs]
  try:
   for t in threads:t.start()
   peers=[]
   for i,(a,b) in enumerate(pairs):
    b.settimeout(2);b.sendall(m.frame(('peer'+str(i)).encode()))
    packet,source=target.recvfrom(100);peers.append(source)
    self.assertEqual(packet,('peer'+str(i)).encode())
    target.sendto(('reply'+str(i)).encode(),source)
    raw=bytearray()
    while not (packets:=m.extract(raw)):raw.extend(b.recv(100))
    self.assertEqual(packets,[('reply'+str(i)).encode()])
   self.assertNotEqual(peers[0],peers[1])
  finally:
   for a,b in pairs:b.close()
   for t in threads:t.join(2)
   for a,b in pairs:a.close()
   target.close()
if __name__=='__main__':unittest.main()
