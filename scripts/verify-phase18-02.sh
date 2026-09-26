#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ ${1:-} == --self-test && $# == 1 ]]; then
  exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
    python3 -m unittest "$ROOT/scripts/ci/test_phase18_02.py"
fi
exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
  python3 "$ROOT/scripts/ci/phase18_02.py" "$@"
