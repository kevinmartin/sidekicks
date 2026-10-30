// Evidence needs decoder/transport counters, not local network addresses or certificates.
const types=new Set(['inbound-rtp','outbound-rtp','codec','data-channel','candidate-pair','media-playout']);
export const sanitizeStats=stats=>stats.filter(s=>types.has(s.type));
export const sanitizeLog=logs=>logs.map(x=>JSON.parse(JSON.stringify(x).replace(/([?&]token=)[^\s"'&]+/g,'$1[redacted]')));
