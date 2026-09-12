#!/usr/bin/env bash
# Compatibility entry point: management now belongs to the independent application.
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
case "${1:-}" in
  --self-test) exec bash "$ROOT/scripts/verify-admin-frontend.sh" --self-test ;;
  '') exec bash "$ROOT/scripts/verify-admin-frontend.sh" --existing-management ;;
  *) echo 'Usage: scripts/verify-observability-ui.sh [--self-test]' >&2; exit 2 ;;
esac
