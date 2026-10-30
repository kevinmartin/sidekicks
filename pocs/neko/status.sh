#!/bin/bash
set -euo pipefail
curl -fsS --max-time 5 http://127.0.0.1:7792/health
