#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools initdb pg_ctl
umask 077
work="$(mktemp -d /tmp/hg-pg.XXXXXX)"
started=false
child_pid=''
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  stop_tree "$child_pid"
  if $started; then pg_ctl -D "$work/data" -m fast -w stop >/dev/null || status=1; fi
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
initdb -D "$work/data" -U postgres --auth=trust --no-locale --encoding=UTF8 >/dev/null
pg_ctl -D "$work/data" -l "$work/postgres.log" -o "-h '' -k $work" -w start >/dev/null
started=true
export HANDOFFGUARD_TEST_DATABASE_URL="postgresql://postgres@/postgres?host=$work"
if [[ $# == 0 ]]; then set -- go test -race ./...; fi
"$@" &
child_pid=$!
wait "$child_pid"
child_pid=''
