#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
if [[ "$#" -eq 0 ]]; then
  exec python3 "$ROOT/ci/verify_component_metrics.py"
fi
if [[ "$1" == "--self-test" ]]; then
  python3 "$ROOT/ci/verify_plugin_metrics.py" "$@"
  exec python3 "$ROOT/ci/verify_component_metrics.py" "$@"
fi
exec python3 "$ROOT/ci/verify_plugin_metrics.py" "$@"
