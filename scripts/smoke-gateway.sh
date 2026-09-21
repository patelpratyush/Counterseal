#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
export HG_TEST_DEMO_APPROVALS=1
exec bash scripts/with-api.sh bash scripts/demo-gateway.sh
