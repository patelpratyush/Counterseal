#!/usr/bin/env bash
# Persistent preview. The database listens only on a private Unix socket.
set -euo pipefail
source "$(dirname "$0")/common.sh"
open_browser=true seed=true check=false port=3000
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-open) open_browser=false ;;
    --no-seed) seed=false ;;
    --check) check=true ;;
    --port) shift; port="${1:?--port requires a number}" ;;
    --help) echo 'Usage: ./start.sh [--no-open] [--no-seed] [--check] [--port NUMBER]'; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done
[[ "$port" =~ ^[0-9]+$ && ${#port} -le 5 ]] && (( 10#$port <= 65535 )) || { echo 'Port must be 0–65535' >&2; exit 2; }
require_tools go node npm initdb pg_ctl jq curl openssl
if $seed; then require_tools java mvn; fi
umask 077
state="${HANDOFFGUARD_PREVIEW_DIR:-$HG_ROOT/.local-preview}"
mkdir -p "$state"
state="$(cd "$state" && pwd)"
if ! mkdir "$state/launcher.lock.d" 2>/dev/null; then
  echo 'Preview lock exists. Stop the other launcher first. If it crashed, remove .local-preview/launcher.lock.d.' >&2
  exit 1
fi
printf '%s\n' "$$" > "$state/launcher.lock.d/pid"
socket_dir='' api_pid='' dashboard_pid='' setup_pid='' database_started=false
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  stop_tree "$dashboard_pid"
  stop_tree "$setup_pid"
  stop_tree "$api_pid"
  if $database_started; then pg_ctl -D "$state/postgres" -m fast -w stop >> "$state/setup.log" 2>&1 || status=1; fi
  [[ -z "$socket_dir" ]] || rm -rf "$socket_dir"
  rm -rf "$state/launcher.lock.d"
  echo 'Preview stopped. Saved data and credentials are preserved.'
  if [[ "$status" != 0 ]]; then echo 'See .local-preview/setup.log, api.log, and dashboard.log for details.' >&2; fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 0' INT
trap 'exit 143' TERM
run_setup() { "$@" >> "$state/setup.log" 2>&1 & setup_pid=$!; wait "$setup_pid"; setup_pid=''; }
if pg_ctl -D "$state/postgres" status >/dev/null 2>&1; then
  echo 'The preview database is already running. Stop its existing launcher first.' >&2; exit 1
fi
if [[ ! -f "$state/credentials.json" ]]; then
  jq -n --arg token "$(openssl rand -hex 32)" --arg password "$(openssl rand -hex 16)" --arg secret "$(openssl rand -hex 32)" \
    '{api_token:$token,password:$password,secret:$secret}' > "$state/credentials.json"
fi
jq -e '(.api_token | type=="string" and length>=32) and (.password | type=="string" and length>=16) and (.secret | type=="string" and length>=32)' "$state/credentials.json" >/dev/null
export HANDOFFGUARD_API_TOKEN HANDOFFGUARD_DASHBOARD_PASSWORD HANDOFFGUARD_DASHBOARD_SECRET HANDOFFGUARD_BINARY
HANDOFFGUARD_API_TOKEN="$(jq -r .api_token "$state/credentials.json")"
HANDOFFGUARD_DASHBOARD_PASSWORD="$(jq -r .password "$state/credentials.json")"
HANDOFFGUARD_DASHBOARD_SECRET="$(jq -r .secret "$state/credentials.json")"
HANDOFFGUARD_BINARY="$state/handoffguard"
export NEXT_TELEMETRY_DISABLED=1 NODE_ENV=development HANDOFFGUARD_LOCAL_PREVIEW=1
socket_dir="$(mktemp -d /tmp/hg-preview.XXXXXX)"
: > "$state/setup.log"
echo 'Preparing local preview…'
if [[ ! -f "$state/postgres/PG_VERSION" ]]; then
  run_setup initdb -D "$state/postgres" -U postgres --auth=trust --no-locale --encoding=UTF8
fi
run_setup pg_ctl -D "$state/postgres" -l "$state/postgres.log" -o "-h '' -k $socket_dir" -w start
database_started=true
export HANDOFFGUARD_DATABASE_URL="postgresql://postgres@/postgres?host=$socket_dir"
run_setup go build -o "$HANDOFFGUARD_BINARY" ./cmd/cli
if [[ ! -f "$state/server.priv" ]]; then run_setup "$HANDOFFGUARD_BINARY" keygen --out "$state/server"; fi
"$HANDOFFGUARD_BINARY" server --key "$state/server.priv" --addr 127.0.0.1:0 > "$state/api.log" 2>&1 &
api_pid=$!
export HANDOFFGUARD_SERVER_URL
HANDOFFGUARD_SERVER_URL="$(api_url_from_log "$api_pid" "$state/api.log")"
# Pass credentials via stdin, not curl's process arguments.
overview="$(printf 'header = "Authorization: Bearer %s"\n' "$HANDOFFGUARD_API_TOKEN" | curl --config - --fail --silent --show-error --max-time 20 "$HANDOFFGUARD_SERVER_URL/v1/dashboard/overview")"
if $seed && [[ "$(jq -r '.stats.runs' <<< "$overview")" == 0 ]]; then
  echo 'Adding a Java Support → Billing → Notification run…'
  run_setup mvn -B -f integrations/java-workflow/pom.xml -DskipTests package
  run_setup java -jar integrations/java-workflow/target/handoffguard-workflow.jar
fi
lock_hash="$(node -e 'const fs=require("node:fs"),crypto=require("node:crypto"); console.log(crypto.createHash("sha256").update(fs.readFileSync("dashboard/package-lock.json")).digest("hex"))')"
if [[ ! -d dashboard/node_modules/next || ! -f "$state/npm-lock-hash" || "$(cat "$state/npm-lock-hash")" != "$lock_hash" ]]; then
  echo 'Installing dashboard dependencies…'
  run_setup npm ci --prefix dashboard
  printf '%s' "$lock_hash" > "$state/npm-lock-hash"
fi
# Select a free loopback port, falling back if the preferred port is busy.
port="$(node - "$port" <<'JS'
const net=require('node:net'); const server=net.createServer();
server.once('error',()=>server.listen(0,'127.0.0.1'));
server.on('listening',()=>{console.log(server.address().port);server.close()});
server.listen(Number(process.argv[2]),'127.0.0.1');
JS
)"
url="http://localhost:$port"
echo 'Starting dashboard…'
npm run dev --prefix dashboard -- --hostname 127.0.0.1 --port "$port" > "$state/dashboard.log" 2>&1 &
dashboard_pid=$!
ready=false
for attempt in {1..600}; do
  kill -0 "$dashboard_pid" 2>/dev/null || exit 1
  if curl --fail --silent --max-time 1 "$url/login" >/dev/null; then ready=true; break; fi
  sleep .2
done
$ready || { echo 'Dashboard startup timed out' >&2; exit 1; }
if $check; then echo 'Startup check passed.'; exit 0; fi
printf '\nDashboard: %s\nPassword:  %s\n\nKeep this terminal open. Press Ctrl+C to stop.\n' "$url" "$HANDOFFGUARD_DASHBOARD_PASSWORD"
if $open_browser; then
  if command -v open >/dev/null; then open "$url"; elif command -v xdg-open >/dev/null; then xdg-open "$url" >/dev/null 2>&1 || true; fi
fi
while kill -0 "$api_pid" 2>/dev/null && kill -0 "$dashboard_pid" 2>/dev/null; do sleep 1; done
echo 'A preview service stopped.' >&2
exit 1
