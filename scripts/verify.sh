#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
ENV_FILE="$REPO_ROOT/.env"
PROJECT_NAME=gopulse
FAILURES=0
TEMP_DIR=

info() {
  printf '[gopulse] %s\n' "$*"
}

pass() {
  printf '[gopulse] PASS %s - %s\n' "$1" "$2"
}

fail() {
  printf '[gopulse] FAIL %s - %s\n' "$1" "$2" >&2
  FAILURES=$((FAILURES + 1))
}

cleanup() {
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
}
trap cleanup EXIT

require_tools() {
  local missing=() tool
  for tool in docker curl python3; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  if ((${#missing[@]} > 0)); then
    printf '[gopulse] ERROR: Missing required tool(s): %s.\n' "${missing[*]}" >&2
    return 1
  fi
  docker compose version >/dev/null 2>&1 || { printf '[gopulse] ERROR: Docker Compose is unavailable.\n' >&2; return 1; }
  docker info >/dev/null 2>&1 || { printf '[gopulse] ERROR: Docker is installed, but the Docker daemon is unavailable.\n' >&2; return 1; }
}

http_port() {
  if [[ -n ${HTTP_PORT:-} ]]; then
    printf '%s\n' "$HTTP_PORT"
    return
  fi
  python3 - "$ENV_FILE" <<'PY'
import re
import sys
path = sys.argv[1]
value = None
try:
    lines = open(path, encoding='utf-8').read().splitlines()
except FileNotFoundError:
    lines = []
for number, raw in enumerate(lines, 1):
    line = raw.strip()
    if not line or line.startswith('#'):
        continue
    if re.match(r'^export(?:\s|$)', line):
        raise SystemExit(f'Unsupported dotenv syntax at line {number}: export is not allowed.')
    match = re.match(r'^([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$', line)
    if not match:
        raise SystemExit(f'Invalid dotenv assignment at line {number}.')
    if match.group(1) != 'HTTP_PORT':
        continue
    value = match.group(2).strip()
    if value[:1] in ("'", '"'):
        if len(value) < 2 or value[-1] != value[0]:
            raise SystemExit(f'Unterminated quoted dotenv value for HTTP_PORT at line {number}.')
        value = value[1:-1]
    elif "'" in value or '"' in value:
        raise SystemExit(f'Mismatched quote in dotenv value for HTTP_PORT at line {number}.')
    break
print(value or '8080')
PY
}

validate_port() {
  [[ $1 =~ ^[0-9]+$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535))
}

check_compose_service() {
  local service=$1 ids state count
  ids=$(docker ps -a --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service" --format '{{.ID}}' 2>/dev/null) || {
    fail "Compose/$service" 'Docker could not list the service container.'
    return
  }
  count=$(sed '/^[[:space:]]*$/d' <<<"$ids" | wc -l | tr -d ' ')
  if [[ $count != 1 ]]; then
    fail "Compose/$service" "expected exactly one container, found $count. Inspect with: docker compose --project-name gopulse --file '$REPO_ROOT/deploy/compose.yaml' ps"
    return
  fi
  state=$(docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$ids" 2>/dev/null) || {
    fail "Compose/$service" "could not inspect container $ids."
    return
  }
  if [[ $state != 'running|healthy' ]]; then
    fail "Compose/$service" "container state is '$state', expected 'running|healthy'."
    return
  fi
  pass "Compose/$service" 'container is running and healthy.'
}

http_get() {
  local url=$1 body_file=$2
  curl --silent --show-error --location --max-time 5 --output "$body_file" --write-out '%{http_code}' "$url"
}

check_health() {
  local port=$1 body="$TEMP_DIR/health.json" status
  if ! status=$(http_get "http://localhost:$port/health" "$body"); then
    fail '/health' 'request failed or exceeded 5 seconds.'
    return
  fi
  if [[ $status != 200 ]]; then
    fail '/health' "returned HTTP $status, expected 200."
    return
  fi
  if ! python3 - "$body" <<'PY'
import json
import sys
try:
    value = json.load(open(sys.argv[1], encoding='utf-8'))
except Exception:
    raise SystemExit(1)
raise SystemExit(0 if isinstance(value, dict) and value.get('status') == 'ok' and value.get('service') == 'backend' else 1)
PY
  then
    fail '/health' 'JSON contract mismatch (expected status=ok and service=backend).'
    return
  fi
  pass '/health' 'HTTP 200 with the expected JSON contract.'
}

check_ready() {
  local port=$1 body="$TEMP_DIR/ready.json" status
  if ! status=$(http_get "http://localhost:$port/ready" "$body"); then
    fail '/ready' 'request failed or exceeded 5 seconds.'
    return
  fi
  if [[ $status != 200 ]]; then
    fail '/ready' "returned HTTP $status, expected 200."
    return
  fi
  if ! python3 - "$body" <<'PY'
import json
import sys
try:
    value = json.load(open(sys.argv[1], encoding='utf-8'))
except Exception:
    raise SystemExit(1)
checks = value.get('checks') if isinstance(value, dict) else None
valid = (
    value.get('status') == 'ready'
    and value.get('service') == 'backend'
    and isinstance(checks, dict)
    and checks.get('mysql') == 'up'
    and checks.get('redis') == 'up'
    and checks.get('rabbitmq') == 'up'
)
raise SystemExit(0 if valid else 1)
PY
  then
    fail '/ready' 'JSON contract mismatch (expected ready backend with mysql, redis, and rabbitmq up).'
    return
  fi
  pass '/ready' 'HTTP 200 with all dependency checks up.'
}

check_frontend() {
  local body="$TEMP_DIR/frontend.html" status
  if ! status=$(http_get 'http://localhost:5173/' "$body"); then
    fail 'Frontend' 'request failed or exceeded 5 seconds.'
    return
  fi
  if [[ ! $status =~ ^2[0-9][0-9]$ ]]; then
    fail 'Frontend' "returned HTTP $status, expected a 2xx response."
    return
  fi
  pass 'Frontend' "HTTP $status from http://localhost:5173/."
}

main() {
  require_tools || return 1
  local port
  if ! port=$(http_port); then
    printf '[gopulse] ERROR: Could not read HTTP_PORT from the environment file.\n' >&2
    return 1
  fi
  validate_port "$port" || { printf "[gopulse] ERROR: HTTP_PORT must be an integer from 1 to 65535; received '%s'.\n" "$port" >&2; return 1; }
  TEMP_DIR=$(mktemp -d)
  info "Verifying the running environment from $REPO_ROOT."
  check_compose_service mysql
  check_compose_service redis
  check_compose_service rabbitmq
  check_health "$port"
  check_ready "$port"
  check_frontend
  if ((FAILURES > 0)); then
    printf '[gopulse] Verification failed with %d issue(s). The script did not change the running environment.\n' "$FAILURES" >&2
    printf '[gopulse] Diagnose Compose with: docker compose --project-name gopulse --file deploy/compose.yaml ps\n' >&2
    return 1
  fi
  info 'Verification passed. The script did not change the running environment.'
}

main "$@"
