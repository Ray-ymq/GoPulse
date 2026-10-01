#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
MANIFEST=
WORK=
while (($#)); do
  case "$1" in
    --manifest) MANIFEST=${2:?missing manifest path}; shift 2 ;;
    --work) WORK=${2:?missing work directory}; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [[ -z "$MANIFEST" || -z "$WORK" ]]; then
  echo 'Usage: scripts/verify-phase20-retention.sh --manifest <candidate manifest> --work <new evidence directory>' >&2
  exit 2
fi
[[ -f "$MANIFEST" ]] || { echo "candidate manifest is missing: $MANIFEST" >&2; exit 1; }
[[ ! -e "$WORK" ]] || { echo "refusing to overwrite evidence directory: $WORK" >&2; exit 1; }
exec python3 "$ROOT/scripts/ci/phase20_retention.py" --manifest "$MANIFEST" --work "$WORK"
