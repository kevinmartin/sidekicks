#!/bin/bash
set -euo pipefail
curl -sS --max-time 3 -H 'Content-Type: application/json' -d '{"action":"stop"}' http://127.0.0.1:7792/control >/dev/null || true
if docker container inspect sidekicks-neko >/dev/null 2>&1; then docker stop -t 10 sidekicks-neko; fi
