#!/usr/bin/env bash
# Reproducible local measurements. Always creates and removes a disposable DB.
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools go initdb pg_ctl postgres
case "${1:-full}" in
  full) count=3; policy_time=500ms; api_time=500x ;;
  smoke) count=1; policy_time=1x; api_time=20x ;;
  *) echo 'Usage: bash scripts/benchmark.sh [full|smoke]' >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || { echo 'Too many arguments' >&2; exit 2; }
export COUNTERSEAL_TRACE=0
date -u '+Measured at: %Y-%m-%dT%H:%M:%SZ'
git rev-parse HEAD
go version
postgres --version
uname -sm
go test ./internal/policy -run '^$' -bench '^BenchmarkDiffScale$' -benchmem -benchtime="$policy_time" -count="$count"
bash scripts/test-postgres.sh go test ./internal/server -run '^$' -bench '^BenchmarkAuthorizationHTTP$' -benchtime="$api_time" -count="$count" -timeout=10m
