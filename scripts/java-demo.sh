#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools java mvn
mvn -B -q -f integrations/java-workflow/pom.xml -DskipTests package
exec java -jar integrations/java-workflow/target/handoffguard-workflow.jar "$@"
