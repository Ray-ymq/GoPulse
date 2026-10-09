#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
[[ $# == 2 && $1 == --candidate && $2 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Usage: scripts/verify-runtime-contracts.sh --candidate x.x.x' >&2; exit 2; }
mkdir -p "$ROOT/.run/phase18-05"
for source in redis mysql rabbitmq kafka elasticsearch victoriametrics; do
  (cd "$ROOT/exporters/$source" && go test -count=1 ./... && go test -race -count=1 ./...) >"$ROOT/.run/phase18-05/$source-exporter-gates.log" 2>&1
  printf 'PASS: %s exporter package and race gates\n' "$source"
done
python3 -m unittest discover -s "$ROOT/scripts/ci" -p test_runtime_contracts.py
python3 "$ROOT/ci/verify_runtime_contracts.py" \
  --contract "$ROOT/deploy/runtime-contracts.json" \
  --compose "$ROOT/deploy/compose.yaml" \
  --env "$ROOT/.env.example" \
  --candidate "$2"
exec python3 "$ROOT/ci/runtime_acceptance.py" --candidate "$2"
