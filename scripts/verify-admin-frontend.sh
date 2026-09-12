#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
case "${1:-}" in
  --dashboard-alerts-users) exec python3 "$ROOT/scripts/ci/verify_dashboard.py" ;;
  --self-test|--existing-management) exec python3 "$ROOT/scripts/ci/verify_admin_frontend.py" "$1" ;;
  *) echo 'Usage: scripts/verify-admin-frontend.sh --self-test|--existing-management|--dashboard-alerts-users' >&2; exit 2 ;;
esac
