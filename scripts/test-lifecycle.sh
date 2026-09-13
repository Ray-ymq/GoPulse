#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
(cd "$ROOT/lifecycle" && go test ./...)
"$ROOT/scripts/verify-compose.sh" --self-test
