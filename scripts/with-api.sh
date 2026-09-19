#!/usr/bin/env bash
# Build the CLI and run a command against a disposable control API.
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools go openssl
: "${HANDOFFGUARD_TEST_DATABASE_URL:?Run through scripts/test-postgres.sh or supply a disposable database URL}"
[[ $# -gt 0 ]] || { echo 'Usage: scripts/with-api.sh COMMAND [ARGS...]' >&2; exit 2; }
umask 077
work="$(mktemp -d /tmp/hg-api.XXXXXX)"
api_pid='' child_pid=''
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  stop_tree "$child_pid"
  stop_tree "$api_pid"
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
export HANDOFFGUARD_BINARY="$work/handoffguard"
export HANDOFFGUARD_DATABASE_URL="$HANDOFFGUARD_TEST_DATABASE_URL"
export HANDOFFGUARD_API_TOKEN="$(openssl rand -hex 32)"
go build -o "$HANDOFFGUARD_BINARY" ./cmd/cli
"$HANDOFFGUARD_BINARY" keygen --out "$work/key" >/dev/null
"$HANDOFFGUARD_BINARY" server --key "$work/key.priv" --addr 127.0.0.1:0 >"$work/server.log" 2>&1 &
api_pid=$!
export HANDOFFGUARD_SERVER_URL
HANDOFFGUARD_SERVER_URL="$(api_url_from_log "$api_pid" "$work/server.log")"
"$@" &
child_pid=$!
wait "$child_pid"
child_pid=''
kill -TERM "$api_pid"
wait "$api_pid"
api_pid=''
echo 'PASS: integration command and graceful API shutdown'
