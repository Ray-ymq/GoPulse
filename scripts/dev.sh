#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
BACKEND_DIR="$REPO_ROOT/backend"
FRONTEND_DIR="$REPO_ROOT/frontend"
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
ENV_FILE="$REPO_ROOT/.env"
ENV_EXAMPLE_FILE="$REPO_ROOT/.env.example"
RUN_DIR="$REPO_ROOT/.run"
BIN_DIR="$RUN_DIR/bin"
LOCK_PATH="$RUN_DIR/dev.lock"
BACKEND_RECORD="$RUN_DIR/backend.json"
WORKER_RECORD="$RUN_DIR/business-worker.json"
FRONTEND_RECORD="$RUN_DIR/frontend.json"
BACKEND_BINARY="$BIN_DIR/gopulse-backend"
WORKER_BINARY="$BIN_DIR/gopulse-business-worker"
VITE_CLI="$FRONTEND_DIR/node_modules/vite/bin/vite.js"
VITE_CONFIG="$FRONTEND_DIR/vite.config.ts"
PROJECT_NAME=gopulse
LOCK_FD=9
LOCK_OWNED=0
LOCK_TOKEN=
BACKEND_PID=
WORKER_PID=
FRONTEND_PID=
BACKEND_STARTED=0
WORKER_STARTED=0
FRONTEND_STARTED=0
EXIT_CODE=0

COMPOSE_KEYS=(
  PUBLISHED_HOST MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PORT
  REDIS_PASSWORD REDIS_PORT RABBITMQ_USER RABBITMQ_PASSWORD
  RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT ELASTICSEARCH_PORT
)
BACKEND_KEYS=(
  APP_ENV HTTP_HOST HTTP_PORT MYSQL_HOST MYSQL_PORT MYSQL_DATABASE MYSQL_USER
  MYSQL_PASSWORD REDIS_HOST REDIS_PORT REDIS_PASSWORD REDIS_DB RABBITMQ_URL
  AUTH_JWT_SECRET AUTH_JWT_TTL AUTH_COOKIE_NAME AUTH_COOKIE_SECURE
  REDIS_POST_DETAIL_TTL REDIS_OPERATION_TIMEOUT
  ELASTICSEARCH_URL ELASTICSEARCH_REQUEST_TIMEOUT SEARCH_REINDEX_BATCH
  OUTBOX_POLL_INTERVAL OUTBOX_CLAIM_BATCH OUTBOX_LEASE_DURATION OUTBOX_PUBLISH_TIMEOUT OUTBOX_RETRY_DELAY
  OUTBOX_CLEANUP_INTERVAL OUTBOX_PUBLISHED_RETENTION OUTBOX_CLEANUP_BATCH
)
WORKER_KEYS=(
  MYSQL_HOST MYSQL_PORT MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD RABBITMQ_URL OUTBOX_RETRY_DELAY
  BUSINESS_WORKER_PREFETCH BUSINESS_WORKER_MAX_RETRIES BUSINESS_WORKER_PUBLISH_TIMEOUT
  BUSINESS_WORKER_SHUTDOWN_TIMEOUT BUSINESS_WORKER_RECONNECT_MIN BUSINESS_WORKER_RECONNECT_MAX
)
ALL_CONFIG_KEYS=(
  PUBLISHED_HOST APP_ENV HTTP_HOST HTTP_PORT MYSQL_HOST MYSQL_PORT MYSQL_DATABASE MYSQL_USER
  MYSQL_PASSWORD MYSQL_ROOT_PASSWORD REDIS_HOST REDIS_PORT REDIS_PASSWORD REDIS_DB
  RABBITMQ_USER RABBITMQ_PASSWORD RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT RABBITMQ_URL
  AUTH_JWT_SECRET AUTH_JWT_TTL AUTH_COOKIE_NAME AUTH_COOKIE_SECURE
  REDIS_POST_DETAIL_TTL REDIS_OPERATION_TIMEOUT
  ELASTICSEARCH_URL ELASTICSEARCH_REQUEST_TIMEOUT SEARCH_REINDEX_BATCH
  OUTBOX_POLL_INTERVAL OUTBOX_CLAIM_BATCH OUTBOX_LEASE_DURATION OUTBOX_PUBLISH_TIMEOUT OUTBOX_RETRY_DELAY
  OUTBOX_CLEANUP_INTERVAL OUTBOX_PUBLISHED_RETENTION OUTBOX_CLEANUP_BATCH
  BUSINESS_WORKER_PREFETCH BUSINESS_WORKER_MAX_RETRIES BUSINESS_WORKER_PUBLISH_TIMEOUT
  BUSINESS_WORKER_SHUTDOWN_TIMEOUT BUSINESS_WORKER_RECONNECT_MIN BUSINESS_WORKER_RECONNECT_MAX
)
REQUIRED_KEYS=(
  MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD MYSQL_ROOT_PASSWORD REDIS_PASSWORD
  RABBITMQ_USER RABBITMQ_PASSWORD RABBITMQ_URL AUTH_JWT_SECRET
)
declare -A CALLER_ENV=()
declare -A DOTENV=()
declare -A CONFIG=()
declare -A DEFAULTS=(
  [APP_ENV]=development [PUBLISHED_HOST]=127.0.0.1 [HTTP_HOST]=127.0.0.1 [HTTP_PORT]=8080
  [MYSQL_HOST]=127.0.0.1 [MYSQL_PORT]=3306
  [REDIS_HOST]=127.0.0.1 [REDIS_PORT]=6379 [REDIS_DB]=0
  [RABBITMQ_PORT]=5672 [RABBITMQ_MANAGEMENT_PORT]=15672 [ELASTICSEARCH_PORT]=9200
  [AUTH_JWT_TTL]=2h [AUTH_COOKIE_NAME]=gopulse_session [AUTH_COOKIE_SECURE]=false
  [REDIS_POST_DETAIL_TTL]=5m [REDIS_OPERATION_TIMEOUT]=200ms
  [ELASTICSEARCH_URL]=http://127.0.0.1:9200 [ELASTICSEARCH_REQUEST_TIMEOUT]=3s [SEARCH_REINDEX_BATCH]=500
  [OUTBOX_POLL_INTERVAL]=1s [OUTBOX_CLAIM_BATCH]=10 [OUTBOX_LEASE_DURATION]=1m
  [OUTBOX_PUBLISH_TIMEOUT]=5s [OUTBOX_RETRY_DELAY]=30s
  [OUTBOX_CLEANUP_INTERVAL]=1h [OUTBOX_PUBLISHED_RETENTION]=168h [OUTBOX_CLEANUP_BATCH]=500
  [BUSINESS_WORKER_PREFETCH]=10 [BUSINESS_WORKER_MAX_RETRIES]=3
  [BUSINESS_WORKER_PUBLISH_TIMEOUT]=5s [BUSINESS_WORKER_SHUTDOWN_TIMEOUT]=10s
  [BUSINESS_WORKER_RECONNECT_MIN]=500ms [BUSINESS_WORKER_RECONNECT_MAX]=30s
)
while IFS='=' read -r key value; do
  CALLER_ENV["$key"]=$value
done < <(env)

info() {
  printf '[gopulse] %s\n' "$*"
}

fail() {
  printf '[gopulse] ERROR: %s\n' "$*" >&2
  return 1
}

json_string() {
  python3 - "$1" <<'PY'
import json
import sys
print(json.dumps(sys.argv[1]))
PY
}

require_tools() {
  local missing=() tool
  for tool in go node npm docker python3 ps flock sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  if ((${#missing[@]} > 0)); then
    fail "Missing required tool(s): ${missing[*]}."
    return 1
  fi
  if ! docker compose version >/dev/null 2>&1; then
    fail 'Docker Compose is unavailable. Install a Docker CLI with the Compose plugin.'
    return 1
  fi
  if ! docker info >/dev/null 2>&1; then
    fail 'Docker is installed, but the Docker daemon is unavailable.'
    return 1
  fi
  info 'Required tools are available.'
}

read_dotenv() {
  local path=$1 raw line key value quote line_number=0
  DOTENV=()
  [[ -f "$path" ]] || { fail "Environment file not found: $path"; return 1; }
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    ((line_number += 1))
    raw=${raw%$'\r'}
    line=${raw#"${raw%%[![:space:]]*}"}
    line=${line%"${line##*[![:space:]]}"}
    [[ -z "$line" || ${line:0:1} == '#' ]] && continue
    if [[ $line =~ ^export([[:space:]]|$) ]]; then
      fail "Unsupported dotenv syntax at line $line_number: export is not allowed."
      return 1
    fi
    if [[ ! $line =~ ^([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
      fail "Invalid dotenv assignment at line $line_number."
      return 1
    fi
    key=${BASH_REMATCH[1]}
    value=${BASH_REMATCH[2]}
    value=${value#"${value%%[![:space:]]*}"}
    value=${value%"${value##*[![:space:]]}"}
    if [[ -n "$value" && (${value:0:1} == "'" || ${value:0:1} == '"') ]]; then
      quote=${value:0:1}
      if ((${#value} < 2)) || [[ ${value: -1} != "$quote" ]]; then
        fail "Unterminated quoted dotenv value for $key at line $line_number."
        return 1
      fi
      value=${value:1:${#value}-2}
      if [[ $value == *"$quote"* ]]; then
        fail "Embedded quote syntax is not supported for $key at line $line_number."
        return 1
      fi
    elif [[ $value == *"'" || $value == *'"' ]]; then
      fail "Mismatched quote in dotenv value for $key at line $line_number."
      return 1
    fi
    [[ $value != *$'\n'* && $value != *$'\r'* ]] || { fail "Multiline dotenv values are not supported for $key at line $line_number."; return 1; }
    DOTENV["$key"]=$value
  done < "$path"
}

resolve_configuration() {
  local key value port redis_db
  CONFIG=()
  for key in "${ALL_CONFIG_KEYS[@]}"; do
    if [[ -v CALLER_ENV[$key] ]]; then
      CONFIG["$key"]=${CALLER_ENV[$key]}
    elif [[ -v DOTENV[$key] ]]; then
      CONFIG["$key"]=${DOTENV[$key]}
    elif [[ -v DEFAULTS[$key] ]]; then
      CONFIG["$key"]=${DEFAULTS[$key]}
    fi
  done
  for key in "${REQUIRED_KEYS[@]}"; do
    if [[ ! -v CONFIG[$key] || -z ${CONFIG[$key]} ]]; then
      fail "Required configuration $key is missing."
      return 1
    fi
  done
  for key in HTTP_PORT MYSQL_PORT REDIS_PORT RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT ELASTICSEARCH_PORT; do
    value=${CONFIG[$key]-}
    if [[ ! $value =~ ^[0-9]+$ ]] || ((10#$value < 1 || 10#$value > 65535)); then
      fail "$key must be an integer between 1 and 65535."
      return 1
    fi
    CONFIG["$key"]=$((10#$value))
  done
  redis_db=${CONFIG[REDIS_DB]-}
  if [[ ! $redis_db =~ ^[0-9]+$ ]]; then
    fail 'REDIS_DB must be a non-negative integer.'
    return 1
  fi
  CONFIG[REDIS_DB]=$((10#$redis_db))

  local validation_status=0
  python3 - "${CONFIG[RABBITMQ_URL]}" "${CONFIG[RABBITMQ_USER]}" "${CONFIG[RABBITMQ_PASSWORD]}" <<'PY' || validation_status=$?
import sys
from urllib.parse import unquote, urlsplit
raw, expected_user, expected_password = sys.argv[1:]
try:
    parsed = urlsplit(raw)
except ValueError:
    raise SystemExit(1)
if parsed.scheme not in {'amqp', 'amqps'} or not parsed.hostname:
    raise SystemExit(1)
if parsed.username is None or parsed.password is None:
    raise SystemExit(2)
if unquote(parsed.username) != expected_user or unquote(parsed.password) != expected_password:
    raise SystemExit(3)
PY
  if ((validation_status != 0)); then
    case $validation_status in
      2) fail 'RABBITMQ_URL must include URL-encoded username and password credentials.' ;;
      3) fail 'RABBITMQ_URL credentials must match RABBITMQ_USER and RABBITMQ_PASSWORD.' ;;
      *) fail 'RABBITMQ_URL must be a valid amqp or amqps URL.' ;;
    esac
    return 1
  fi

  if ! python3 - "${CONFIG[ELASTICSEARCH_URL]}" <<'PYURL'
import sys
from urllib.parse import urlsplit
try:
    parsed = urlsplit(sys.argv[1])
except ValueError:
    raise SystemExit(1)
if parsed.scheme not in {'http', 'https'} or not parsed.hostname:
    raise SystemExit(1)
if parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
    raise SystemExit(1)
PYURL
  then
    fail 'ELASTICSEARCH_URL must be an HTTP(S) URL with a host and without credentials, query, or fragment.'
    return 1
  fi
}

compose() {
  local key
  for key in "${COMPOSE_KEYS[@]}"; do export "$key=${CONFIG[$key]}"; done
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

container_json() {
  local service=$1 id
  id=$(docker ps --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service" --format '{{.ID}}' 2>/dev/null | head -n 1)
  [[ -n "$id" ]] || return 1
  docker inspect "$id" 2>/dev/null
}

healthy_compose_port() {
  local service=$1 container_port=$2 host_port=$3
  container_json "$service" | python3 -c '
import json
import sys
container_port, host_port = sys.argv[1:]
data = json.load(sys.stdin)[0]
if not data.get("State", {}).get("Running") or data.get("State", {}).get("Health", {}).get("Status") != "healthy":
    raise SystemExit(1)
for binding in data.get("NetworkSettings", {}).get("Ports", {}).get(container_port) or []:
    if str(binding.get("HostPort")) == host_port:
        raise SystemExit(0)
raise SystemExit(1)
' "$container_port" "$host_port"
}

port_owner() {
  local port=$1 output
  output=$(ss -ltnp "sport = :$port" 2>/dev/null | tail -n +2 || true)
  if [[ -n "$output" ]]; then
    printf '%s' "$output" | tr '\n' ';'
    return 0
  fi
  return 1
}

check_ports() {
  local -a names=(Backend Frontend MySQL Redis RabbitMQ 'RabbitMQ management' Elasticsearch)
  local -a ports=("${CONFIG[HTTP_PORT]}" 5173 "${CONFIG[MYSQL_PORT]}" "${CONFIG[REDIS_PORT]}" "${CONFIG[RABBITMQ_PORT]}" "${CONFIG[RABBITMQ_MANAGEMENT_PORT]}" "${CONFIG[ELASTICSEARCH_PORT]}")
  local -a services=('' '' mysql redis rabbitmq rabbitmq elasticsearch)
  local -a container_ports=('' '' 3306/tcp 6379/tcp 5672/tcp 15672/tcp 9200/tcp)
  local i j owner
  for ((i=0; i<${#ports[@]}; i++)); do
    for ((j=i+1; j<${#ports[@]}; j++)); do
      if [[ ${ports[i]} == "${ports[j]}" ]]; then
        fail "Configured services share port ${ports[i]}: ${names[i]}, ${names[j]}."
        return 1
      fi
    done
  done
  for ((i=0; i<${#ports[@]}; i++)); do
    if owner=$(port_owner "${ports[i]}"); then
      if [[ -n ${services[i]} ]] && healthy_compose_port "${services[i]}" "${container_ports[i]}" "${ports[i]}"; then
        info "${names[i]} already uses port ${ports[i]} through a healthy gopulse container."
        continue
      fi
      fail "Port ${ports[i]} required by ${names[i]} is already in use: $owner"
      return 1
    fi
  done
  info 'Required ports are available or owned by healthy gopulse containers.'
}

acquire_lock() {
  mkdir -p "$BIN_DIR"
  local created=0 platform lock_json
  if (set -o noclobber; : > "$LOCK_PATH") 2>/dev/null; then
    created=1
  fi

  exec 9<>"$LOCK_PATH"
  if ! flock -n 9; then
    fail 'Another development session is already running for this repository.'
    return 1
  fi

  if ((created == 0)); then
    platform=$(python3 - "$LOCK_PATH" <<'PY'
import json
import sys
try:
    print(json.load(open(sys.argv[1], encoding='utf-8')).get('platform', ''))
except Exception:
    print('')
PY
)
    if [[ -n "$platform" && $platform != unix ]]; then
      flock -u 9 || true
      exec 9>&-
      fail 'A development lock from another platform exists. Run the matching down script before retrying.'
      return 1
    fi
    : > "$LOCK_PATH"
    info 'Removed an unlocked stale development lock.'
  fi

  LOCK_TOKEN=$(python3 - <<'PY'
import secrets
print(secrets.token_hex(16))
PY
)
  local start_time executable
  start_time=$(ps -o lstart= -p "$$" | sed 's/^ *//')
  executable=$(readlink -f "/proc/$$/exe")
  lock_json=$(python3 - "$$" "$start_time" "$executable" "$REPO_ROOT" "${BASH_SOURCE[0]}" "$LOCK_TOKEN" <<'PY'
import json
import os
import sys
pid, start, executable, cwd, script, token = sys.argv[1:]
print(json.dumps({'pid': int(pid), 'startTime': start, 'executablePath': executable, 'workingDirectory': cwd, 'scriptPath': os.path.realpath(script), 'platform': 'unix', 'token': token}, separators=(',', ':')))
PY
)
  printf '%s' "$lock_json" >&9
  LOCK_OWNED=1
  info 'Acquired the development run lock.'
}
release_lock() {
  ((LOCK_OWNED == 1)) || return 0
  local token=
  token=$(python3 - "$LOCK_PATH" <<'PY'
import json
import sys
try:
    print(json.load(open(sys.argv[1], encoding='utf-8')).get('token', ''))
except Exception:
    print('')
PY
)
  if [[ $token == "$LOCK_TOKEN" ]]; then rm -f -- "$LOCK_PATH"; fi
  flock -u 9 || true
  exec 9>&-
  LOCK_OWNED=0
}

process_start_ticks() {
  python3 - "$1" <<'PY'
import sys

pid = int(sys.argv[1])
stat = open(f'/proc/{pid}/stat', encoding='utf-8').read().strip()
command_end = stat.rfind(')')
if command_end < 0:
    raise SystemExit(1)
fields = stat[command_end + 2:].split()
if len(fields) <= 19:
    raise SystemExit(1)
print(fields[19])
PY
}

process_executable() {
  readlink -f "/proc/$1/exe" 2>/dev/null
}

process_command_line() {
  tr '\0' ' ' < "/proc/$1/cmdline" 2>/dev/null || true
}

validate_record() {
  local path=$1 expected_cwd=$2 expected_marker=$3 expected_executable=${4:-}
  python3 - "$path" "$expected_cwd" "$expected_marker" "$expected_executable" <<'PY'
import json
import os
import sys
path, expected_cwd, expected_marker, expected_executable = sys.argv[1:]
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
if cwd != os.path.realpath(expected_cwd) or marker != expected_marker:
    print('record identity does not match this repository')
    raise SystemExit(1)
if expected_executable and executable != os.path.realpath(expected_executable):
    print('recorded executable does not match the expected application')
    raise SystemExit(1)
try:
    stat = open(f'/proc/{pid}/stat', encoding='utf-8').read().strip()
    command_end = stat.rfind(')')
    fields = stat[command_end + 2:].split()
    actual_start_ticks = fields[19]
    actual_executable = os.path.realpath(f'/proc/{pid}/exe')
    command_line = open(f'/proc/{pid}/cmdline', 'rb').read().replace(b'\0', b' ').decode(errors='replace')
except Exception:
    print('recorded process is not running')
    raise SystemExit(1)
if actual_start_ticks != start_ticks:
    print('process start identity does not match')
    raise SystemExit(1)
if actual_executable != executable:
    print('process executable does not match')
    raise SystemExit(1)
if expected_marker not in command_line:
    print('process command line does not match the recorded project context')
    raise SystemExit(1)
print(pid)
PY
}

reject_or_remove_record() {
  local name=$1 path=$2 expected_cwd=$3 marker=$4 executable=${5:-} result
  [[ -f "$path" ]] || return 0
  if result=$(validate_record "$path" "$expected_cwd" "$marker" "$executable"); then
    fail "$name is already running under this repository. Run scripts/down.sh before starting another dev session."
    return 1
  fi
  rm -f -- "$path"
  info "Removed stale $name process record ($result)."
}

compose_health() {
  local service=$1
  if ! container_json "$service" | python3 -c '
import json
import sys
data = json.load(sys.stdin)[0]
state = data.get("State", {})
if not state.get("Running"):
    print("stopped:" + str(state.get("Status", "unknown")))
else:
    print(state.get("Health", {}).get("Status", "no-healthcheck"))
'; then
    printf 'missing\n'
  fi
}

wait_for_infrastructure() {
  local deadline=$((SECONDS + 180)) service status all_healthy
  local services=(mysql redis rabbitmq elasticsearch)
  info 'Waiting for MySQL, Redis, RabbitMQ, and Elasticsearch healthchecks.'
  while ((SECONDS < deadline)); do
    all_healthy=1
    for service in "${services[@]}"; do
      status=$(compose_health "$service")
      [[ $status == healthy ]] || all_healthy=0
      [[ $status != stopped:* ]] || break 2
    done
    ((all_healthy == 0)) || { info 'Infrastructure is healthy.'; return 0; }
    sleep 2
  done
  local failed=()
  for service in "${services[@]}"; do
    status=$(compose_health "$service")
    [[ $status == healthy ]] || failed+=("$service=$status")
  done
  fail "Infrastructure did not become healthy (${failed[*]}). Inspect it with: docker compose --project-name $PROJECT_NAME --env-file '$ENV_FILE' --file '$COMPOSE_FILE' ps"
}

run_database_migrations() {
  local -a env_args=() key
  for key in "${BACKEND_KEYS[@]}"; do
    [[ -v CONFIG[$key] ]] && env_args+=("$key=${CONFIG[$key]}")
  done
  info 'Applying database migrations.'
  (cd "$BACKEND_DIR" && env "${env_args[@]}" go run ./cmd/migrate up)
}

run_search_reindex() {
  local -a env_args=() key
  for key in MYSQL_HOST MYSQL_PORT MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD ELASTICSEARCH_URL ELASTICSEARCH_REQUEST_TIMEOUT SEARCH_REINDEX_BATCH; do
    env_args+=("$key=${CONFIG[$key]}")
  done
  info 'Initializing the rebuildable post search index when missing.'
  (cd "$BACKEND_DIR" && env "${env_args[@]}" go run ./cmd/search-reindex --if-missing)
}

ensure_frontend_dependencies() {
  local lock="$FRONTEND_DIR/package-lock.json" modules="$FRONTEND_DIR/node_modules" marker="$FRONTEND_DIR/node_modules/.gopulse-package-lock.sha256" hash platform fingerprint recorded= needs_install=0
  [[ -f "$lock" ]] || { fail 'frontend/package-lock.json is required for reproducible installation.'; return 1; }
  hash=$(sha256sum "$lock" | awk '{print $1}')
  platform=$(node -p 'process.platform + "-" + process.arch')
  fingerprint="$hash|$platform"
  [[ -d "$modules" ]] || needs_install=1
  if ((needs_install == 0)); then
    [[ -f "$marker" ]] && recorded=$(<"$marker")
    [[ $recorded == "$fingerprint" ]] || needs_install=1
  fi
  if ((needs_install == 0)) && ! (cd "$FRONTEND_DIR" && npm ls --depth=0 --ignore-scripts >/dev/null 2>&1); then needs_install=1; fi
  if ((needs_install == 1)); then
    info 'Frontend dependencies are missing or do not match package-lock.json; running npm ci.'
    (cd "$FRONTEND_DIR" && npm ci) || return 1
    printf '%s\n' "$fingerprint" > "$marker" || return 1
  else
    info 'Frontend dependencies match package-lock.json.'
  fi
}

build_applications() {
  info 'Building the Backend and Business Worker development executables.'
  (cd "$BACKEND_DIR" && go build -o "$BACKEND_BINARY" ./cmd/server && go build -o "$WORKER_BINARY" ./cmd/business-worker)
}

write_process_record() {
  local pid=$1 path=$2 cwd=$3 marker=$4 temporary
  temporary="$path.$RANDOM.tmp"
  local start executable
  start=$(process_start_ticks "$pid")
  executable=$(process_executable "$pid")
  python3 - "$temporary" "$path" "$pid" "$start" "$executable" "$cwd" "$marker" <<'PY'
import json
import os
import sys
temporary, path, pid, start, executable, cwd, marker = sys.argv[1:]
with open(temporary, 'w', encoding='utf-8') as handle:
    json.dump({'pid': int(pid), 'startTicks': start, 'executablePath': executable, 'workingDirectory': os.path.realpath(cwd), 'commandLineMarker': marker}, handle, separators=(',', ':'))
    handle.flush()
    os.fsync(handle.fileno())
os.replace(temporary, path)
PY
}

start_backend() {
  local -a env_args=() key
  for key in "${BACKEND_KEYS[@]}"; do env_args+=("$key=${CONFIG[$key]}"); done
  info 'Starting Backend.'
  env "${env_args[@]}" python3 - "$BACKEND_DIR" "$BACKEND_BINARY" <<'PY' &
import os
import sys
cwd, executable = sys.argv[1:]
os.chdir(cwd)
os.setsid()
os.execve(executable, [executable], os.environ)
PY
  BACKEND_PID=$!
  sleep 0.6
  kill -0 "$BACKEND_PID" 2>/dev/null || { local code=0; wait "$BACKEND_PID" || code=$?; fail "Backend exited during startup with code $code."; return 1; }
  BACKEND_STARTED=1
  write_process_record "$BACKEND_PID" "$BACKEND_RECORD" "$BACKEND_DIR" "$BACKEND_BINARY"
}

start_worker() {
  local -a env_args=() key
  for key in "${WORKER_KEYS[@]}"; do env_args+=("$key=${CONFIG[$key]}"); done
  info 'Starting Business Worker.'
  env "${env_args[@]}" python3 - "$BACKEND_DIR" "$WORKER_BINARY" <<'PY' &
import os
import sys
cwd, executable = sys.argv[1:]
os.chdir(cwd)
os.setsid()
os.execve(executable, [executable], os.environ)
PY
  WORKER_PID=$!
  sleep 0.6
  kill -0 "$WORKER_PID" 2>/dev/null || { local code=0; wait "$WORKER_PID" || code=$?; fail "Business Worker exited during startup with code $code."; return 1; }
  WORKER_STARTED=1
  write_process_record "$WORKER_PID" "$WORKER_RECORD" "$BACKEND_DIR" "$WORKER_BINARY"
}

start_frontend() {
  [[ -f "$VITE_CLI" ]] || { fail 'The project-local Vite CLI is missing after dependency installation.'; return 1; }
  local -a unset_args=() key
  for key in "${ALL_CONFIG_KEYS[@]}"; do unset_args+=("-u" "$key"); done
  info 'Starting Frontend.'
  env "${unset_args[@]}" "HTTP_PORT=${CONFIG[HTTP_PORT]}" python3 - "$FRONTEND_DIR" "$(command -v node)" "$VITE_CLI" --host localhost --strictPort --config "$VITE_CONFIG" <<'PY' &
import os
import sys
cwd, executable, *arguments = sys.argv[1:]
os.chdir(cwd)
os.setsid()
os.execve(executable, [executable, *arguments], os.environ)
PY
  FRONTEND_PID=$!
  sleep 0.6
  kill -0 "$FRONTEND_PID" 2>/dev/null || { local code=0; wait "$FRONTEND_PID" || code=$?; fail "Frontend exited during startup with code $code."; return 1; }
  FRONTEND_STARTED=1
  write_process_record "$FRONTEND_PID" "$FRONTEND_RECORD" "$FRONTEND_DIR" "$VITE_CONFIG"
}

stop_recorded_application() {
  local name=$1 path=$2 cwd=$3 marker=$4 executable=${5:-} fallback=${6:-} result pid stopped=0
  if [[ -f "$path" ]]; then
    if result=$(validate_record "$path" "$cwd" "$marker" "$executable"); then
      pid=$result
      kill -TERM -- "-$pid" 2>/dev/null || true
      for _ in {1..20}; do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
      kill -KILL -- "-$pid" 2>/dev/null || true
      info "Stopped $name (PID $pid)."
      stopped=1
    else
      info "Removed stale $name record without stopping a process ($result)."
    fi
    rm -f -- "$path"
  fi
  if ((stopped == 0)) && [[ -n "$fallback" ]] && kill -0 "$fallback" 2>/dev/null; then
    kill -TERM -- "-$fallback" 2>/dev/null || true
    sleep 0.2
    kill -KILL -- "-$fallback" 2>/dev/null || true
    info "Stopped $name (PID $fallback)."
  fi
}

cleanup() {
  trap - EXIT INT TERM
  if ((FRONTEND_STARTED == 1)); then
    stop_recorded_application Frontend "$FRONTEND_RECORD" "$FRONTEND_DIR" "$VITE_CONFIG" "$(command -v node 2>/dev/null || true)" "$FRONTEND_PID" || true
  fi
  if ((WORKER_STARTED == 1)); then
    stop_recorded_application "Business Worker" "$WORKER_RECORD" "$BACKEND_DIR" "$WORKER_BINARY" "$WORKER_BINARY" "$WORKER_PID" || true
  fi
  if ((BACKEND_STARTED == 1)); then
    stop_recorded_application Backend "$BACKEND_RECORD" "$BACKEND_DIR" "$BACKEND_BINARY" "$BACKEND_BINARY" "$BACKEND_PID" || true
  fi
  release_lock || true
  exit "$EXIT_CODE"
}

on_signal() {
  EXIT_CODE=130
  cleanup
}
trap cleanup EXIT
trap on_signal INT TERM

main() {
  require_tools || return 1
  acquire_lock || return 1
  reject_or_remove_record Backend "$BACKEND_RECORD" "$BACKEND_DIR" "$BACKEND_BINARY" "$BACKEND_BINARY" || return 1
  reject_or_remove_record "Business Worker" "$WORKER_RECORD" "$BACKEND_DIR" "$WORKER_BINARY" "$WORKER_BINARY" || return 1
  reject_or_remove_record Frontend "$FRONTEND_RECORD" "$FRONTEND_DIR" "$VITE_CONFIG" "$(command -v node)" || return 1

  local source_file
  if [[ -f "$ENV_FILE" ]]; then source_file=$ENV_FILE; else source_file=$ENV_EXAMPLE_FILE; fi
  [[ -f "$source_file" ]] || { fail '.env is absent and .env.example is not available; no local environment file was created.'; return 1; }
  read_dotenv "$source_file" || return 1
  resolve_configuration || return 1
  check_ports || return 1

  if [[ ! -f "$ENV_FILE" ]]; then
    cp -- "$ENV_EXAMPLE_FILE" "$ENV_FILE" || return 1
    info 'Created .env from .env.example.'
  fi
  read_dotenv "$ENV_FILE" || return 1
  resolve_configuration || return 1

  info 'Starting Compose infrastructure.'
  compose up -d mysql redis rabbitmq elasticsearch || return 1
  wait_for_infrastructure || return 1
  run_database_migrations || return 1
  run_search_reindex || return 1
  ensure_frontend_dependencies || return 1
  build_applications || return 1
  start_backend || return 1
  start_worker || return 1
  start_frontend || return 1

  printf '\nGoPulse development services:\n'
  printf '  Frontend:            http://localhost:5173\n'
  printf '  Backend:             http://localhost:%s\n' "${CONFIG[HTTP_PORT]}"
  printf '  Health:              http://localhost:%s/health\n' "${CONFIG[HTTP_PORT]}"
  printf '  Readiness:           http://localhost:%s/ready\n' "${CONFIG[HTTP_PORT]}"
  printf '  RabbitMQ management: http://localhost:%s\n' "${CONFIG[RABBITMQ_MANAGEMENT_PORT]}"
  printf '  Elasticsearch:       http://localhost:%s\n\n' "${CONFIG[ELASTICSEARCH_PORT]}"
  info 'Press Ctrl+C to stop Frontend, Business Worker, and Backend. Infrastructure will remain running.'

  while true; do
    if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
      wait "$BACKEND_PID" || EXIT_CODE=$?
      fail "Backend exited unexpectedly with code $EXIT_CODE."
      return 1
    fi
    if ! kill -0 "$WORKER_PID" 2>/dev/null; then
      wait "$WORKER_PID" || EXIT_CODE=$?
      fail "Business Worker exited unexpectedly with code $EXIT_CODE."
      return 1
    fi
    if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
      wait "$FRONTEND_PID" || EXIT_CODE=$?
      fail "Frontend exited unexpectedly with code $EXIT_CODE."
      return 1
    fi
    sleep 0.5
  done
}

if ! main; then
  EXIT_CODE=1
fi
