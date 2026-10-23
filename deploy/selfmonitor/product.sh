#!/usr/bin/env bash
# Fixed current-host product project; secrets stay in the repository .env.
set -euo pipefail
selfmonitor_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_dir=$(cd -- "$selfmonitor_dir/../.." && pwd)
SELFMONITOR_SOCKET_GID=$(getent group gopulse-selfmonitor | cut -d: -f3)
if [[ -z "$SELFMONITOR_SOCKET_GID" ]]; then
  echo 'Install the gopulse-selfmonitor group and control socket first (see README).' >&2
  exit 1
fi
export SELFMONITOR_SOCKET_GID
exec docker compose --project-name gopulse \
  --env-file "$repo_dir/.env.example" --env-file "$repo_dir/.env" \
  --env-file "$selfmonitor_dir/selfmonitor.env" \
  --file "$repo_dir/deploy/compose.yaml" \
  --file "$selfmonitor_dir/product.override.yaml" "$@"
