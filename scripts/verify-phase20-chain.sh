#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
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
  echo 'Usage: scripts/verify-phase20-chain.sh --manifest <candidate manifest> --work <evidence directory>' >&2
  exit 2
fi
[[ -f "$MANIFEST" ]] || { echo "candidate manifest is missing: $MANIFEST" >&2; exit 1; }
[[ -d "$WORK" ]] || { echo "evidence directory is missing: $WORK" >&2; exit 1; }

if [[ ! -e "$WORK/candidate.json" || "$(realpath "$MANIFEST")" != "$(realpath "$WORK/candidate.json")" ]]; then
  cp "$MANIFEST" "$WORK/candidate.json"
fi

exec python3 "$ROOT/scripts/ci/phase20_chain.py" --manifest "$MANIFEST" --work "$WORK"
