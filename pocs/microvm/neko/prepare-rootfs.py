#!/usr/bin/env python3
"""Modify only a copied Neko rootfs for per-guest endpoint metadata."""
from pathlib import Path
import sys
root=Path(sys.argv[1])
s=(root/'server.mjs').read_text()
s=s.replace("const viewers=new Map()", "const viewerPort=Number(process.env.SIDEKICKS_VIEWER_PORT||7906),apiPort=Number(process.env.SIDEKICKS_API_PORT||7796);\nconst allowedPorts=new Set([7792,7902,8080,viewerPort,apiPort]);\nconst permittedHost=h=>{try{const u=new URL('http://'+h);return ['127.0.0.1','localhost','[::1]'].includes(u.hostname)&&allowedPorts.has(Number(u.port||80));}catch{return false;}};\nconst viewers=new Map()")
s=s.replace("const localOrigin=o=>!o||/^http:\\/\\/(127\\.0\\.0\\.1|localhost|\\[::1\\])(:\\d+)?$/.test(o);", "const localOrigin=o=>!o||(/^http:\\/\\//.test(o)&&permittedHost(o.slice(7)));")
s=s.replace("viewerUrl:'http://127.0.0.1:7902/',nativeViewerUrl:'http://127.0.0.1:7902/?mode=native'", "viewerUrl:`http://127.0.0.1:${viewerPort}/`,nativeViewerUrl:`http://127.0.0.1:${viewerPort}/?mode=native`")
s=s.replace("!/^(127\\.0\\.0\\.1|localhost|\\[::1\\])(?::(?:7790|7792|7902|8080))?$/.test(req.headers.host??'')", "!permittedHost(req.headers.host??'')")
s=s.replace("!/^(127\\.0\\.0\\.1|localhost|\\[::1\\]):(7792|7902)$/.test(req.headers.host??'')", "!permittedHost(req.headers.host??'')")
s=s.replace("server.listen(port,'0.0.0.0'", "server.listen(port,'127.0.0.1'")
assert "viewerPort" in s and "7790|7792" not in s and "(7792|7902)" not in s
(root/'server.mjs').write_text(s)
c=(root/'neko.yaml').read_text().replace('59020-59039','59120-59139')
c=c.replace('  cors:\n    allowed_origins: [\"http://127.0.0.1:7902\", \"http://localhost:7902\"]','  cors: [\"http://127.0.0.1:7906\", \"http://localhost:7906\", \"http://127.0.0.1:7902\"]')
(root/'neko.yaml').write_text(c)
