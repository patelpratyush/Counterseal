#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
require_tools go jq
work="$(mktemp -d /tmp/hg-action-test.XXXXXX)"
trap 'rm -rf "$work"' EXIT
go build -o "$work/handoffguard" ./cmd/cli
export HG_BINARY="$work/handoffguard" RUNNER_TEMP="$work" GITHUB_WORKSPACE="$HG_ROOT"
export GITHUB_OUTPUT="$work/output" GITHUB_STEP_SUMMARY="$work/summary"
export HG_PARENT=examples/policy-parent.json HG_CHILD=examples/policy-child-allow.json HG_PARENT_KEY='' HG_CHILD_KEY=''
check() {
  local expected="$1" status=0
  : > "$GITHUB_OUTPUT"; : > "$GITHUB_STEP_SUMMARY"
  bash scripts/policy-action.sh > "$work/log" || status=$?
  grep -qx "decision=$expected" "$GITHUB_OUTPUT"
  if [[ "$expected" == ALLOW ]]; then test "$status" = 0; else test "$status" != 0; fi
}
check ALLOW
HG_CHILD=examples/policy-child-deny.json check DENY
report="$(sed -n 's/^report=//p' "$GITHUB_OUTPUT")"
jq -e 'any(.violations[]; .code=="ACTION_EXPANDED") and any(.violations[]; .code=="APPROVAL_WEAKENED")' "$report" >/dev/null
HG_PARENT='' check ERROR
HG_PARENT=missing.json check ERROR
HG_PARENT_KEY=missing.pub check ERROR
literal="$work/\$(touch SHOULD_NOT_EXIST).json"
cp "$HG_CHILD" "$literal"
HG_CHILD="$literal" check ALLOW
[[ ! -e SHOULD_NOT_EXIST ]]
jq '.allowed_actions += ["<script>alert(1)</script>"]' "$HG_CHILD" > "$work/unsafe.json"
HG_CHILD="$work/unsafe.json" check DENY
! grep -q '<script>' "$GITHUB_STEP_SUMMARY"
"$HG_BINARY" keygen --out "$work/key" >/dev/null
for name in parent child; do
  fixture=examples/policy-parent.json
  [[ "$name" != child ]] || fixture=examples/policy-child-allow.json
  "$HG_BINARY" envelope create --in "$fixture" --key "$work/key.priv" --out "$work/$name.json" >/dev/null
done
HG_PARENT="$work/parent.json" HG_CHILD="$work/child.json" HG_PARENT_KEY="$work/key.pub" HG_CHILD_KEY="$work/key.pub" check ALLOW
jq '.purpose="tampered"' "$work/child.json" > "$work/tampered.json"
HG_PARENT="$work/parent.json" HG_CHILD="$work/tampered.json" HG_PARENT_KEY="$work/key.pub" HG_CHILD_KEY="$work/key.pub" check DENY
echo 'PASS: policy narrowing, expansion, invalid inputs, literal paths, escaped summary, signatures and tampering'
