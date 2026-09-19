#!/usr/bin/env bash
# Disposable Compose project. Never removes a developer's saved Compose volumes.
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools docker openssl jq npm node
umask 077
work="$(mktemp -d /tmp/hg-compose.XXXXXX)"
project="hg-test-$$"
port="${HG_COMPOSE_TEST_PORT:-3137}"
[[ "$port" =~ ^[0-9]+$ && ${#port} -le 5 ]] && (( 10#$port > 0 && 10#$port <= 65535 )) || exit 2
printf 'HG_POSTGRES_PASSWORD=%s\nHG_API_TOKEN=%s\nHG_DASHBOARD_PASSWORD=%s\nHG_DASHBOARD_SECRET=%s\nHG_DASHBOARD_PORT=%s\n' \
  "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" "$(openssl rand -hex 16)" "$(openssl rand -hex 32)" "$port" > "$work/env"
compose=(docker compose --env-file "$work/env" -p "$project")
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ "$status" != 0 ]]; then "${compose[@]}" logs --tail 60; fi
  "${compose[@]}" --profile demo down --volumes --remove-orphans >/dev/null || status=1
  rm -r "$work"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
"${compose[@]}" --profile demo config --quiet
"${compose[@]}" --profile demo build
"${compose[@]}" up --detach --wait --wait-timeout 180
key_before="$("${compose[@]}" exec -T api sha256sum /data/server.pub)"
"${compose[@]}" run --rm demo > "$work/success.json"
jq -e '.audit.status=="VALID" and (.receipts | length==3)' "$work/success.json" >/dev/null
export HG_COMPOSE_RUN_ID HG_COMPOSE_PASSWORD HG_COMPOSE_URL
HG_COMPOSE_RUN_ID="$(jq -r .runId "$work/success.json")"
HG_COMPOSE_PASSWORD="$(sed -n 's/^HG_DASHBOARD_PASSWORD=//p' "$work/env")"
HG_COMPOSE_URL="http://localhost:$port"
if "${compose[@]}" run --rm demo --amount=825 > "$work/denied.log" 2>&1; then
  echo 'Unapproved refund unexpectedly succeeded' >&2; exit 1
fi
grep -q 'Counterseal denied the tool call' "$work/denied.log"
"${compose[@]}" run --rm demo --amount=825 --approve-demo-refund > "$work/approved.json"
jq -e '.audit.status=="VALID" and .receipts.BILLING.amount==825' "$work/approved.json" >/dev/null
# Recreate containers, preserving both database and signing-key volumes.
"${compose[@]}" down
"${compose[@]}" up --detach --wait --wait-timeout 180
key_after="$("${compose[@]}" exec -T api sha256sum /data/server.pub)"
[[ "$key_before" == "$key_after" ]] || { echo 'Signing key changed after recreation' >&2; exit 1; }
# Browser verifies that the original run and its audit survived recreation.
(cd dashboard && npx playwright test --config playwright.compose.config.ts)
echo 'PASS: Compose Java workflow, approval denial/allow, persistent keys/data, dashboard login and audit'
