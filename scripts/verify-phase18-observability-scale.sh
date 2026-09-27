#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ $# -ne 2 || $1 != --repetitions || $2 != 2 ]]; then
  echo 'Usage: scripts/verify-phase18-observability-scale.sh --repetitions 2' >&2
  exit 2
fi

exec python3 "$ROOT/scripts/ci/phase18_observability_scale.py" --repetitions 2
