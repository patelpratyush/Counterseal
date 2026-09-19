#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
exec bash scripts/with-api.sh npm run test:e2e --prefix dashboard
