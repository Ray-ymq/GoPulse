#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
for app in frontend admin-frontend; do
  (cd "$ROOT/$app" && npm test && npm run build)
done
