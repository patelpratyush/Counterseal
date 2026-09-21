#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
command -v docker >/dev/null || { echo 'Install Docker with Compose first.' >&2; exit 1; }
env_file=.env.compose
command="${1:-up}"
if [[ $# -gt 0 ]]; then shift; fi
case "$command" in
  up|demo|down|logs|status) ;;
  --help|-h) echo 'Usage: ./compose.sh [up|demo [JAVA_OPTIONS...]|down|logs|status]'; exit 0 ;;
  *) echo 'Usage: ./compose.sh [up|demo [JAVA_OPTIONS...]|down|logs|status]' >&2; exit 2 ;;
esac
if [[ ! -e "$env_file" ]]; then
  [[ "$command" == up ]] || { echo 'Run ./compose.sh up first.' >&2; exit 1; }
  command -v openssl >/dev/null
  umask 077
  # Noclobber protects credentials if two launchers start simultaneously.
  (set -o noclobber
    printf 'HG_POSTGRES_PASSWORD=%s\nHG_API_TOKEN=%s\nHG_DASHBOARD_PASSWORD=%s\nHG_DASHBOARD_SECRET=%s\nHG_DASHBOARD_PORT=3100\n' \
      "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" "$(openssl rand -hex 16)" "$(openssl rand -hex 32)" > "$env_file")
fi
compose=(docker compose --env-file "$env_file")
case "$command" in
  up)
    "${compose[@]}" up --build --detach --wait --wait-timeout 180
    port="$(sed -n 's/^HG_DASHBOARD_PORT=//p' "$env_file")"
    password="$(sed -n 's/^HG_DASHBOARD_PASSWORD=//p' "$env_file")"
    printf '\nDashboard: http://localhost:%s\nUsername: operator\nInitial password: %s\n\nAdd a simulated Java workflow: ./compose.sh demo\nStop and preserve data: ./compose.sh down\n' "${port:-3100}" "$password"
    ;;
  demo) "${compose[@]}" run --build --rm demo "$@" ;;
  down) "${compose[@]}" down ;;
  logs) "${compose[@]}" logs --follow --tail 100 ;;
  status) "${compose[@]}" ps ;;
esac
