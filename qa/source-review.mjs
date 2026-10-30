import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';

const root = process.cwd();
const roots = ['harness', 'pocs/novnc', 'pocs/neko', 'scripts', 'shared'];
const include = /\.(go|py|mjs|js|ts|html|sh|yaml|yml|json)$|Dockerfile$/;
async function walk(dir) {
  const found=[];
  for (const entry of await fs.readdir(dir,{withFileTypes:true})) {
    if (/^(node_modules|\.cache|\.docker-public|__pycache__)$/.test(entry.name)) continue;
    const file=path.join(dir,entry.name);
    if (entry.isDirectory()) found.push(...await walk(file));
    else if(include.test(entry.name)) found.push(file);
  }
  return found;
}
const files=(await Promise.all(roots.map(walk))).flat().sort();
const manifests=[], findings=[];
const patterns=[
  ['privileged container', /--privileged|privileged:\s*true/],
  ['host network', /--network[= ]host|network_mode:\s*host/],
  ['broad host mount', /(?:--volume|-v)\s+[^\s]*(?:\/Users\/|\/home\/|docker\.sock)/],
  ['wildcard CORS', /Access-Control-Allow-Origin.{0,12}\*/],
  ['process invocation (manual scope review)', /exec\.Command|create_subprocess|child_process|\bexecFile\b|\bspawn\(/],
  ['loopback host publish', /-p\s+127\.0\.0\.1/],
  ['lease enforcement (manual review)', /compare_digest|timingSafeEqual|validLease|leaseToken|ownership changed|hostId|locked_controls/],
];
for(const file of files){
  const data=await fs.readFile(file);const text=data.toString();
  manifests.push({file,bytes:data.length,sha256:crypto.createHash('sha256').update(data).digest('hex')});
  text.split('\n').forEach((line,index)=>{for(const[label,pattern]of patterns)if(pattern.test(line))findings.push({file,line:index+1,check:label,source:line.trim().slice(0,500)});});
}
const report={generatedAt:new Date().toISOString(),root,files:manifests,manualReviewCandidates:findings,limitation:'Static candidates and exact source hashes only. This scan makes no safety or live-functionality pass claim.'};
await fs.mkdir('artifacts/independent-qa',{recursive:true});
await fs.writeFile('artifacts/independent-qa/source-review.json',JSON.stringify(report,null,2));
console.log(`Recorded ${files.length} source hashes and ${findings.length} review candidates.`);
