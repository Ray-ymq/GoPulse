#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT/lifecycle"
go test ./internal/backup ./internal/control -run 'Test(AuthenticatedRoundTrip|ArchiveBoundary|ExportContract|PrivatePublication|PrivatePassphraseSource|BackupInspect)' -count=1
