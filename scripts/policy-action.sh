#!/usr/bin/env bash
# Treat all policy paths as data; never interpolate inputs into executable code.
set -euo pipefail
command -v jq >/dev/null
cd "${GITHUB_WORKSPACE:-.}"
report="$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/handoffguard-policy.XXXXXX")"
stderr="$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/handoffguard-error.XXXXXX")"
trap 'rm -f "$stderr"' EXIT
error_report() { jq -n --arg error "$1" '{decision:"ERROR",error:$error,checks:"Policy check could not complete"}' > "$report"; }
if [[ -z "${HG_PARENT:-}" || -z "${HG_CHILD:-}" || -z "${HG_BINARY:-}" ]]; then
  error_report 'Both envelope paths and the checker binary are required'
else
  args=(policy diff "$HG_PARENT" "$HG_CHILD" --format json)
  [[ -z "${HG_PARENT_KEY:-}" ]] || args+=(--parent-key "$HG_PARENT_KEY")
  [[ -z "${HG_CHILD_KEY:-}" ]] || args+=(--child-key "$HG_CHILD_KEY")
  status=0
  "$HG_BINARY" "${args[@]}" > "$report" 2> "$stderr" || status=$?
  if ! jq -e 'type == "object" and (.decision == "ALLOW" or .decision == "DENY") and (.violations | type == "array")' "$report" >/dev/null 2>&1; then
    error_report "$(cat "$stderr")"
  elif [[ "$(jq -r .decision "$report")" == ALLOW ]] && { [[ "$status" != 0 ]] || ! jq -e '.violations | length == 0' "$report" >/dev/null; }; then
    error_report 'Policy checker returned an inconsistent ALLOW'
  fi
fi
decision="$(jq -r .decision "$report")"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then printf 'decision=%s\nreport=%s\n' "$decision" "$report" >> "$GITHUB_OUTPUT"; fi
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    printf '## Counterseal: %s\n\n' "$decision"
    printf 'Checks delegated policy content; signatures are checked only when both public keys are supplied.\n\n<pre>'
    jq . "$report" | jq -Rrs '@html'
    printf '</pre>\n'
  } >> "$GITHUB_STEP_SUMMARY"
fi
printf 'Counterseal decision: %s\n' "$decision"
if [[ "$decision" != ALLOW ]]; then
  echo '::error title=Counterseal policy check::Delegation denied or check failed. See the job summary.'
  exit 1
fi
