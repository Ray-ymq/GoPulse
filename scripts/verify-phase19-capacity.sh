#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

if [[ ${1:-} == --self-test && $# == 1 ]]; then
  (cd "$ROOT/loadtest" && go test -count=1 ./...)
  PYTHONPATH="$ROOT/scripts/ci" python3 -m unittest discover -s "$ROOT/scripts/ci" -p 'test_phase19_*.py'
  python3 -m py_compile "$ROOT/scripts/ci/phase19_capacity.py" "$ROOT/scripts/ci/phase19_sampler.py" "$ROOT/scripts/ci/phase19_evidence.py"
  exit 0
fi

if [[ ${1:-} == --calibration && $# == 1 ]]; then
  exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
    python3 "$ROOT/scripts/ci/phase19_capacity.py" --calibration
fi

exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
  python3 "$ROOT/scripts/ci/phase19_capacity.py" "$@"
