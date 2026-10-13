#!/usr/bin/env python3
"""Measure KCP acknowledgment cost with the deployed executable in owned namespaces.

Run under scripts/with_test_lock.py outside sudo. A derived copy of the existing
namespace fixture retains its firewall sentinel and teardown. Only workload size,
full outbound capture and removal of unrelated timed loads are changed. No runtime
source or host networking settings are changed.
"""
import argparse
import collections
import datetime
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess

ROOT = Path(__file__).resolve().parents[1]
PAYLOAD = 100_000_000  # Decimal MB; one HTTP body, not a timed load approximation.

# The standalone target/generator uses bounded storage and checks a deterministic
# byte pattern via SHA-256. Its CPU and HTTP overhead are separate from the tunnel.
HELPER = r'''#!/usr/bin/env python3
import argparse,hashlib,json,time,urllib.request,http.server
p=argparse.ArgumentParser();p.add_argument('-mode');p.add_argument('-addr');p.add_argument('-verify-size',type=int,default=100000000);a=p.parse_args()
pattern=bytes(range(256))*256
if a.mode=='serve':
 class Target(http.server.BaseHTTPRequestHandler):
  protocol_version='HTTP/1.1'
  def do_GET(self):
   self.send_response(200);self.send_header('Content-Length','100000000');self.end_headers()
   left=100000000
   while left:
    b=pattern[:min(left,len(pattern))];self.wfile.write(b);left-=len(b)
  def log_message(self,*args):pass
 host,port=a.addr.rsplit(':',1);http.server.ThreadingHTTPServer((host,int(port)),Target).serve_forever()
elif a.mode=='verify':
 expected=hashlib.sha256();left=a.verify_size
 while left:
  b=pattern[:min(left,len(pattern))];expected.update(b);left-=len(b)
 start=time.monotonic();unix_start=time.time();actual=hashlib.sha256();n=0
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 with opener.open('http://'+a.addr+'/bulk',timeout=180) as response:
  assert int(response.headers['Content-Length'])==a.verify_size
  while True:
   b=response.read(65536)
   if not b:break
   actual.update(b);n+=len(b)
 elapsed=time.monotonic()-start
 assert n==a.verify_size and actual.digest()==expected.digest(), 'integrity/length failure'
 print(json.dumps({'verified_bytes':n,'sha256':actual.hexdigest(),'transfer_seconds':elapsed,'unix_start':unix_start,'unix_end':time.time(),'goodput_mbit':n*8/elapsed/1e6}))
else:raise RuntimeError('unsupported fixture mode')
'''


def capture_accounting(path, sender):
    """Parse full Ethernet/IPv4/raw-TCP KCP captures, retaining mixed ACKs too.

    ACKs are identified by inner command 82; outer TCP flags never classify them.
    Pure feedback packets own their complete headers. Mixed packets charge ACK
    record bytes only, because their headers already carry another command.
    Ethernet occupancy adds FCS, preamble/SFD and inter-frame gap; it is an
    estimate for Ethernet, not an assertion about the real WAN encapsulation.
    """
    counts = collections.Counter()
    commands = collections.Counter()
    with path.open('rb') as source:
        header = source.read(24)
        order = {b'\xd4\xc3\xb2\xa1':'<', b'\xa1\xb2\xc3\xd4':'>',
                 b'\x4d\x3c\xb2\xa1':'<', b'\xa1\xb2\x3c\x4d':'>'}.get(header[:4])
        if order is None or len(header) != 24:
            raise ValueError('unsupported/truncated pcap header')
        if struct.unpack(order+'I', header[20:24])[0] != 1:
            raise ValueError('capture must have Ethernet link type')
        while True:
            record = source.read(16)
            if not record:
                break
            if len(record) != 16:
                raise ValueError('truncated capture record')
            _, _, captured, original = struct.unpack(order+'IIII', record)
            frame = source.read(captured)
            if captured != original or len(frame) != captured:
                raise ValueError('truncated capture packet')
            if len(frame) < 54 or frame[12:14] != b'\x08\x00':
                raise ValueError('unexpected Ethernet frame')
            ip = frame[14:];ihl=(ip[0]&15)*4
            if ip[9] != 6 or struct.unpack('!H', ip[6:8])[0]&0x3fff:
                raise ValueError('unexpected protocol/fragment')
            if '.'.join(map(str,ip[12:16])) != sender:
                raise ValueError('outbound capture includes another direction')
            total=struct.unpack('!H',ip[2:4])[0];tcp=ip[ihl:total];thl=(tcp[12]>>4)*4
            data=tcp[thl:];offset=0;ack_bytes=0;ack_records=0;non_ack=False
            while offset < len(data):
                if len(data)-offset < 24:
                    raise ValueError('short KCP record: enc/FEC must be disabled')
                _,cmd,_,_,_,_,_,size=struct.unpack_from('<IBBHIIII',data,offset)
                length=24+size
                if cmd not in (81,82,83,84) or offset+length>len(data):
                    raise ValueError('invalid KCP command/length')
                commands[str(cmd)]+=1
                if cmd==82:
                    ack_bytes+=length;ack_records+=1
                else:
                    non_ack=True
                if cmd==81:
                    counts['push_record_bytes']+=length
                    counts['push_payload_bytes']+=size
                offset+=length
            if not data:
                raise ValueError('unexpected kernel TCP packet')
            counts['packets']+=1;counts['ip_bytes']+=total
            counts['ethernet_occupancy_bytes']+=max(original,60)+24
            counts['ack_records']+=ack_records;counts['ack_record_bytes']+=ack_bytes
            if ack_records:
                counts['packets_with_ack']+=1
                if non_ack:
                    counts['mixed_ack_packets']+=1
                    counts['ack_incremental_ip_bytes']+=ack_bytes
                    counts['ack_incremental_ethernet_bytes']+=ack_bytes
                else:
                    counts['pure_ack_packets']+=1;counts['pure_ack_ip_bytes']+=total
                    counts['pure_ack_ethernet_bytes']+=max(original,60)+24
                    counts['ack_incremental_ip_bytes']+=total
                    counts['ack_incremental_ethernet_bytes']+=max(original,60)+24
    return {'counts':dict(counts),'commands':dict(commands)}


def derived_fixture(out):
    """Keep the mature fixture cleanup but capture a single exact-size transfer."""
    original=(ROOT/'scripts/netns_bench.py').read_text()
    text=original
    def replace(old,new):
        nonlocal text
        if text.count(old)!=1:
            raise ValueError('namespace fixture changed; inspect adaptation: '+old[:80])
        text=text.replace(old,new)
    replace('ROOT = Path(__file__).resolve().parents[1]', 'ROOT = Path('+repr(str(ROOT))+')')
    replace('not 1 <= a.cold_read_bytes <= 16777216','not 1 <= a.cold_read_bytes <= 100000000')
    replace("spawn(s,'capture','tcpdump','-i','spq-s','-n','-w',str(out/'wire.pcap'),'-c','128','tcp','port','29999')",
            "captures=[]\n            for namespace,label,iface in ((c,'client','spq-c'),(s,'server','spq-s')):\n                captures.append(spawn(namespace,label+'-capture','tcpdump','-i',iface,'-Q','out','-s','0','-B','8192','-U','-n','-w',str(out/(label+'-out.pcap')),'tcp','port','29999'))")
    replace("modes = (['http','bulk'] if a.mode=='both' else [a.mode]) if not a.hold else ['hold']", 'modes = []  # Only the exact-size integrity transfer belongs to this experiment.')
    replace("decoded = ns(s,'tcpdump','-n','-vv','-r',str(out/'wire.pcap')).stdout\n            (out/'wire.txt').write_text(decoded)",
            "time.sleep(.5)  # Include the final ACKs before stopping capture.\n            for capture in captures:\n                capture.send_signal(signal.SIGINT)\n            for capture in captures:\n                if capture.wait(timeout=10)!=0:raise RuntimeError('capture failed')\n            for side in ('client','server'):\n                capture_log=(out/(side+'-capture.log')).read_text()\n                if '0 packets dropped by kernel' not in capture_log:raise RuntimeError('capture lost packets; reject accounting')")
    replace("recovery_fields='{enabled: true'", "recovery_fields='{enabled: true, stalled_after: 10s'")
    fixture=out/'namespace_fixture.py';fixture.write_text(text)
    helper=out/'transfer-helper';helper.write_text(HELPER);helper.chmod(0o755)
    return fixture,helper,hashlib.sha256(original.encode()).hexdigest()


def main():
    """Run one serialized matrix with immutable receipts and byte-level checks."""
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',required=True)
    parser.add_argument('--library-path')
    parser.add_argument('--output',required=True)
    parser.add_argument('--profiles',nargs='+',choices=['clean-local','clean-80ms','loss-reorder-80ms','asymmetric-50-10'],
                        default=['clean-local','clean-80ms','loss-reorder-80ms'])
    a=parser.parse_args()
    if os.geteuid()!=0:
        parser.error('run with sudo, under the shared test lock')
    out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=False)
    binary=Path(a.binary).resolve();fixture,helper,source_hash=derived_fixture(out)
    kcp={'mode':'manual','sndwnd':4096,'rcvwnd':4096,'mtu':1350,'nodelay':0,'interval':30,
         'resend':2,'nocongestion':1,'wdelay':True,'acknodelay':False,'small_write_flush':256,
         'smuxbuf':4194304,'streambuf':2097152,'adaptive_buffers':False,'ack_timestamps':False,
         'credit_hints':False,'smux_recovery_grace':0}
    common=['--binary',str(binary),'--benchmark-helper',str(helper),'--cold-read-bytes',str(PAYLOAD),
            '--capture','--sessions','4','--packet-workers','2','--conversation-listener',
            '--client-procs','4','--server-procs','2','--path-recovery','--preserve-connections',
            '--open-timeout','15s','--dial-timeout','5s','--client-flag','S','--server-flag','PA',
            '--block','null','--adaptive','on','--kcp-options',json.dumps(kcp),'--bridge',
            '--strict-rate','--rate-mbit','100','--down-rate-mbit','100','--queue-packets','2048','--seed','804']
    profiles={'clean-local':[],'clean-80ms':['--delay-ms','40'],
              'loss-reorder-80ms':['--delay-ms','40','--loss','.5','--reorder','5'],
              'asymmetric-50-10':['--delay-ms','40','--loss','.5','--reorder','5','--rate-mbit','50','--down-rate-mbit','10']}
    receipt={'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
             'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'payload_bytes':PAYLOAD,
             'source_fixture_sha256':source_hash,'derived_fixture_sha256':hashlib.sha256(fixture.read_bytes()).hexdigest(),
             'helper_sha256':hashlib.sha256(helper.read_bytes()).hexdigest(),'cases':[],
             'limits':['one download stream using one of four pooled carriers','per-case rate caps are recorded in command/fixture parameters',
                       'captures perturb timing; this measures feedback cost, not peak throughput',
                       'Ethernet occupancy includes FCS/preamble/IFG estimate; no WAN encapsulation',
                       'mixed packet header cost is not double-counted','no enc/FEC or optional ACK timestamps']}
    env=os.environ.copy()
    if a.library_path:env['LD_LIBRARY_PATH']=a.library_path
    try:
        for name in a.profiles:
            flags=profiles[name]
            directory=out/name;args=['python3','-B',str(fixture),'--output',str(directory),*common,*flags]
            print('START',name,flush=True)
            with (out/(name+'.log')).open('x') as log:
                run=subprocess.run(args,env=env,stdout=log,stderr=subprocess.STDOUT)
            row={'profile':name,'exit_code':run.returncode,'command':args}
            receipt['cases'].append(row)
            if (directory/'cleanup.json').exists():row['cleanup']=json.loads((directory/'cleanup.json').read_text())
            if run.returncode:raise RuntimeError('fixture failed; inspect '+str(out/(name+'.log')))
            report=json.loads((directory/'results.json').read_text());row['transfer']=report['results'][0]
            if row['transfer']['verified_bytes']!=PAYLOAD or report['binary_sha256']!=receipt['binary_sha256']:
                raise RuntimeError('payload/artifact mismatch')
            for side,ip in [('client','198.18.0.1'),('server','198.18.0.2')]:
                row[side]=capture_accounting(directory/(side+'-out.pcap'),ip)
            ack=row['client']['counts'];seconds=row['transfer']['transfer_seconds']
            row['ack_payload_percent']=ack.get('ack_record_bytes',0)/PAYLOAD*100
            row['ack_ip_percent']=ack.get('ack_incremental_ip_bytes',0)/PAYLOAD*100
            row['ack_ethernet_percent']=ack.get('ack_incremental_ethernet_bytes',0)/PAYLOAD*100
            row['average_ack_ip_mbit']=ack.get('ack_incremental_ip_bytes',0)*8/seconds/1e6
            print('RESULT',name,'ACK_IP_BYTES',ack.get('ack_incremental_ip_bytes',0),'ACK_IP_PERCENT',round(row['ack_ip_percent'],4),'ACK_MBIT',round(row['average_ack_ip_mbit'],4),'SECONDS',round(seconds,3),flush=True)
            (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
    finally:
        (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
        if 'SUDO_UID' in os.environ:
            for path in [out,*out.rglob('*')]:os.chown(path,int(os.environ['SUDO_UID']),int(os.environ['SUDO_GID']))
    print('RECEIPT',out/'receipt.json',flush=True)


if __name__=='__main__':main()
