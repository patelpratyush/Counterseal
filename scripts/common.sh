#!/usr/bin/env bash
# Shared lifecycle helpers for local development and CI (macOS/Linux).
set -euo pipefail
HG_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$HG_ROOT"
for directory in /usr/local/opt/postgresql@18/bin /opt/homebrew/opt/postgresql@18/bin; do
  if [[ -d "$directory" ]]; then export PATH="$directory:$PATH"; fi
done
require_tools() {
  local tool
  for tool in "$@"; do command -v "$tool" >/dev/null || { echo "Required tool missing: $tool" >&2; return 1; }; done
}
# Capture descendants before terminating the parent so npm/Maven children do not escape.
stop_tree() {
  local pid="${1:-}" child
  [[ -n "$pid" ]] || return 0
  local children
  children="$(pgrep -P "$pid" || true)"
  kill -TERM "$pid" 2>/dev/null || true
  for child in $children; do stop_tree "$child"; done
  local attempt
  for attempt in {1..50}; do
    kill -0 "$pid" 2>/dev/null || break
    sleep .1
  done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
api_url_from_log() {
  local pid="$1" log="$2" url attempt
  for attempt in {1..200}; do
    url="$(sed -n 's/^HandoffGuard listening on /http:\/\//p' "$log" | head -1)"
    if [[ -n "$url" ]]; then printf '%s' "$url"; return; fi
    kill -0 "$pid" 2>/dev/null || { echo "API exited. See $log" >&2; return 1; }
    sleep .1
  done
  echo "API startup timed out. See $log" >&2; return 1
}
