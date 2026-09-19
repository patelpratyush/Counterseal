#!/usr/bin/env bash
set -euo pipefail
exec bash "$(dirname "$0")/java-demo.sh" --scenario=gateway "$@"
