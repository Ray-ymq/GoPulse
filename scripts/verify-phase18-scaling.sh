#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ ${1:-} == --self-test && $# == 1 ]]; then
  (cd "$ROOT/loadtest" && go test ./...)
  (cd "$ROOT/backend" && go test -count=1 ./internal/outbox ./internal/alert ./internal/worker ./internal/http)
  (cd "$ROOT/marshaller" && go test -count=1 ./internal/consumer ./internal/envelope)
  (cd "$ROOT/monitor" && go test -count=1 ./internal/plugin ./internal/metrics/collector)
  PYTHONPATH="$ROOT/scripts/ci" python3 -m unittest \
    "$ROOT/scripts/ci/test_phase18_scaling.py" \
    "$ROOT/scripts/ci/test_phase18_evidence.py" \
    "$ROOT/scripts/ci/test_phase18_sampler.py" \
    "$ROOT/scripts/ci/test_phase18_qualification.py"
  exit 0
fi
exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" \
  python3 "$ROOT/scripts/ci/phase18_scaling.py" "$@"
