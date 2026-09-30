#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
exec env PYTHONPATH="$ROOT/scripts/ci${PYTHONPATH:+:$PYTHONPATH}" python3 "$ROOT/scripts/ci/phase20_diagnostic.py" "$@"
