#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools java mvn
export HG_TEST_DEMO_APPROVALS=1
exec bash scripts/with-api.sh mvn -B -f integrations/java-workflow/pom.xml -Pintegration verify
