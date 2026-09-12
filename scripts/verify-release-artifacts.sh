#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ${1:-} == --self-test && $# == 1 ]]; then
  exec python3 -m unittest discover -s "$ROOT/scripts/ci" -p 'test_release_*.py'
fi
exec python3 "$ROOT/scripts/ci/verify_release_artifacts.py" "$@"
