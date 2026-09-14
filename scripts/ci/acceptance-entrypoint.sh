#!/bin/sh
set -eu
if [ "${1:-}" = phase16 ]; then
    shift
    exec python3 /work/scripts/ci/phase16_acceptance.py "$@"
fi
exec npx playwright test "$@"
