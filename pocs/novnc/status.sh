#!/usr/bin/env bash
set -euo pipefail
curl -fsS http://127.0.0.1:7791/health
printf '\n'
curl -fsS http://127.0.0.1:7791/desktop/state
printf '\n'
