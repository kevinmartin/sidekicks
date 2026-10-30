"""Host launcher decisions with mocked Docker/curl; never touches a runtime."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
CONFIG = (ROOT/'current-image.sh').read_text()
CURRENT = re.search(r"NOVNC_CURRENT_IMAGE='([^']+)'", CONFIG)[1]
BASELINE = re.search(r"NOVNC_TRANSPORT_BASELINE='([^']+)'", CONFIG)[1]

STUB = '''#!/usr/bin/env python3
import json,os,sys
args=sys.argv[1:]
with open(os.environ['NOVNC_TEST_LOG'],'a') as f:f.write(json.dumps([os.path.basename(sys.argv[0])]+args)+'\\n')
s=json.loads(os.environ['NOVNC_TEST_STATE'])
if os.path.basename(sys.argv[0])=='curl':sys.exit(0)
if args[0]=='inspect':
 if not s.get('exists'):sys.exit(1)
 if '-f' in args:
  fmt=args[args.index('-f')+1]
  print(s['actual'] if fmt=='{{.Image}}' else str(s.get('running',False)).lower())
 else:print('{}')
elif args[:2]==['image','inspect']:
 if args[-1]=='sidekicks-novnc:live-drag':
  if not s.get('baseline'):sys.exit(1)
  print(s['baseline'])
 elif '-f' in args:print('sha256:new-candidate')
 elif not s.get('imagePresent',True):sys.exit(1)
 else:print('{}')
elif args[0] not in ['start','run','build','logs']:sys.exit(91)
'''


class LauncherTests(unittest.TestCase):
    def invoke(self, script, state, *args):
        with tempfile.TemporaryDirectory(prefix='sidekicks-novnc-launch-test-') as d:
            p=Path(d)
            for name in ['docker','curl']:
                (p/name).write_text(STUB)
                (p/name).chmod(0o755)
            log=p/'calls.jsonl'
            env={**os.environ,'PATH':d+os.pathsep+os.environ['PATH'],
                 'NOVNC_TEST_LOG':str(log),'NOVNC_TEST_STATE':json.dumps(state)}
            result=subprocess.run(['bash',str(ROOT/script),*args],env=env,text=True,capture_output=True,timeout=10)
            calls=[json.loads(l) for l in log.read_text().splitlines()] if log.exists() else []
            return result,calls

    def test_stopped_current_container_is_started_without_rebuild_or_replacement(self):
        r,c=self.invoke('start.sh',{'exists':True,'actual':CURRENT,'running':False})
        self.assertEqual(r.returncode,0,r.stderr)
        self.assertIn(['docker','start','sidekicks-novnc'],c)
        self.assertFalse(any(x[1] in {'run','build','stop','rm','rename'} for x in c))

    def test_running_current_container_only_checks_health(self):
        r,c=self.invoke('start.sh',{'exists':True,'actual':CURRENT,'running':True})
        self.assertEqual(r.returncode,0,r.stderr)
        self.assertFalse(any(x[1] in {'run','start','build','stop','rm','rename'} for x in c))
        self.assertEqual(len([x for x in c if x[0]=='curl']),2)

    def test_baseline_container_mismatch_is_preserved_even_if_stopped(self):
        r,c=self.invoke('start.sh',{'exists':True,'actual':BASELINE,'running':False})
        self.assertNotEqual(r.returncode,0)
        self.assertIn('Coordinate stop/rename/preserve',r.stderr)
        self.assertTrue(all(x[1]=='inspect' for x in c))

    def test_missing_current_image_fails_without_implicit_old_build(self):
        r,c=self.invoke('start.sh',{'exists':False,'imagePresent':False})
        self.assertNotEqual(r.returncode,0)
        self.assertIn('Dockerfile.xfce-clean',r.stderr)
        self.assertFalse(any(x[1] in {'run','start','build','stop','rm'} for x in c))

    def test_absent_container_uses_exact_current_image_and_loopback_isolation(self):
        r,c=self.invoke('start.sh',{'exists':False,'imagePresent':True})
        self.assertEqual(r.returncode,0,r.stderr)
        run=next(x for x in c if x[1]=='run')
        self.assertEqual(run[-1],CURRENT)
        for flag in ['127.0.0.1:7901:6080','127.0.0.1:7791:8080','--cap-drop','no-new-privileges','512m']:
            self.assertIn(flag,run)
        self.assertNotIn('--privileged',run)
        self.assertNotIn('-v',run)

    def test_rebuild_selects_clean_recipe_and_never_changes_container(self):
        r,c=self.invoke('rebuild.sh',{'baseline':BASELINE})
        self.assertEqual(r.returncode,0,r.stderr)
        build=next(x for x in c if x[1]=='build')
        self.assertEqual(build[build.index('-f')+1],str(ROOT/'Dockerfile.xfce-clean'))
        self.assertEqual(build[build.index('-t')+1],'sidekicks-novnc:xfce-candidate')
        self.assertFalse(any(x[1] in {'run','start','stop','rm','rename'} for x in c))
        self.assertIn('isolated stage QA',r.stdout)

    def test_rebuild_rejects_changed_transport_baseline_before_build(self):
        r,c=self.invoke('rebuild.sh',{'baseline':'sha256:foreign'})
        self.assertNotEqual(r.returncode,0)
        self.assertIn('Restore that recorded image',r.stderr)
        self.assertFalse(any(x[1]=='build' for x in c))

    def test_rebuild_cannot_overwrite_current_or_historical_tags(self):
        for tag in ['sidekicks-novnc:local','sidekicks-novnc:live-drag','sidekicks-novnc:cloud-ux','sidekicks-novnc:polish-xfce-final',CURRENT]:
            with self.subTest(tag=tag):
                r,c=self.invoke('rebuild.sh',{'baseline':BASELINE},tag)
                self.assertEqual(r.returncode,2)
                self.assertEqual(c,[])


if __name__=='__main__':unittest.main()
