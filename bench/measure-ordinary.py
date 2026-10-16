#!/usr/bin/env python3
"""Qualification instrumentation around the existing OpenSSH/SFTP publisher.
SSH counters exclude IP/TCP retransmissions; payload passes include both existing
verification readbacks. Not a new transport or source repository builder.
"""
import json, pathlib, re, resource, subprocess, sys, time
root=pathlib.Path(sys.argv[1]);started=time.monotonic()
with (root/'publisher.log').open('wb') as log:
 process=subprocess.Popen(sys.argv[2:],stdout=log,stderr=log)
 peak=0
 while process.poll() is None:
  try:
   if pathlib.Path(f'/proc/{process.pid}/exe').resolve()==pathlib.Path(sys.argv[2]):
    status=pathlib.Path(f'/proc/{process.pid}/status').read_text()
    m=re.search(r'^VmHWM:\s+(\d+)',status,re.M)
    if m:peak=max(peak,int(m[1]))
  except OSError:pass
  time.sleep(.002)
 rc=process.wait()
wall=time.monotonic()-started;usage=resource.getrusage(resource.RUSAGE_CHILDREN)
connections=[]
for log in sorted(root.glob('ssh-*.log')):
 for sent,received in re.findall(r'Transferred: sent (\d+), received (\d+) bytes',log.read_text()):
  connections.append({'log':log.name,'sent':int(sent),'received':int(received)})
logical=sum(p.stat().st_size for p in (root/'source').rglob('*') if p.is_file())
result={'success':rc==0,'wall_seconds':wall,'child_cpu_user_seconds':usage.ru_utime,'child_cpu_system_seconds':usage.ru_stime,'sampled_publisher_vm_hwm_kib':peak,'rss_sampling_milliseconds':2,'logical_bytes':logical,'payload_passes':3,'payload_bytes_from_upload_plus_two_readback_semantics':3*logical,'publisher_private_scratch_bytes':logical,'ssh_connections':connections,'openssh_reported_transport_bytes':sum(c['sent']+c['received'] for c in connections),'ip_tcp_overhead_measured':False,'persistent_server_full_trees':1}
(root/'performance.json').write_text(json.dumps(result,indent=2)+'\n')
if rc:raise SystemExit(rc)
if not connections:raise SystemExit('No SSH byte counters recorded; do not claim wire measurement')
