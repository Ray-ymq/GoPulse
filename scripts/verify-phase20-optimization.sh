#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
task_pythonpath="$ROOT/scripts/ci"
if [[ -n ${PYTHONPATH:-} ]]; then
  task_pythonpath="$task_pythonpath:$PYTHONPATH"
fi
exec env PYTHONPATH="$task_pythonpath" python3 "$ROOT/scripts/ci/phase20_optimization.py" "$@"
