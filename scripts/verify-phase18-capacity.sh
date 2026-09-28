#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ ${1:-} == --self-test && $# == 1 ]]; then
  (cd "$ROOT/loadtest" && go test ./...)
  PYTHONPATH="$ROOT/scripts/ci" python3 -m unittest \
    "$ROOT/scripts/ci/test_phase18_capacity.py" \
    "$ROOT/scripts/ci/test_phase18_evidence.py" \
    "$ROOT/scripts/ci/test_phase18_sampler.py"
  exit 0
fi
exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
  python3 "$ROOT/scripts/ci/phase18_capacity.py" "$@"
