import struct
import unittest
from rfb_gate import ClientFrames, ProtocolError

class FramingTests(unittest.TestCase):
    def ready(self):
        p=ClientFrames()
        self.assertEqual(len(p.feed(b'RFB 003.008\n\x01\x01')),3)
        return p
    def test_fragmented_key_is_never_misclassified(self):
        packet=struct.pack('!BBHI',4,1,0,0xffe1)
        p=self.ready()
        for b in packet[:-1]: self.assertEqual(p.feed(bytes([b])),[])
        self.assertEqual(p.feed(packet[-1:]),[(True,packet)])
    def test_coalesced_framebuffer_request_then_input(self):
        request=struct.pack('!BBHHHH',3,1,0,0,1280,720)
        mouse=struct.pack('!BBHH',5,1,10,20)
        self.assertEqual(self.ready().feed(request+mouse),[(False,request),(True,mouse)])
    def test_variable_clipboard_and_encoding(self):
        enc=struct.pack('!BBHi',2,0,1,7)
        clip=struct.pack('!B3xi',6,3)+b'abc'
        self.assertEqual(self.ready().feed(enc+clip),[(False,enc),(True,clip)])
    def test_fail_closed_unknown_and_oversized(self):
        with self.assertRaises(ProtocolError): self.ready().feed(b'\xf9')
        with self.assertRaises(ProtocolError): self.ready().feed(struct.pack('!B3xi',6,2000000))
    def test_exclusive_client_refused(self):
        p=ClientFrames()
        with self.assertRaises(ProtocolError): p.feed(b'RFB 003.008\n\x01\x00')

if __name__=='__main__': unittest.main()
