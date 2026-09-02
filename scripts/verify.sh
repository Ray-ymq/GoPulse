#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
ENV_FILE="$REPO_ROOT/.env"
WORKER_RECORD="$REPO_ROOT/.run/business-worker.json"
WORKER_BINARY="$REPO_ROOT/.run/bin/gopulse-business-worker"
BACKEND_DIR="$REPO_ROOT/backend"
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

check_worker_process() {
  local result
  if [[ ! -f "$WORKER_RECORD" ]]; then
    fail 'Business Worker' "process record is missing: $WORKER_RECORD"
    return
  fi
  if ! result=$(python3 - "$WORKER_RECORD" "$BACKEND_DIR" "$WORKER_BINARY" <<'PY'
import json
import os
import sys
path, expected_cwd, expected_executable = sys.argv[1:]
try:
    record = json.load(open(path, encoding='utf-8'))
    pid = int(record['pid'])
    start_ticks = str(record['startTicks'])
    executable = os.path.realpath(str(record['executablePath']))
    cwd = os.path.realpath(str(record['workingDirectory']))
    marker = str(record['commandLineMarker'])
except Exception:
    print('record is malformed')
    raise SystemExit(1)
if cwd != os.path.realpath(expected_cwd) or marker != expected_executable:
    print('record identity does not match this repository')
    raise SystemExit(1)
if executable != os.path.realpath(expected_executable):
    print('recorded executable does not match the expected worker')
    raise SystemExit(1)
try:
    stat = open(f'/proc/{pid}/stat', encoding='utf-8').read().strip()
    fields = stat[stat.rfind(')') + 2:].split()
    actual_start_ticks = fields[19]
    actual_executable = os.path.realpath(f'/proc/{pid}/exe')
    command_line = open(f'/proc/{pid}/cmdline', 'rb').read().replace(b'\0', b' ').decode(errors='replace')
except Exception:
    print('recorded process is not running')
    raise SystemExit(1)
if actual_start_ticks != start_ticks or actual_executable != executable or marker not in command_line:
    print('running process identity does not match the record')
    raise SystemExit(1)
print(pid)
PY
  ); then
    fail 'Business Worker' "$result"
    return
  fi
  pass 'Business Worker' "PID $result matches its repository-owned process record."
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

check_protected_api() {
  local port=$1 body="$TEMP_DIR/protected-api.json" status
  if ! status=$(http_get "http://localhost:$port/api/v1/posts" "$body"); then
    fail 'Protected API' 'request failed or exceeded 5 seconds.'
    return
  fi
  if [[ $status != 401 ]]; then
    fail 'Protected API' "returned HTTP $status, expected unauthenticated HTTP 401."
    return
  fi
  if ! python3 - "$body" <<'PYAPI'
import json
import sys
try:
    value = json.load(open(sys.argv[1], encoding='utf-8'))
except Exception:
    raise SystemExit(1)
error = value.get('error') if isinstance(value, dict) else None
valid = isinstance(error, dict) and error.get('code') == 'authentication_required'
raise SystemExit(0 if valid else 1)
PYAPI
  then
    fail 'Protected API' 'JSON contract mismatch (expected authentication_required).'
    return
  fi
  pass 'Protected API' 'unauthenticated post listing returned the expected HTTP 401 contract.'
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
  check_worker_process
  check_health "$port"
  check_ready "$port"
  check_protected_api "$port"
  check_frontend
  if ((FAILURES > 0)); then
    printf '[gopulse] Verification failed with %d issue(s). The script did not change the running environment.\n' "$FAILURES" >&2
    printf '[gopulse] Diagnose Compose with: docker compose --project-name gopulse --file deploy/compose.yaml ps\n' >&2
    return 1
  fi
  info 'Verification passed. The script did not change the running environment.'
}

main "$@"
