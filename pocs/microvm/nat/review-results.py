#!/usr/bin/env python3
"""Recompute independent comparison arithmetic without changing raw evidence."""
import hashlib,json,math,sys
from pathlib import Path

def summary(xs):
 a=sorted(xs);n=len(a)
 return {'n':n,'median':(a[(n-1)//2]+a[n//2])/2 if n else None,'p95':a[math.ceil(.95*n)-1] if n else None,'max':a[-1] if n else None,'min':a[0] if n else None}
def same(a,b):return a==b or (isinstance(a,(int,float)) and isinstance(b,(int,float)) and math.isclose(a,b,rel_tol=1e-10,abs_tol=1e-8))
def assert_summary(actual,reported):
 for k,v in actual.items():assert same(v,reported[k]),(k,v,reported[k])
folder=Path(sys.argv[1]);manifest=json.loads((folder/'manifest.json').read_text());out={'manifest':str(folder/'manifest.json'),'state':manifest['state'],'windows':[],'method':{'median':'average sorted middle pair','p95':'nearest rank ceil(.95*n)-1','rawMutation':False}}
assert manifest['state'] in ['COMPLETE','COMPLETE_WITH_LIMITATIONS'],manifest['state']
for w in manifest['windows']:
 p=folder/w['file'];r=json.loads(p.read_text());obs=r['samples'];good=[s['ms'] for s in obs if s['status']=='observed' and math.isfinite(s['ms']) and s['ms']>=0]
 calc=summary(good);assert_summary(calc,r['latency']);assert r['latency']['attempts']==len(obs);assert r['latency']['failures']==len(obs)-len(good)
 intervals=summary([x for s in obs for x in s['polls']]);assert_summary(intervals,r['samplingIntervalsMs'])
 first=r['mediaSamples'][0]['videos'][0];last=next(x for x in r['mediaSamples'][-1]['videos'] if x['id']==first['id']);assert first['ssrc']==last['ssrc']
 for k,v in r['mediaSummary']['deltas'].items():
  if k in first and k in last:assert same(last[k]-first[k],v['delta'])
  else:assert v['status']=='unavailable' and v['delta'] is None
 jb=None
 if all(k in first and k in last for k in ['jitterBufferDelay','jitterBufferEmittedCount']):
  denominator=last['jitterBufferEmittedCount']-first['jitterBufferEmittedCount']
  if denominator>0:jb=1000*(last['jitterBufferDelay']-first['jitterBufferDelay'])/denominator
 assert same(jb,r['mediaSummary']['jitterBufferMeanMs'])
 resources=r['resourceSamples'];elapsed=resources[-1]['host']['monotonic']-resources[0]['host']['monotonic'];assert same(elapsed,r['resources']['elapsedSeconds'])
 for component in r['resources']['components']:
  if component['status']!='available':continue
  a=next(x for x in resources[0]['host']['processes'] if x['pid']==component['pid']);b=next(x for x in resources[-1]['host']['processes'] if x['pid']==component['pid'])
  assert same(100*(b['cpuSeconds']-a['cpuSeconds'])/elapsed,component['corePercent'])
 out['windows'].append({'window':r['id'],'state':r['state'],'rawSHA256':hashlib.sha256(p.read_bytes()).hexdigest(),'attempts':len(obs),'successes':len(good),'failures':len(obs)-len(good),'latencyMs':calc,'samplingMs':intervals,'jitterBufferMeanMs':jb,'jitterSnapshotsMs':r['jitterSnapshotsMs'],'mediaDeltas':r['mediaSummary']['deltas'],'observerVideoGaps':r['observerFrameGaps'],'observerRAFGaps':r['observerRAF'],'resources':r['resources'],'finalSelectedPair':r['finalMedia']['pair']})
out['status']='PASS';target=Path('pocs/microvm/artifacts/nat/statistics-review.json');target.write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({'status':out['status'],'windows':[{'window':w['window'],'state':w['state'],'attempts':w['attempts'],'successes':w['successes'],'failures':w['failures'],'latencyMs':w['latencyMs'],'samplingMs':w['samplingMs'],'jitterBufferMeanMs':w['jitterBufferMeanMs'],'jitterSnapshotsMs':w['jitterSnapshotsMs'],'resources':w['resources']} for w in out['windows']]},indent=2))
