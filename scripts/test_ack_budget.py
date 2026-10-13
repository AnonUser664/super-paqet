"""Validate ACK accounting independently of live transport timing."""
import importlib.util
from pathlib import Path
import struct
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('ack_budget',Path(__file__).with_name('measure_ack_budget.py'))
budget=importlib.util.module_from_spec(spec);spec.loader.exec_module(budget)


def record(cmd,payload=b''):
    """Construct the documented little-endian KCP wire header."""
    return struct.pack('<IBBHIIII',123,cmd,0,4096,100,2,1,len(payload))+payload


def packet(payload,flags=2):
    """Construct IPv4/TCP without options; checksums do not affect byte cost."""
    ip=bytearray(20);ip[0]=0x45;ip[9]=6;ip[2:4]=struct.pack('!H',40+len(payload))
    ip[12:16]=bytes([198,18,0,1]);ip[16:20]=bytes([198,18,0,2])
    tcp=bytearray(20);tcp[12]=0x50;tcp[13]=flags
    return bytes(12)+b'\x08\x00'+ip+tcp+payload


class Accounting(unittest.TestCase):
    """Header attribution must not confuse outer flags or mixed feedback."""
    def parse(self,frames,truncated=False):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'wire.pcap'
            data=struct.pack('<IHHIIII',0xa1b2c3d4,2,4,0,0,262144,1)
            for frame in frames:
                data+=struct.pack('<IIII',1,0,len(frame),len(frame)+int(truncated))+frame
            path.write_bytes(data)
            return budget.capture_accounting(path,'198.18.0.1')['counts']

    def test_pure_ack_full_headers(self):
        counts=self.parse([packet(record(82))])
        self.assertEqual(counts['ack_record_bytes'],24)
        self.assertEqual(counts['ack_incremental_ip_bytes'],64)
        self.assertEqual(counts['ack_incremental_ethernet_bytes'],102)

    def test_packed_ack_headers_count_once(self):
        counts=self.parse([packet(record(82)+record(82))])
        self.assertEqual(counts['ack_records'],2)
        self.assertEqual(counts['pure_ack_packets'],1)
        self.assertEqual(counts['ack_incremental_ip_bytes'],88)

    def test_mixed_ack_incremental_cost(self):
        counts=self.parse([packet(record(82)+record(81,b'data'))])
        self.assertEqual(counts['mixed_ack_packets'],1)
        self.assertEqual(counts['ack_incremental_ip_bytes'],24)
        self.assertEqual(counts['ack_incremental_ethernet_bytes'],24)

    def test_outer_ack_flag_is_not_kcp_ack(self):
        self.assertEqual(self.parse([packet(record(81,b'data'),24)]).get('ack_records'),0)
        self.assertEqual(self.parse([packet(record(82),2)])['ack_records'],1)

    def test_capture_truncation_rejected(self):
        with self.assertRaises(ValueError):self.parse([packet(record(82))],truncated=True)

    def test_repeated_data_is_not_unique_payload(self):
        counts=self.parse([packet(record(81,b'data')),packet(record(81,b'data'))])
        self.assertEqual(counts['push_payload_bytes'],8)
        self.assertEqual(counts['unique_push_payload_bytes'],4)
        self.assertEqual(counts['repeated_push_payload_bytes'],4)

    def test_malformed_kcp_rejected(self):
        with self.assertRaises(ValueError):self.parse([packet(record(82)[:-1])])


if __name__=='__main__':unittest.main()
