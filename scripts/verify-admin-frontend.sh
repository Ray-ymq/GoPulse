#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
case "${1:-}" in
  --self-test|--existing-management) exec python3 "$ROOT/scripts/ci/verify_admin_frontend.py" "$1" ;;
  *) echo 'Usage: scripts/verify-admin-frontend.sh --self-test|--existing-management' >&2; exit 2 ;;
esac
