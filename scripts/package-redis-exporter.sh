#!/usr/bin/env bash
# Thin entry point kept for the acceptance scripts that still call this path.
# The implementation lives in monitor/cmd/plugin-package; the callers retire with
# the rest of scripts/, and this forwarder is deleted with them.
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BINARY="$REPO_ROOT/.run/bin/plugin-package"
mkdir -p "$(dirname "$BINARY")"
cd "$REPO_ROOT/monitor"
# Build then replace, so the executable keeps its own exit status (usage errors
# report 2) and concurrent callers never execute a partial binary.
go build -o "$BINARY.$$" ./cmd/plugin-package
mv -f "$BINARY.$$" "$BINARY"
exec "$BINARY" --repo-root "$REPO_ROOT" "$@"
