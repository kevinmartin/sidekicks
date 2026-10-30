import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
test('guest MIME handler accepts GIO absolute filenames and preserves URL argv without a shell',()=>{
 const file=fileURLToPath(new URL('../config/browser-url.py',import.meta.url));
 const code=`import importlib.util,pathlib,unittest.mock as mock,sys
s=importlib.util.spec_from_file_location('browser_url',sys.argv[1]);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert m.browser_command([])==['/etc/sidekicks/browser.sh']
with mock.patch.object(pathlib.Path,'read_text',return_value='/tmp/guest-profile'):
 for value,expected in [('/home/neko/Sidekicks/a b.html','file:///home/neko/Sidekicks/a%20b.html'),('https://example.invalid/a?x=1&y=2','https://example.invalid/a?x=1&y=2'),('file:///home/neko/a.html','file:///home/neko/a.html')]:
  command=m.browser_command([value]);assert command[-2:]==['--new-tab',expected];assert command[0]=='firefox-esr'
for args in [['--new-window'],['javascript:alert(1)'],['file://remote/path'],['file:relative'],['relative.html'],['//remote/path'],['https://'],['https://example.invalid/\\n--bad'],['https://one.invalid','https://two.invalid']]:
 try:m.browser_command(args)
 except ValueError:pass
 else:raise AssertionError(args)
print('PASS')`;
 assert.equal(execFileSync('python3',['-c',code,file],{encoding:'utf8'}).trim(),'PASS');
});
