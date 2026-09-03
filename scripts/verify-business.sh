#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
BACKEND_DIR="$REPO_ROOT/backend"
FRONTEND_DIR="$REPO_ROOT/frontend"
TOKEN=
PROJECT_NAME=
DATABASE_NAME=
TEMP_DIR=
ACCEPTANCE_ENV=
BACKEND_PID=
WORKER_PID=
SEARCH_INDEXER_PID=
FRONTEND_PID=
RESOURCES_STARTED=0
CLEANUP_DONE=0
SNAPSHOT_READY=0
PUBLISHED_HOST=127.0.0.1
REDIS_DB=0
MYSQL_PORT=
REDIS_PORT=
RABBITMQ_PORT=
RABBITMQ_MANAGEMENT_PORT=
ELASTICSEARCH_PORT=
HTTP_PORT=
FRONTEND_PORT=
MYSQL_USER=
MYSQL_PASSWORD=
MYSQL_ROOT_PASSWORD=
REDIS_PASSWORD=
RABBITMQ_USER=
RABBITMQ_PASSWORD=
JWT_SECRET=
COOKIE_NAME=
COOKIE_JAR=
RESPONSE_FILE=
HTTP_STATUS=
RESPONSE_HEADERS_FILE=
LAST_REQUEST_ID=
LOGGING_CAPTURE=0
CLIENT_REQUEST_ID=
LOG_EXPECTATIONS=
REQUEST_TRACE=
MYSQL_CONTAINER_ID=
ELASTICSEARCH_CONTAINER_ID=
RABBITMQ_CONTAINER_ID=

info() { printf '[gopulse-acceptance] %s\n' "$*"; }
fail() { printf '[gopulse-acceptance] ERROR: %s\n' "$*" >&2; return 1; }

valid_token() { [[ $1 =~ ^[a-f0-9]{12}$ ]]; }
valid_project() { [[ $1 =~ ^gopulse-acceptance-[a-f0-9]{12}$ ]]; }
valid_database() { [[ $1 =~ ^gopulse_acceptance_[a-f0-9]{12}$ ]]; }
valid_port() { [[ $1 =~ ^[0-9]+$ ]] && ((10#$1 >= 1024 && 10#$1 <= 65535)); }

validate_target() {
  local token=$1 project=$2 database=$3 host=$4 mysql_port=$5 redis_port=$6 rabbit_port=$7 rabbit_management_port=$8 elasticsearch_port=$9 http_port=${10} frontend_port=${11}
  valid_token "$token" || { fail 'acceptance token must contain exactly 12 lowercase hexadecimal characters'; return 1; }
  [[ $project == "gopulse-acceptance-$token" ]] && valid_project "$project" || { fail 'Compose project is outside the acceptance whitelist'; return 1; }
  [[ $database == "gopulse_acceptance_$token" ]] && valid_database "$database" || { fail 'database is outside the acceptance whitelist'; return 1; }
  [[ $host == 127.0.0.1 ]] || { fail 'all published acceptance addresses must be 127.0.0.1'; return 1; }
  local port
  for port in "$mysql_port" "$redis_port" "$rabbit_port" "$rabbit_management_port" "$elasticsearch_port" "$http_port" "$frontend_port"; do
    valid_port "$port" || { fail "invalid acceptance port: $port"; return 1; }
  done
  [[ $mysql_port != 3306 && $redis_port != 6379 && $rabbit_port != 5672 && $rabbit_management_port != 15672 && $elasticsearch_port != 9200 && $http_port != 8080 && $frontend_port != 5173 ]] || {
    fail 'acceptance must not use a default development port'
    return 1
  }
  [[ $(printf '%s\n' "$mysql_port" "$redis_port" "$rabbit_port" "$rabbit_management_port" "$elasticsearch_port" "$http_port" "$frontend_port" | sort -u | wc -l) == 7 ]] || {
    fail 'acceptance ports must be unique'
    return 1
  }
}

self_test() {
  local token=012345abcdef project=gopulse-acceptance-012345abcdef database=gopulse_acceptance_012345abcdef
  validate_target "$token" "$project" "$database" 127.0.0.1 43306 46379 45672 45673 49200 48080 45173 >/dev/null
  local rejected=0
  for command in \
    "validate_target '' '$project' '$database' 127.0.0.1 43306 46379 45672 45673 49200 48080 45173" \
    "validate_target '$token' gopulse '$database' 127.0.0.1 43306 46379 45672 45673 49200 48080 45173" \
    "validate_target '$token' '$project' gopulse 127.0.0.1 43306 46379 45672 45673 49200 48080 45173" \
    "validate_target '$token' '$project' '$database' 0.0.0.0 43306 46379 45672 45673 49200 48080 45173" \
    "validate_target '$token' '$project' '$database' 127.0.0.1 3306 46379 45672 45673 49200 48080 45173" \
    "validate_target '$token' '$project' '$database' 127.0.0.1 43306 43306 45672 45673 49200 48080 45173"; do
    if eval "$command" >/dev/null 2>&1; then
      fail "unsafe target unexpectedly passed: $command"
      return 1
    fi
    rejected=$((rejected + 1))
  done
  info "Safety self-test passed: one valid target accepted and $rejected unsafe targets rejected without Docker access."
}

require_tools() {
  local missing=() tool
  for tool in bash curl docker go node npm python3 sha256sum sort setsid; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done
  ((${#missing[@]} == 0)) || { fail "missing required tool(s): ${missing[*]}"; return 1; }
  docker compose version >/dev/null 2>&1 || { fail 'Docker Compose is unavailable'; return 1; }
  docker info >/dev/null 2>&1 || { fail 'Docker daemon is unavailable'; return 1; }
}

generate_ports() {
  local values
  while true; do
    values=$(python3 - <<'PY'
import socket
sockets=[]
ports=[]
for _ in range(7):
    sock=socket.socket()
    sock.bind(('127.0.0.1', 0))
    sockets.append(sock)
    ports.append(sock.getsockname()[1])
print(' '.join(map(str, ports)))
for sock in sockets:
    sock.close()
PY
)
    read -r MYSQL_PORT REDIS_PORT RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT ELASTICSEARCH_PORT HTTP_PORT FRONTEND_PORT <<<"$values"
    if validate_target "$TOKEN" "$PROJECT_NAME" "$DATABASE_NAME" "$PUBLISHED_HOST" "$MYSQL_PORT" "$REDIS_PORT" "$RABBITMQ_PORT" "$RABBITMQ_MANAGEMENT_PORT" "$ELASTICSEARCH_PORT" "$HTTP_PORT" "$FRONTEND_PORT" >/dev/null 2>&1; then
      return
    fi
  done
}

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ACCEPTANCE_ENV" --file "$COMPOSE_FILE" "$@"
}

service_id() {
  local service=$1 ids count
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service")
  count=$(sed '/^$/d' <<<"$ids" | wc -l | tr -d ' ')
  [[ $count == 1 ]] || { fail "expected one $service container in $PROJECT_NAME, found $count"; return 1; }
  printf '%s\n' "$ids"
}

verify_service_ownership() {
  local service=$1 container_port=$2 expected_host_port=$3 id project_label service_label published
  id=$(service_id "$service") || return 1
  project_label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
  service_label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id")
  [[ $project_label == "$PROJECT_NAME" && $service_label == "$service" ]] || {
    fail "container $id ownership labels do not match $PROJECT_NAME/$service"
    return 1
  }
  published=$(docker inspect "$id" | python3 -c 'import json, sys
container_port=sys.argv[1] + "/tcp"
bindings=json.load(sys.stdin)[0].get("HostConfig", {}).get("PortBindings", {}).get(container_port) or []
if len(bindings) != 1:
    raise SystemExit(1)
print("{}:{}".format(bindings[0].get("HostIp", ""), bindings[0].get("HostPort", "")))' "$container_port") || {
    fail "container $id has no unique persistent port binding for $container_port"
    return 1
  }
  [[ $published == "$PUBLISHED_HOST:$expected_host_port" ]] || {
    fail "container $id publishes $container_port as '$published', expected $PUBLISHED_HOST:$expected_host_port"
    return 1
  }
  printf '%s\n' "$id"
}

assert_project_absent() {
  local containers networks volumes
  containers=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME")
  networks=$(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME")
  volumes=$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME")
  if [[ -n $containers || -n $networks || -n $volumes ]]; then
    fail "refusing to reuse pre-existing resources for $PROJECT_NAME"
    return 1
  fi
}

wait_service_health() {
  local service=$1 deadline=$((SECONDS + 120)) id state
  id=$(service_id "$service") || return 1
  while ((SECONDS < deadline)); do
    state=$(docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id" 2>/dev/null || true)
    [[ $state == 'running|healthy' ]] && return
    sleep 2
  done
  compose logs "$service" >&2 || true
  fail "$service did not become healthy"
}

wait_http_status() {
  local url=$1 expected=$2 deadline=$((SECONDS + 60)) status
  while ((SECONDS < deadline)); do
    status=$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 3 "$url" 2>/dev/null || true)
    [[ $status == "$expected" ]] && return
    sleep 1
  done
  fail "$url did not return HTTP $expected"
}

snapshot_development_state() {
  local output=$1 id
  {
    if [[ -f "$REPO_ROOT/.env" ]]; then sha256sum "$REPO_ROOT/.env"; else echo '.env missing'; fi
    if [[ -d "$REPO_ROOT/.run" ]]; then
      find "$REPO_ROOT/.run" -type f -print0 | sort -z | xargs -0 -r sha256sum
    else
      echo '.run missing'
    fi
    while IFS= read -r id; do
      [[ -z $id ]] || docker inspect --format '{{.Id}}|{{.Name}}|{{.State.Status}}|{{.State.StartedAt}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id"
    done < <(docker ps -aq --filter 'label=com.docker.compose.project=gopulse' | sort)
    docker volume ls --filter 'label=com.docker.compose.project=gopulse' --format '{{.Name}}' | sort
    id=$(docker ps -q --filter 'label=com.docker.compose.project=gopulse' --filter 'label=com.docker.compose.service=mysql' | head -n1)
    if [[ -n $id ]]; then
      docker exec "$id" sh -c 'mysqldump --single-transaction --skip-comments --compact --user=root --password="$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE" 2>/dev/null' | sha256sum || echo 'development mysql snapshot unavailable'
    else
      echo 'development mysql not running'
    fi
    id=$(docker ps -q --filter 'label=com.docker.compose.project=gopulse' --filter 'label=com.docker.compose.service=redis' | head -n1)
    if [[ -n $id ]]; then
      docker exec "$id" sh -c 'redis-cli --no-auth-warning --raw -a "$REDIS_PASSWORD" INFO keyspace; redis-cli --no-auth-warning --raw -a "$REDIS_PASSWORD" --scan | sort' | sha256sum || echo 'development redis snapshot unavailable'
    else
      echo 'development redis not running'
    fi
  } >"$output"
}

write_environment() {
  MYSQL_USER="acceptance_$TOKEN"
  MYSQL_PASSWORD="mysql-$TOKEN"
  MYSQL_ROOT_PASSWORD="root-$TOKEN"
  REDIS_PASSWORD="redis-$TOKEN"
  RABBITMQ_USER="rabbit_$TOKEN"
  RABBITMQ_PASSWORD="rabbit-$TOKEN"
  JWT_SECRET="acceptance-jwt-$TOKEN-32-byte-secret"
  COOKIE_NAME="gopulse_acceptance_$TOKEN"
  cat >"$ACCEPTANCE_ENV" <<ENV
APP_ENV=test
PUBLISHED_HOST=$PUBLISHED_HOST
HTTP_HOST=$PUBLISHED_HOST
HTTP_PORT=$HTTP_PORT
MYSQL_HOST=$PUBLISHED_HOST
MYSQL_PORT=$MYSQL_PORT
MYSQL_DATABASE=$DATABASE_NAME
MYSQL_USER=$MYSQL_USER
MYSQL_PASSWORD=$MYSQL_PASSWORD
MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD
REDIS_HOST=$PUBLISHED_HOST
REDIS_PORT=$REDIS_PORT
REDIS_PASSWORD=$REDIS_PASSWORD
REDIS_DB=$REDIS_DB
RABBITMQ_USER=$RABBITMQ_USER
RABBITMQ_PASSWORD=$RABBITMQ_PASSWORD
RABBITMQ_PORT=$RABBITMQ_PORT
RABBITMQ_MANAGEMENT_PORT=$RABBITMQ_MANAGEMENT_PORT
ELASTICSEARCH_PORT=$ELASTICSEARCH_PORT
ELASTICSEARCH_URL=http://$PUBLISHED_HOST:$ELASTICSEARCH_PORT
ELASTICSEARCH_REQUEST_TIMEOUT=3s
SEARCH_REINDEX_BATCH=2
RABBITMQ_URL=amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/
AUTH_JWT_SECRET=$JWT_SECRET
AUTH_JWT_TTL=2h
AUTH_COOKIE_NAME=$COOKIE_NAME
AUTH_COOKIE_SECURE=false
REDIS_POST_DETAIL_TTL=5m
REDIS_OPERATION_TIMEOUT=200ms
OUTBOX_POLL_INTERVAL=200ms
OUTBOX_CLAIM_BATCH=1
OUTBOX_LEASE_DURATION=3s
OUTBOX_PUBLISH_TIMEOUT=2s
OUTBOX_RETRY_DELAY=10s
OUTBOX_CLEANUP_INTERVAL=1h
OUTBOX_PUBLISHED_RETENTION=168h
OUTBOX_CLEANUP_BATCH=100
BUSINESS_WORKER_PREFETCH=1
BUSINESS_WORKER_MAX_RETRIES=2
BUSINESS_WORKER_PUBLISH_TIMEOUT=2s
BUSINESS_WORKER_SHUTDOWN_TIMEOUT=5s
BUSINESS_WORKER_RECONNECT_MIN=200ms
BUSINESS_WORKER_RECONNECT_MAX=2s
SEARCH_INDEXER_PREFETCH=1
SEARCH_INDEXER_MAX_RETRIES=20
SEARCH_INDEXER_RETRY_DELAY=2s
SEARCH_INDEXER_PUBLISH_TIMEOUT=2s
SEARCH_INDEXER_SHUTDOWN_TIMEOUT=5s
SEARCH_INDEXER_RECONNECT_MIN=200ms
SEARCH_INDEXER_RECONNECT_MAX=2s
ENV
}

backend_environment() {
  env \
    APP_ENV=test HTTP_HOST="$PUBLISHED_HOST" HTTP_PORT="$HTTP_PORT" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    REDIS_HOST="$PUBLISHED_HOST" REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB="$REDIS_DB" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" \
    ELASTICSEARCH_URL="http://$PUBLISHED_HOST:$ELASTICSEARCH_PORT" ELASTICSEARCH_REQUEST_TIMEOUT=3s SEARCH_REINDEX_BATCH=2 \
    AUTH_JWT_SECRET="$JWT_SECRET" AUTH_JWT_TTL=2h AUTH_COOKIE_NAME="$COOKIE_NAME" AUTH_COOKIE_SECURE=false \
    REDIS_POST_DETAIL_TTL=5m REDIS_OPERATION_TIMEOUT=200ms \
    OUTBOX_POLL_INTERVAL=200ms OUTBOX_CLAIM_BATCH=1 OUTBOX_LEASE_DURATION=3s OUTBOX_PUBLISH_TIMEOUT=2s OUTBOX_RETRY_DELAY=10s SEARCH_INDEXER_RETRY_DELAY=2s \
    OUTBOX_CLEANUP_INTERVAL=1h OUTBOX_PUBLISHED_RETENTION=168h OUTBOX_CLEANUP_BATCH=100 \
    BUSINESS_WORKER_PREFETCH=1 BUSINESS_WORKER_MAX_RETRIES=2 BUSINESS_WORKER_PUBLISH_TIMEOUT=2s \
    BUSINESS_WORKER_SHUTDOWN_TIMEOUT=5s BUSINESS_WORKER_RECONNECT_MIN=200ms BUSINESS_WORKER_RECONNECT_MAX=2s \
    "$@"
}

run_search_reindex() {
  backend_environment "$TEMP_DIR/gopulse-search-reindex" "$@" >>"$TEMP_DIR/search-reindex.log" 2>&1
}

start_backend() {
  [[ -z ${BACKEND_PID:-} ]] || fail 'Backend is already recorded as running'
  setsid bash -c 'cd "$1"; shift; exec env "$@"' _ "$BACKEND_DIR" \
    APP_ENV="${BACKEND_APP_ENV:-test}" HTTP_HOST="$PUBLISHED_HOST" HTTP_PORT="$HTTP_PORT" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    REDIS_HOST="$PUBLISHED_HOST" REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB="$REDIS_DB" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" \
    ELASTICSEARCH_URL="http://$PUBLISHED_HOST:$ELASTICSEARCH_PORT" ELASTICSEARCH_REQUEST_TIMEOUT=3s SEARCH_REINDEX_BATCH=2 \
    AUTH_JWT_SECRET="$JWT_SECRET" AUTH_JWT_TTL=2h AUTH_COOKIE_NAME="$COOKIE_NAME" AUTH_COOKIE_SECURE=false \
    REDIS_POST_DETAIL_TTL=5m REDIS_OPERATION_TIMEOUT=200ms \
    OUTBOX_POLL_INTERVAL=200ms OUTBOX_CLAIM_BATCH=1 OUTBOX_LEASE_DURATION=3s OUTBOX_PUBLISH_TIMEOUT=2s OUTBOX_RETRY_DELAY=10s SEARCH_INDEXER_RETRY_DELAY=2s \
    OUTBOX_CLEANUP_INTERVAL=1h OUTBOX_PUBLISHED_RETENTION=168h OUTBOX_CLEANUP_BATCH=100 \
    "$TEMP_DIR/gopulse-backend" >>"$TEMP_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/health" 200 || { cat "$TEMP_DIR/backend.log" >&2; return 1; }
}

start_worker() {
  [[ -z ${WORKER_PID:-} ]] || fail 'Business Worker is already recorded as running'
  setsid bash -c 'cd "$1"; shift; exec env "$@"' _ "$BACKEND_DIR" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" OUTBOX_RETRY_DELAY=10s \
    BUSINESS_WORKER_PREFETCH=1 BUSINESS_WORKER_MAX_RETRIES=2 BUSINESS_WORKER_PUBLISH_TIMEOUT=2s \
    BUSINESS_WORKER_SHUTDOWN_TIMEOUT=5s BUSINESS_WORKER_RECONNECT_MIN=200ms BUSINESS_WORKER_RECONNECT_MAX=2s \
    "$TEMP_DIR/gopulse-business-worker" >>"$TEMP_DIR/business-worker.log" 2>&1 &
  WORKER_PID=$!
  sleep 0.5
  if ! kill -0 "$WORKER_PID" 2>/dev/null; then
    cat "$TEMP_DIR/business-worker.log" >&2 || true
    fail 'Business Worker exited during startup'
  fi
}

start_search_indexer() {
  [[ -z ${SEARCH_INDEXER_PID:-} ]] || fail 'Search Indexer is already recorded as running'
  setsid bash -c 'cd "$1"; shift; exec env "$@"' _ "$BACKEND_DIR" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" \
    ELASTICSEARCH_URL="http://$PUBLISHED_HOST:$ELASTICSEARCH_PORT" ELASTICSEARCH_REQUEST_TIMEOUT=2s \
    SEARCH_INDEXER_PREFETCH=1 SEARCH_INDEXER_MAX_RETRIES=20 SEARCH_INDEXER_RETRY_DELAY=2s SEARCH_INDEXER_PUBLISH_TIMEOUT=2s \
    SEARCH_INDEXER_SHUTDOWN_TIMEOUT=5s SEARCH_INDEXER_RECONNECT_MIN=200ms SEARCH_INDEXER_RECONNECT_MAX=2s \
    "$TEMP_DIR/gopulse-search-indexer" >>"$TEMP_DIR/search-indexer.log" 2>&1 &
  SEARCH_INDEXER_PID=$!
  sleep 0.5
  if ! kill -0 "$SEARCH_INDEXER_PID" 2>/dev/null; then
    cat "$TEMP_DIR/search-indexer.log" >&2 || true
    fail 'Search Indexer exited during startup'
  fi
}

validate_process_ownership() {
  local pid=$1 cwd executable marker
  if [[ $pid == "${BACKEND_PID:-}" ]]; then
    cwd=$BACKEND_DIR; executable="$TEMP_DIR/gopulse-backend"; marker="$TEMP_DIR/gopulse-backend"
  elif [[ $pid == "${WORKER_PID:-}" ]]; then
    cwd=$BACKEND_DIR; executable="$TEMP_DIR/gopulse-business-worker"; marker="$TEMP_DIR/gopulse-business-worker"
  elif [[ $pid == "${SEARCH_INDEXER_PID:-}" ]]; then
    cwd=$BACKEND_DIR; executable="$TEMP_DIR/gopulse-search-indexer"; marker="$TEMP_DIR/gopulse-search-indexer"
  elif [[ $pid == "${FRONTEND_PID:-}" ]]; then
    cwd=$FRONTEND_DIR; executable=$(command -v node); marker='node_modules/vite/bin/vite.js'
  else
    fail "PID $pid is not recorded as an acceptance application process"
    return 1
  fi
  python3 - "$pid" "$cwd" "$executable" "$marker" <<'PY'
import os, sys
pid, expected_cwd, expected_executable, marker = sys.argv[1:]
try:
    actual_cwd = os.path.realpath(f'/proc/{pid}/cwd')
    actual_executable = os.path.realpath(f'/proc/{pid}/exe')
    command_line = open(f'/proc/{pid}/cmdline', 'rb').read().replace(b'\0', b' ').decode(errors='replace')
except Exception as exc:
    raise SystemExit(f'cannot inspect PID {pid}: {exc}')
if actual_cwd != os.path.realpath(expected_cwd):
    raise SystemExit(f'PID {pid} cwd ownership mismatch')
if actual_executable != os.path.realpath(expected_executable):
    raise SystemExit(f'PID {pid} executable ownership mismatch')
if marker not in command_line:
    raise SystemExit(f'PID {pid} command marker ownership mismatch')
PY
}

stop_pid() {
  local pid=${1:-}
  [[ -n $pid ]] || return 0
  if kill -0 "$pid" 2>/dev/null; then
    validate_process_ownership "$pid" || return 1
    kill -- "-$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true
    local deadline=$((SECONDS + 15))
    while kill -0 "$pid" 2>/dev/null && ((SECONDS < deadline)); do sleep 0.2; done
    kill -KILL -- "-$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

start_frontend() {
  [[ -x "$FRONTEND_DIR/node_modules/.bin/vite" ]] || (cd "$FRONTEND_DIR" && npm ci)
  setsid bash -c 'cd "$1"; shift; exec env "$@"' _ "$FRONTEND_DIR" \
    HTTP_PORT="$HTTP_PORT" node node_modules/vite/bin/vite.js --host "$PUBLISHED_HOST" --port "$FRONTEND_PORT" --strictPort \
    >>"$TEMP_DIR/frontend.log" 2>&1 &
  FRONTEND_PID=$!
  wait_http_status "http://$PUBLISHED_HOST:$FRONTEND_PORT/" 200 || { cat "$TEMP_DIR/frontend.log" >&2; return 1; }
}

api_request() {
  local method=$1 path=$2 expected=$3 body=${4:-} cookie_mode=${5:-read}
  local -a args=(--silent --show-error --max-time 10 --request "$method" --output "$RESPONSE_FILE" --dump-header "$RESPONSE_HEADERS_FILE" --write-out '%{http_code}')
  if [[ $cookie_mode == write ]]; then args+=(--cookie "$COOKIE_JAR" --cookie-jar "$COOKIE_JAR"); else args+=(--cookie "$COOKIE_JAR"); fi
  if [[ -n $body ]]; then args+=(--header 'Content-Type: application/json' --data "$body"); fi
  [[ -z ${CLIENT_REQUEST_ID:-} ]] || args+=(--header "X-Request-ID: $CLIENT_REQUEST_ID")
  HTTP_STATUS=$(curl "${args[@]}" "http://$PUBLISHED_HOST:$HTTP_PORT/api/v1$path")
  LAST_REQUEST_ID=$(awk 'BEGIN{IGNORECASE=1} /^X-Request-ID:/ {gsub("\r", "", $2); value=$2} END{print value}' "$RESPONSE_HEADERS_FILE")
  [[ $HTTP_STATUS == "$expected" ]] || {
    printf '[gopulse-acceptance] response body: ' >&2
    cat "$RESPONSE_FILE" >&2 || true
    printf '\n' >&2
    fail "$method $path returned HTTP $HTTP_STATUS, expected $expected"
  }
  if ((LOGGING_CAPTURE == 1)); then
    [[ $LAST_REQUEST_ID =~ ^[a-f0-9]{32}$ ]] || { fail "$method $path returned invalid X-Request-ID: ${LAST_REQUEST_ID:-missing}"; return 1; }
    printf '%s\t%s\t%s\t%s\n' "$method" "$path" "$expected" "$LAST_REQUEST_ID" >>"$REQUEST_TRACE"
  fi
}

json_get() {
  local path=$1
  python3 - "$RESPONSE_FILE" "$path" <<'PY'
import json, sys
value=json.load(open(sys.argv[1], encoding='utf-8'))
for part in sys.argv[2].split('.'):
    if not part:
        continue
    value=value[int(part)] if isinstance(value, list) else value[part]
if value is None:
    print('')
elif isinstance(value, bool):
    print(str(value).lower())
else:
    print(value)
PY
}

assert_json() {
  local expression=$1
  python3 - "$RESPONSE_FILE" "$expression" <<'PY'
import json, sys
value=json.load(open(sys.argv[1], encoding='utf-8'))
if not eval(sys.argv[2], {'__builtins__': {}}, {'value': value, 'len': len}):
    raise SystemExit(f'JSON assertion failed: {sys.argv[2]} value={value!r}')
PY
}

urlencode() { python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$1"; }

api_for() {
  local jar=$1
  shift
  local previous=$COOKIE_JAR
  COOKIE_JAR=$jar
  api_request "$@"
  COOKIE_JAR=$previous
}

mysql_query() {
  [[ -n $MYSQL_CONTAINER_ID ]] || fail 'acceptance MySQL container is not recorded'
  docker exec "$MYSQL_CONTAINER_ID" mysql --user="$MYSQL_USER" --password="$MYSQL_PASSWORD" \
    --batch --skip-column-names "$DATABASE_NAME" --execute "$1" 2>/dev/null
}

wait_sql_equals() {
  local sql=$1 expected=$2 description=$3 deadline=$((SECONDS + ${4:-30})) actual=
  while ((SECONDS < deadline)); do
    actual=$(mysql_query "$sql" 2>/dev/null | tr -d '\r' || true)
    [[ $actual == "$expected" ]] && return 0
    sleep 0.25
  done
  fail "$description (expected '$expected', found '${actual:-unavailable}')"
}

outbox_event_for_comment() {
  mysql_query "SELECT event_id FROM business_outbox WHERE event_type='comment.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.comment_id'))='$1' ORDER BY id DESC LIMIT 1;"
}

outbox_event_for_like() {
  mysql_query "SELECT event_id FROM business_outbox WHERE event_type='post.liked' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$1' ORDER BY id DESC LIMIT 1;"
}

outbox_event_for_post() {
  mysql_query "SELECT event_id FROM business_outbox WHERE event_type='post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$1' ORDER BY id DESC LIMIT 1;"
}

wait_notification() {
  wait_sql_equals "SELECT COUNT(*) FROM notifications WHERE source_event_id='$1';" "$2" "notification count for source event $1" "${3:-30}"
}

queue_metric() {
  local queue=$1 field=$2 body="$TEMP_DIR/queue.json" status
  status=$(curl --silent --show-error --max-time 5 --user "$RABBITMQ_USER:$RABBITMQ_PASSWORD" \
    --output "$body" --write-out '%{http_code}' \
    "http://$PUBLISHED_HOST:$RABBITMQ_MANAGEMENT_PORT/api/queues/%2F/$queue" 2>/dev/null || true)
  [[ $status == 200 ]] || return 1
  python3 - "$body" "$field" <<'PY'
import json, sys
value=json.load(open(sys.argv[1], encoding='utf-8')).get(sys.argv[2])
if not isinstance(value, int):
    raise SystemExit(1)
print(value)
PY
}

wait_queue_at_least() {
  local queue=$1 field=$2 minimum=$3 description=$4 deadline=$((SECONDS + ${5:-30})) actual=0
  while ((SECONDS < deadline)); do
    actual=$(queue_metric "$queue" "$field" 2>/dev/null || echo 0)
    [[ $actual =~ ^[0-9]+$ ]] && ((actual >= minimum)) && return 0
    sleep 0.25
  done
  fail "$description (expected $field >= $minimum, found $actual)"
}

wait_queue_equals() {
  local queue=$1 field=$2 expected=$3 description=$4 deadline=$((SECONDS + ${5:-30})) actual=
  while ((SECONDS < deadline)); do
    actual=$(queue_metric "$queue" "$field" 2>/dev/null || true)
    [[ $actual == "$expected" ]] && return 0
    sleep 0.25
  done
  fail "$description (expected $field=$expected, found ${actual:-unavailable})"
}

wait_log_json() {
  local path=$1 description=$2 timeout=$3
  shift 3
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if python3 - "$path" "$@" <<'PYLOGMATCH' 2>/dev/null
import json, sys
path, *pairs = sys.argv[1:]
expected = {}
for pair in pairs:
    key, raw = pair.split('=', 1)
    expected[key] = int(raw) if raw.isdigit() else raw
try:
    stream = open(path, encoding='utf-8')
except OSError:
    raise SystemExit(1)
with stream:
    for raw in stream:
        try:
            value = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if all(value.get(key) == wanted for key, wanted in expected.items()):
            raise SystemExit(0)
raise SystemExit(1)
PYLOGMATCH
    then
      return 0
    fi
    sleep 0.25
  done
  printf '[gopulse-acceptance] %s tail:\n' "$path" >&2
  tail -n 40 "$path" >&2 2>/dev/null || true
  fail "$description (missing JSON log fields: $*)"
}

generate_event() {
  python3 - "$@" <<'PY'
import datetime, json, sys, uuid
event_type, actor, recipient, post, comment = sys.argv[1:]
event_id=str(uuid.uuid4())
value={
    'schema_version': 1,
    'event_id': event_id,
    'event_type': event_type,
    'occurred_at': datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z'),
    'actor_id': int(actor),
    'recipient_id': int(recipient),
    'post_id': int(post),
}
if comment:
    value['comment_id']=int(comment)
print(event_id + '\t' + json.dumps(value, separators=(',', ':')))
PY
}

publish_message() {
  local routing_key=$1 event_type=$2 event_id=$3 payload=$4 attempt=${5:-0} exchange=${6:-gopulse.business.v1}
  local request="$TEMP_DIR/publish.json" response="$TEMP_DIR/publish-response.json" status
  python3 - "$request" "$routing_key" "$event_type" "$event_id" "$payload" "$attempt" <<'PY'
import datetime, json, sys, time
path, routing, event_type, event_id, payload, attempt = sys.argv[1:]
try:
    occurred_at=json.loads(payload)['occurred_at'].replace('Z', '+00:00')
    timestamp=int(datetime.datetime.fromisoformat(occurred_at).timestamp())
except (KeyError, TypeError, ValueError, json.JSONDecodeError):
    timestamp=int(time.time())
properties={
    'content_type': 'application/json',
    'delivery_mode': 2,
    'message_id': event_id,
    'type': event_type,
    'timestamp': timestamp,
    'headers': {'x-gopulse-attempt': int(attempt)},
}
json.dump({'properties': properties, 'routing_key': routing, 'payload': payload, 'payload_encoding': 'string'}, open(path, 'w', encoding='utf-8'))
PY
  status=$(curl --silent --show-error --max-time 10 --user "$RABBITMQ_USER:$RABBITMQ_PASSWORD" \
    --header 'Content-Type: application/json' --request POST --data-binary "@$request" \
    --output "$response" --write-out '%{http_code}' \
    "http://$PUBLISHED_HOST:$RABBITMQ_MANAGEMENT_PORT/api/exchanges/%2F/$exchange/publish")
  [[ $status == 200 ]] || { cat "$response" >&2 || true; fail "RabbitMQ publish returned HTTP $status"; return 1; }
  python3 - "$response" <<'PY'
import json, sys
value=json.load(open(sys.argv[1], encoding='utf-8'))
raise SystemExit(0 if value.get('routed') is True else 1)
PY
}

create_owner_post() {
  local jar=$1 title=$2
  api_for "$jar" POST /posts 201 "{\"title\":\"$title\",\"content\":\"Phase 2 reliability acceptance\"}" read
  json_get data.id
}

run_reliability_matrix() {
  info 'Running the closed Phase 2 reliability fault matrix.'
  local owner_jar="$TEMP_DIR/owner.cookies" actor_jar="$TEMP_DIR/actor.cookies"
  local owner="owner_$TOKEN" actor="actor_$TOKEN" password="matrix-$TOKEN-password"
  local owner_id actor_id post_id comment_id comment_event like_event queue_count
  local second_post second_comment second_comment_event second_like_event
  local broker_post broker_comment broker_comment_event broker_like_event
  local unacked_post unacked_comment unacked_event unacked_payload
  local duplicate_payload duplicate_count
  local transient_event transient_payload transient_lock_pid generated
  local follow_event follow_payload dead_before
  local durable_post durable_comment durable_event rabbit_before

  api_for "$owner_jar" POST /auth/register 201 "{\"username\":\"$owner\",\"password\":\"$password\"}" write
  owner_id=$(json_get data.id)
  post_id=$(create_owner_post "$owner_jar" "Matrix normal $TOKEN")
  api_for "$actor_jar" POST /auth/register 201 "{\"username\":\"$actor\",\"password\":\"$password\"}" write
  actor_id=$(json_get data.id)
  api_for "$actor_jar" POST "/posts/$post_id/comments" 201 '{"content":"normal matrix comment"}' read
  comment_id=$(json_get data.id)
  api_for "$actor_jar" PUT "/posts/$post_id/like" 204 '' read
  comment_event=$(outbox_event_for_comment "$comment_id")
  like_event=$(outbox_event_for_like "$post_id")
  wait_notification "$comment_event" 1
  wait_notification "$like_event" 1
  api_for "$owner_jar" GET /notifications 200 '' read
  assert_json "len(value['data']) >= 2"
  info 'Matrix 1/10 passed: normal two-user facts and notifications are observable through the application.'

  second_post=$(create_owner_post "$owner_jar" "Worker pause $TOKEN")
  stop_pid "$WORKER_PID"
  WORKER_PID=
  api_for "$actor_jar" POST "/posts/$second_post/comments" 201 '{"content":"queued while worker stopped"}' read
  second_comment=$(json_get data.id)
  api_for "$actor_jar" PUT "/posts/$second_post/like" 204 '' read
  second_comment_event=$(outbox_event_for_comment "$second_comment")
  second_like_event=$(outbox_event_for_like "$second_post")
  wait_notification "$second_comment_event" 0
  wait_notification "$second_like_event" 0
  wait_queue_at_least gopulse.business-worker.v1 messages_ready 2 'worker pause did not retain both durable messages'
  start_worker
  wait_notification "$second_comment_event" 1
  wait_notification "$second_like_event" 1
  info 'Matrix 2/10 passed: stopped consumer retained messages and recovery produced one notification per event.'

  broker_post=$(create_owner_post "$owner_jar" "Broker outage $TOKEN")
  RABBITMQ_CONTAINER_ID=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")
  docker stop "$RABBITMQ_CONTAINER_ID" >/dev/null
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 503
  api_for "$actor_jar" POST "/posts/$broker_post/comments" 201 '{"content":"written while broker stopped"}' read
  broker_comment=$(json_get data.id)
  api_for "$actor_jar" PUT "/posts/$broker_post/like" 204 '' read
  broker_comment_event=$(outbox_event_for_comment "$broker_comment")
  broker_like_event=$(outbox_event_for_like "$broker_post")
  wait_sql_equals "SELECT COUNT(*) FROM business_outbox WHERE event_id IN ('$broker_comment_event','$broker_like_event') AND status IN ('pending','leased');" 2 'broker outage did not preserve pending/leased Outbox rows'
  stop_pid "$BACKEND_PID"
  BACKEND_PID=
  start_backend
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/health" 200
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 503
  RABBITMQ_CONTAINER_ID=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")
  docker start "$RABBITMQ_CONTAINER_ID" >/dev/null
  wait_service_health rabbitmq
  verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT" >/dev/null
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
  wait_notification "$broker_comment_event" 1 45
  wait_notification "$broker_like_event" 1 45
  info 'Matrix 3-5/10 passed: broker outage preserved facts/Outbox, readiness degraded, and Backend restart plus broker recovery auto-completed delivery.'

  unacked_post=$second_post
  unacked_comment=$second_comment
  generated=$(generate_event comment.created "$actor_id" "$owner_id" "$unacked_post" "$unacked_comment")
  unacked_event=${generated%%$'\t'*}; unacked_payload=${generated#*$'\t'}
  validate_process_ownership "$WORKER_PID"
  kill -STOP -- "-$WORKER_PID"
  publish_message comment.created.v1 comment.created "$unacked_event" "$unacked_payload"
  wait_queue_at_least gopulse.business-worker.v1 messages_unacknowledged 1 'worker never held an unacked delivery' 15
  validate_process_ownership "$WORKER_PID"
  kill -KILL -- "-$WORKER_PID" 2>/dev/null || true
  wait "$WORKER_PID" 2>/dev/null || true
  WORKER_PID=
  start_worker
  wait_notification "$unacked_event" 1 30
  wait_queue_equals gopulse.business-worker.v1 messages_unacknowledged 0 'redelivered message remained unacked'
  info 'Matrix 6/10 passed: Worker restart redelivered an unacked message without a duplicate notification.'

  duplicate_payload=$unacked_payload
  publish_message comment.created.v1 comment.created "$unacked_event" "$duplicate_payload"
  publish_message comment.created.v1 comment.created "$unacked_event" "$duplicate_payload"
  wait_queue_equals gopulse.business-worker.v1 messages 0 'duplicate deliveries did not drain' 15
  wait_notification "$unacked_event" 1
  info 'Matrix 7/10 passed: duplicate event IDs were absorbed by the notification unique constraint.'

  generated=$(generate_event comment.created "$actor_id" "$owner_id" "$unacked_post" "$unacked_comment")
  transient_event=${generated%%$'\t'*}; transient_payload=${generated#*$'\t'}
  MYSQL_CONTAINER_ID=$(verify_service_ownership mysql 3306 "$MYSQL_PORT")
  docker exec "$MYSQL_CONTAINER_ID" mysql --user="$MYSQL_USER" --password="$MYSQL_PASSWORD" "$DATABASE_NAME" \
    --execute "START TRANSACTION; INSERT INTO notifications (source_event_id,type,recipient_id,actor_id,post_id,comment_id,created_at) VALUES ('$transient_event','comment.created',$owner_id,$actor_id,$unacked_post,$unacked_comment,NOW(6)); SELECT SLEEP(15); ROLLBACK;" >/dev/null 2>&1 &
  transient_lock_pid=$!
  sleep 1
  publish_message comment.created.v1 comment.created "$transient_event" "$transient_payload"
  wait_log_json "$TEMP_DIR/business-worker.log" 'temporary MySQL lock failure did not schedule a confirmed delayed retry' 10 \
    service=business-worker module=worker message='event retry scheduled' event_id="$transient_event" event_type=comment.created attempt=1 reason=retry_scheduled
  wait "$transient_lock_pid" 2>/dev/null || true
  wait_notification "$transient_event" 1 30
  dead_before=$(queue_metric gopulse.business-worker.dead.v1 messages 2>/dev/null || echo 0)
  publish_message comment.created.v1 comment.created "invalid-$TOKEN" 'not-json'
  wait_queue_at_least gopulse.business-worker.dead.v1 messages "$((dead_before + 1))" 'permanent invalid message did not enter the dead queue' 15
  generated=$(generate_event comment.created "$actor_id" "$owner_id" "$unacked_post" "$unacked_comment")
  follow_event=${generated%%$'\t'*}; follow_payload=${generated#*$'\t'}
  publish_message comment.created.v1 comment.created "$follow_event" "$follow_payload"
  wait_notification "$follow_event" 1 20
  info 'Matrix 8/10 passed: temporary failures retried, permanent poison entered dead queue, and a later valid message completed.'

  durable_post=$(create_owner_post "$owner_jar" "Rabbit durable $TOKEN")
  stop_pid "$WORKER_PID"
  WORKER_PID=
  api_for "$actor_jar" POST "/posts/$durable_post/comments" 201 '{"content":"persist across rabbit restart"}' read
  durable_comment=$(json_get data.id)
  durable_event=$(outbox_event_for_comment "$durable_comment")
  wait_queue_at_least gopulse.business-worker.v1 messages_ready 1 'durable message was not queued before RabbitMQ restart'
  rabbit_before=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  docker restart "$rabbit_before" >/dev/null
  wait_service_health rabbitmq
  RABBITMQ_CONTAINER_ID=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")
  [[ $RABBITMQ_CONTAINER_ID == "$rabbit_before" ]] || fail 'RabbitMQ restart changed the owned acceptance container identity'
  wait_queue_at_least gopulse.business-worker.v1 messages_ready 1 'durable queue/message did not survive RabbitMQ restart'
  start_worker
  wait_notification "$durable_event" 1 30
  info 'Matrix 9/10 passed: durable topology and persistent messages survived RabbitMQ container restart.'
}

run_api_flow() {
  local username="api_$TOKEN" password="acceptance-$TOKEN-password" first_post_id cursor encoded index
  api_request POST /auth/register 201 "{\"username\":\"$username\",\"password\":\"$password\"}" write
  assert_json "value['data']['username'] == '$username'"
  api_request GET /users/me 200 '' read
  api_request POST /auth/logout 204 '' write
  api_request GET /posts 401 '' read
  assert_json "value['error']['code'] == 'authentication_required'"
  api_request POST /auth/login 200 "{\"username\":\"$username\",\"password\":\"$password\"}" write

  for index in 1 2 3; do
    api_request POST /posts 201 "{\"title\":\"Acceptance post $index\",\"content\":\"Persistent content $index\"}" read
    [[ $index != 1 ]] || first_post_id=$(json_get data.id)
  done
  api_request GET '/posts?limit=2' 200 '' read
  assert_json "len(value['data']) == 2 and value['meta']['next_cursor'] is not None"
  cursor=$(json_get meta.next_cursor)
  encoded=$(urlencode "$cursor")
  api_request GET "/posts?limit=2&cursor=$encoded" 200 '' read
  assert_json "len(value['data']) >= 1"
  api_request GET "/posts/$first_post_id" 200 '' read

  for index in 1 2 3; do
    api_request POST "/posts/$first_post_id/comments" 201 "{\"content\":\"Acceptance comment $index\"}" read
  done
  api_request GET "/posts/$first_post_id/comments?limit=2" 200 '' read
  assert_json "len(value['data']) == 2 and value['meta']['next_cursor'] is not None"

  api_request PUT "/posts/$first_post_id/like" 204 '' read
  api_request PUT "/posts/$first_post_id/like" 204 '' read
  api_request GET "/posts/$first_post_id" 200 '' read
  assert_json "value['data']['like_count'] == 1 and value['data']['liked_by_me'] is True and value['data']['comment_count'] == 3"
  api_request DELETE "/posts/$first_post_id/like" 204 '' read
  api_request DELETE "/posts/$first_post_id/like" 204 '' read
  api_request GET "/posts/$first_post_id" 200 '' read
  assert_json "value['data']['like_count'] == 0 and value['data']['liked_by_me'] is False"
  printf '%s\n' "$first_post_id" >"$TEMP_DIR/first-post-id"
  printf '%s\n' "$username" >"$TEMP_DIR/api-username"
  printf '%s\n' "$password" >"$TEMP_DIR/api-password"
}

run_browser_flow() {
  info 'Running real Chromium page rendering and interaction acceptance.'
  (cd "$FRONTEND_DIR" && GOPULSE_BASE_URL="http://$PUBLISHED_HOST:$FRONTEND_PORT" GOPULSE_ACCEPTANCE_TOKEN="$TOKEN" npm run test:e2e)
}

es_request() {
  local method=$1 path=$2 expected=$3 body=${4:-} output="$TEMP_DIR/elasticsearch-response.json" status
  local -a args=(--silent --show-error --max-time 10 --output "$output" --write-out '%{http_code}')
  if [[ $method == HEAD ]]; then args+=(--head); else args+=(--request "$method"); fi
  [[ -z $body ]] || args+=(--header 'Content-Type: application/json' --data "$body")
  status=$(curl "${args[@]}" "http://$PUBLISHED_HOST:$ELASTICSEARCH_PORT$path")
  [[ $status == "$expected" ]] || { cat "$output" >&2 || true; fail "$method Elasticsearch $path returned HTTP $status, expected $expected"; }
}

active_search_index() {
  es_request GET "/_alias/gopulse-post-search-v1" 200
  python3 - "$TEMP_DIR/elasticsearch-response.json" <<'PYINDEX'
import json, re, sys
value=json.load(open(sys.argv[1], encoding='utf-8'))
if len(value) != 1:
    raise SystemExit('search alias must resolve to exactly one index')
index=next(iter(value))
if not re.fullmatch(r'gopulse-post-search-v1-[a-z0-9-]+', index):
    raise SystemExit(f'unsafe physical index name: {index}')
print(index)
PYINDEX
}

run_search_browser_flow() {
  local username=$1 password=$2 query=$3 title=$4
  info 'Running the targeted search-rebuild browser acceptance.'
  (cd "$FRONTEND_DIR" && \
    GOPULSE_BASE_URL="http://$PUBLISHED_HOST:$FRONTEND_PORT" \
    GOPULSE_ACCEPTANCE_TOKEN="$TOKEN" \
    GOPULSE_SEARCH_USERNAME="$username" GOPULSE_SEARCH_PASSWORD="$password" \
    GOPULSE_SEARCH_QUERY="$query" GOPULSE_SEARCH_TITLE="$title" \
    npm run test:e2e -- --grep search-rebuild)
}

run_search_rebuild_flow() {
  local username="search_$TOKEN" password="search-$TOKEN-password"
  local title_term="historicaltitle$TOKEN" body_term="historicalbody$TOKEN" page_term="searchbatch$TOKEN"
  local title="Historical search rebuild $TOKEN" first_post_id cursor encoded first_page_ids second_page_ids
  local active_index unrelated_index="gopulse-acceptance-$TOKEN-unrelated"

  api_request POST /auth/register 201 "$(printf '{"username":"%s","password":"%s"}' "$username" "$password")" write
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "$title $title_term $page_term" "MySQL hydration $body_term $page_term")" read
  first_post_id=$(json_get data.id)
  api_request POST "/posts/$first_post_id/comments" 201 '{"content":"search hydration comment one"}' read
  api_request POST "/posts/$first_post_id/comments" 201 '{"content":"search hydration comment two"}' read
  api_request PUT "/posts/$first_post_id/like" 204 '' read
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Second historical result $page_term" 'secondary searchable body')" read
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Third historical result $page_term" 'third searchable body')" read

  run_search_reindex
  api_request GET "/search/posts?q=$(urlencode "$title_term")&limit=20" 200 '' read
  assert_json "len(value['data']) == 1 and value['data'][0]['id'] == $first_post_id and value['data'][0]['title'].startswith('Historical search rebuild') and value['data'][0]['comment_count'] == 2 and value['data'][0]['like_count'] == 1 and value['data'][0]['liked_by_me'] is True"
  api_request GET "/search/posts?q=$(urlencode "$body_term")&limit=20" 200 '' read
  assert_json "len(value['data']) == 1 and value['data'][0]['id'] == $first_post_id"
  api_request GET "/search/posts?q=$(urlencode "unrelated$TOKEN")&limit=20" 200 '' read
  assert_json "len(value['data']) == 0 and value['meta']['next_cursor'] is None"

  api_request GET "/search/posts?q=$(urlencode "$page_term")&limit=2" 200 '' read
  assert_json "len(value['data']) == 2 and value['meta']['next_cursor'] is not None"
  first_page_ids=$(python3 - "$RESPONSE_FILE" <<'PYIDS'
import json, sys
print(','.join(str(item['id']) for item in json.load(open(sys.argv[1], encoding='utf-8'))['data']))
PYIDS
)
  cursor=$(json_get meta.next_cursor)
  encoded=$(urlencode "$cursor")
  api_request GET "/search/posts?q=$(urlencode "other$page_term")&limit=2&cursor=$encoded" 400 '' read
  assert_json "value['error']['code'] == 'validation_failed'"
  api_request GET "/search/posts?q=$(urlencode "$page_term")&limit=2&cursor=$encoded" 200 '' read
  assert_json "len(value['data']) == 1"
  second_page_ids=$(python3 - "$RESPONSE_FILE" <<'PYIDS'
import json, sys
print(','.join(str(item['id']) for item in json.load(open(sys.argv[1], encoding='utf-8'))['data']))
PYIDS
)
  [[ ",$first_page_ids," != *",$second_page_ids,"* ]] || fail 'search pagination returned a duplicate post'
  api_request GET '/search/posts?q=%20%20&limit=20' 400 '' read
  assert_json "value['error']['code'] == 'validation_failed'"
  api_request GET "/search/posts?q=$(urlencode "$page_term")&limit=51" 400 '' read
  assert_json "value['error']['code'] == 'validation_failed'"
  api_request GET "/search/posts?q=$(urlencode "$page_term")&cursor=not-a-cursor" 400 '' read
  assert_json "value['error']['code'] == 'validation_failed'"
  api_request GET "/search/posts?q=$(urlencode "$page_term")&limit=50" 200 '' read
  assert_json "len(value['data']) <= 50"

  active_index=$(active_search_index)
  es_request PUT "/$unrelated_index" 200 '{}'
  [[ $active_index =~ ^gopulse-post-search-v1-[a-z0-9-]+$ ]] || fail 'refusing to delete an unowned search index'
  es_request DELETE "/$active_index" 200
  api_request GET "/search/posts?q=$(urlencode "$title_term")&limit=20" 503 '' read
  assert_json "value['error']['code'] == 'search_unavailable' and '9200' not in value['error']['message'] and 'gopulse-post-search' not in value['error']['message']"

  run_search_reindex
  api_request GET "/search/posts?q=$(urlencode "$page_term")&limit=2&cursor=$encoded" 400 '' read
  assert_json "value['error']['code'] == 'validation_failed'"
  api_request GET "/search/posts?q=$(urlencode "$title_term")&limit=20" 200 '' read
  assert_json "len(value['data']) == 1 and value['data'][0]['id'] == $first_post_id"
  es_request HEAD "/$unrelated_index" 200
  run_search_browser_flow "$username" "$password" "$title_term" "$title $title_term $page_term"
  info 'Targeted search rebuild acceptance passed: historical posts recovered and the unrelated index remained intact.'
}

wait_search_post() {
  local query=$1 post_id=$2 description=$3 deadline=$((SECONDS + ${4:-45})) status
  while ((SECONDS < deadline)); do
    status=$(curl --silent --show-error --max-time 5 --cookie "$COOKIE_JAR" --output "$RESPONSE_FILE" --write-out '%{http_code}' \
      "http://$PUBLISHED_HOST:$HTTP_PORT/api/v1/search/posts?q=$(urlencode "$query")&limit=20" 2>/dev/null || true)
    if [[ $status == 200 ]] && python3 - "$RESPONSE_FILE" "$post_id" <<'PYSEARCH'
import json, sys
try:
    payload=json.load(open(sys.argv[1], encoding='utf-8'))
    wanted=int(sys.argv[2])
    found=sum(1 for item in payload.get('data', []) if item.get('id') == wanted)
except Exception:
    raise SystemExit(1)
raise SystemExit(0 if found == 1 else 1)
PYSEARCH
    then
      return 0
    fi
    sleep 0.5
  done
  fail "$description"
}

assert_topology_bindings() {
  local queue=$1 expected=$2 forbidden=$3 response status
  response="$TEMP_DIR/${queue}.bindings.json"
  status=$(curl --silent --show-error --max-time 5 --user "$RABBITMQ_USER:$RABBITMQ_PASSWORD" \
    --output "$response" --write-out '%{http_code}' \
    "http://$PUBLISHED_HOST:$RABBITMQ_MANAGEMENT_PORT/api/queues/%2F/$queue/bindings")
  [[ $status == 200 ]] || { fail "could not inspect bindings for $queue"; return 1; }
  python3 - "$response" "$expected" "$forbidden" <<'PYBIND'
import json, sys
bindings=json.load(open(sys.argv[1], encoding='utf-8'))
keys={item.get('routing_key') for item in bindings if item.get('source')}
expected=set(filter(None, sys.argv[2].split(',')))
forbidden=set(filter(None, sys.argv[3].split(',')))
if not expected.issubset(keys) or keys.intersection(forbidden):
    raise SystemExit(f'binding mismatch: keys={sorted(keys)} expected={sorted(expected)} forbidden={sorted(forbidden)}')
PYBIND
}

run_search_live_flow() {
  local username="live_$TOKEN" password="live-$TOKEN-password"
  local first_term="incremental$TOKEN" paused_term="paused$TOKEN" broker_term="broker$TOKEN" elastic_term="elastic$TOKEN" concurrent_term="concurrent$TOKEN"
  local first_id paused_id broker_id elastic_id concurrent_id event_id payload rabbit_id reindex_pid

  api_request POST /auth/register 201 "$(printf '{"username":"%s","password":"%s"}' "$username" "$password")" write
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Live $first_term" 'automatic index')" read
  first_id=$(json_get data.id)
  wait_sql_equals "SELECT COUNT(*) FROM business_outbox WHERE event_type='post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$first_id' AND JSON_EXTRACT(payload, '$.recipient_id') IS NULL AND JSON_EXTRACT(payload, '$.title') IS NULL;" 1 'post.created outbox was not atomically recorded'
  wait_search_post "$first_term" "$first_id" 'normal incremental post did not become searchable'
  assert_topology_bindings gopulse.business-worker.v1 'comment.created.v1,post.liked.v1' 'post.created.v1'
  assert_topology_bindings gopulse.search-indexer.v1 'post.created.v1' 'comment.created.v1,post.liked.v1'

  stop_pid "$SEARCH_INDEXER_PID"
  SEARCH_INDEXER_PID=
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Paused $paused_term" 'retained in search queue')" read
  paused_id=$(json_get data.id)
  wait_queue_at_least gopulse.search-indexer.v1 messages_ready 1 'stopped Search Indexer did not retain the durable message'
  start_search_indexer
  wait_search_post "$paused_term" "$paused_id" 'restarted Search Indexer did not converge the retained post'

  event_id=$(mysql_query "SELECT event_id FROM business_outbox WHERE event_type='post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$first_id' ORDER BY id DESC LIMIT 1;")
  payload=$(mysql_query "SELECT CAST(payload AS CHAR) FROM business_outbox WHERE event_id='$event_id';")
  publish_message post.created.v1 post.created "$event_id" "$payload" 0 gopulse.search.v1
  publish_message post.created.v1 post.created "$event_id" "$payload" 0 gopulse.search.v1
  wait_queue_equals gopulse.search-indexer.v1 messages 0 'duplicate search deliveries did not drain' 30
  wait_search_post "$first_term" "$first_id" 'duplicate delivery changed the logical search result'

  rabbit_id=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  docker stop "$rabbit_id" >/dev/null
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Broker $broker_term" 'outbox remains pending')" read
  broker_id=$(json_get data.id)
  wait_sql_equals "SELECT COUNT(*) FROM business_outbox WHERE event_type='post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$broker_id' AND status <> 'published';" 1 'RabbitMQ outage did not retain the post outbox event'
  docker start "$rabbit_id" >/dev/null
  wait_service_health rabbitmq
  RABBITMQ_CONTAINER_ID=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  wait_search_post "$broker_term" "$broker_id" 'RabbitMQ recovery did not publish and index the retained post' 60

  docker stop "$ELASTICSEARCH_CONTAINER_ID" >/dev/null
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Elastic $elastic_term" 'retry after outage')" read
  elastic_id=$(json_get data.id)
  api_request GET "/search/posts?q=$(urlencode "$elastic_term")&limit=20" 503 '' read
  docker start "$ELASTICSEARCH_CONTAINER_ID" >/dev/null
  wait_service_health elasticsearch
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")
  wait_search_post "$elastic_term" "$elastic_id" 'Elasticsearch recovery did not converge the retried post' 60

  run_search_reindex &
  reindex_pid=$!
  api_request POST /posts 201 "$(printf '{"title":"%s","content":"%s"}' "Concurrent $concurrent_term" 'created during representative rebuild')" read
  concurrent_id=$(json_get data.id)
  wait "$reindex_pid"
  wait_search_post "$concurrent_term" "$concurrent_id" 'concurrent rebuild and incremental indexing omitted the new post' 60
  (cd "$FRONTEND_DIR" && \
    GOPULSE_BASE_URL="http://$PUBLISHED_HOST:$FRONTEND_PORT" \
    GOPULSE_ACCEPTANCE_TOKEN="$TOKEN" \
    GOPULSE_SEARCH_USERNAME="$username" GOPULSE_SEARCH_PASSWORD="$password" \
    GOPULSE_SEARCH_QUERY="$first_term" GOPULSE_SEARCH_TITLE="Live $first_term" \
    npm run test:e2e -- --grep search-live)
  info 'Incremental search acceptance passed: topology isolation, atomic Outbox, pause/restart, duplicate, broker, Elasticsearch, rebuild cooperation, and browser observation converged.'
}

verify_restart_and_cache() {
  local post_id redis_id
  post_id=$(cat "$TEMP_DIR/first-post-id")
  stop_pid "$BACKEND_PID"
  BACKEND_PID=
  start_backend
  api_request GET /users/me 200 '' read
  api_request GET "/posts/$post_id" 200 '' read

  redis_id=$(verify_service_ownership redis 6379 "$REDIS_PORT")
  docker exec "$redis_id" redis-cli --no-auth-warning --raw -a "$REDIS_PASSWORD" -n "$REDIS_DB" FLUSHDB | grep -qx OK
  api_request GET "/posts/$post_id" 200 '' read
  [[ $(docker exec "$redis_id" redis-cli --no-auth-warning --raw -a "$REDIS_PASSWORD" -n "$REDIS_DB" EXISTS "gopulse:post:detail:v1:$post_id") == 1 ]] || fail 'post detail cache was not rebuilt after the acceptance Redis DB was cleared'
}

verify_redis_failure_and_recovery() {
  local post_id redis_id username="fault_$TOKEN" password="fault-$TOKEN-password" fault_post_id
  post_id=$(cat "$TEMP_DIR/first-post-id")
  redis_id=$(verify_service_ownership redis 6379 "$REDIS_PORT")
  docker stop "$redis_id" >/dev/null
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 503

  rm -f "$COOKIE_JAR"
  api_request POST /auth/register 201 "{\"username\":\"$username\",\"password\":\"$password\"}" write
  api_request GET /users/me 200 '' read
  api_request POST /posts 201 '{"title":"Redis unavailable","content":"MySQL remains authoritative"}' read
  fault_post_id=$(json_get data.id)
  api_request GET /posts 200 '' read
  api_request GET "/posts/$fault_post_id" 200 '' read
  api_request POST "/posts/$fault_post_id/comments" 201 '{"content":"Still writable"}' read
  api_request PUT "/posts/$fault_post_id/like" 204 '' read
  api_request DELETE "/posts/$fault_post_id/like" 204 '' read
  api_request POST /auth/logout 204 '' write
  api_request POST /auth/login 200 "{\"username\":\"$username\",\"password\":\"$password\"}" write

  docker start "$redis_id" >/dev/null
  wait_service_health redis
  verify_service_ownership redis 6379 "$REDIS_PORT" >/dev/null
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
  api_request GET "/posts/$post_id" 200 '' read
  [[ $(docker exec "$redis_id" redis-cli --no-auth-warning --raw -a "$REDIS_PASSWORD" -n "$REDIS_DB" EXISTS "gopulse:post:detail:v1:$post_id") == 1 ]] || fail 'cache did not recover after Redis restart'
}


expect_log() {
  local message=$1 request_id=$2
  shift 2
  python3 - "$LOG_EXPECTATIONS" "$message" "$request_id" "$@" <<'PYEXPECT'
import json, sys
path, message, request_id, *pairs = sys.argv[1:]
value={'message': message, 'request_id': request_id}
for pair in pairs:
    key, raw=pair.split('=', 1)
    value[key]=int(raw) if raw.isdigit() else raw
with open(path, 'a', encoding='utf-8') as stream:
    stream.write(json.dumps(value, separators=(',', ':')) + '\n')
PYEXPECT
}

validate_logging_live() {
  local sensitive_file="$TEMP_DIR/log-sensitive-values.txt"
  {
    printf '%s\n' "$LOG_USERNAME" "$LOG_PASSWORD" "$LOG_TITLE" "$LOG_CONTENT" "$LOG_COMMENT" "$LOG_SEARCH" "$LOG_FORGED_ID"
    awk 'NF >= 7 && ($0 ~ /^#HttpOnly_/ || $0 !~ /^#/) {print $NF}' "$TEMP_DIR"/*.cookies 2>/dev/null || true
  } >"$sensitive_file"
  python3 - "$TEMP_DIR/backend.log" "$REQUEST_TRACE" "$LOG_EXPECTATIONS" "$sensitive_file" <<'PYLOG'
import datetime, json, re, sys
log_path, trace_path, expectation_path, sensitive_path = sys.argv[1:]
records=[]
for number, raw in enumerate(open(log_path, encoding='utf-8'), 1):
    line=raw.rstrip('\n')
    if not line:
        continue
    try:
        value=json.loads(line)
    except json.JSONDecodeError:
        raise SystemExit(f'backend log line {number} is not Schema v1 JSON: {line!r}')
    records.append(value)
if not records:
    raise SystemExit('backend produced no JSON records')
for value in records:
    for key in ('log_schema_version','timestamp','level','service','module','message'):
        if key not in value:
            raise SystemExit(f'log record missing {key}: {value!r}')
    if value['log_schema_version'] != 1 or value['service'] != 'backend' or value['level'] not in ('info','warn','error'):
        raise SystemExit(f'invalid fixed log fields: {value!r}')
    parsed=datetime.datetime.fromisoformat(value['timestamp'].replace('Z','+00:00'))
    if parsed.utcoffset() != datetime.timedelta(0):
        raise SystemExit(f'non-UTC timestamp: {value!r}')

traces=[]
for line in open(trace_path, encoding='utf-8'):
    method, path, status, request_id=line.rstrip('\n').split('\t')
    if not re.fullmatch(r'[a-f0-9]{32}', request_id):
        raise SystemExit(f'invalid captured request ID: {request_id!r}')
    traces.append((method,path,int(status),request_id))
if len({item[3] for item in traces}) != len(traces):
    raise SystemExit('request IDs were reused')
completed=[value for value in records if value.get('message') == 'http request completed']
for method, path, status, request_id in traces:
    matches=[value for value in completed if value.get('request_id') == request_id]
    if len(matches) != 1:
        raise SystemExit(f'{method} {path} has {len(matches)} completion records')
    value=matches[0]
    expected_level='error' if status >= 500 else 'warn' if status >= 400 else 'info'
    if value.get('method') != method or value.get('status') != status or value.get('level') != expected_level:
        raise SystemExit(f'incorrect completion record for {method} {path}: {value!r}')
    if not isinstance(value.get('duration_ms'), int) or not isinstance(value.get('response_bytes'), int):
        raise SystemExit(f'non-integer duration/size: {value!r}')
    if value.get('route') in (None, '') or '?' in value['route']:
        raise SystemExit(f'unsafe route field: {value!r}')

for expected in map(json.loads, open(expectation_path, encoding='utf-8')):
    matches=[]
    for value in records:
        if all(value.get(key) == wanted for key, wanted in expected.items()):
            matches.append(value)
    if len(matches) != 1:
        raise SystemExit(f'expected exactly one matching business/cache log, found {len(matches)}: {expected!r}')
    request_id=expected['request_id']
    if sum(1 for value in completed if value.get('request_id') == request_id) != 1:
        raise SystemExit(f'expected log is not correlated to one completion: {expected!r}')

for status, code in ((400,'validation_failed'),(401,'authentication_required'),(404,'post_not_found'),(503,'search_unavailable')):
    candidates=[value for value in completed if value.get('status') == status and value.get('error_code') == code]
    if not candidates:
        raise SystemExit(f'missing safe {status}/{code} completion')

contents=open(log_path, encoding='utf-8').read()
for raw in open(sensitive_path, encoding='utf-8'):
    secret=raw.strip()
    if secret and secret in contents:
        raise SystemExit(f'sensitive sentinel leaked into backend log: {secret!r}')
print(f'Validated {len(records)} backend JSON records and {len(traces)} correlated requests.')
PYLOG
  validate_phase4_process_logs "$sensitive_file"
}

validate_phase4_process_logs() {
  local extra_sensitive=${1:-}
  python3 - "$TEMP_DIR/backend.log" "$TEMP_DIR/business-worker.log" "$TEMP_DIR/search-indexer.log" "$TEMP_DIR/search-reindex.log" "$ACCEPTANCE_ENV" "$extra_sensitive" <<'PYPHASE4LOG'
import datetime, json, os, sys
backend_path, worker_path, indexer_path, reindex_path, env_path, extra_path = sys.argv[1:]
specs = {
    backend_path: 'backend',
    worker_path: 'business-worker',
    indexer_path: 'search-indexer',
    reindex_path: 'search-reindex',
}
records_by_service = {}
all_text = []
for path, service in specs.items():
    records = []
    try:
        stream = open(path, encoding='utf-8')
    except OSError as exc:
        raise SystemExit(f'missing {service} log: {exc}')
    with stream:
        for number, raw in enumerate(stream, 1):
            line = raw.rstrip('\n')
            if not line:
                continue
            all_text.append(line)
            try:
                value = json.loads(line)
            except json.JSONDecodeError:
                raise SystemExit(f'{service} log line {number} is not JSON: {line!r}')
            for key in ('log_schema_version','timestamp','level','service','module','message'):
                if key not in value:
                    raise SystemExit(f'{service} record missing {key}: {value!r}')
            if value['log_schema_version'] != 1 or value['service'] != service or value['level'] not in ('info','warn','error'):
                raise SystemExit(f'{service} record has invalid fixed fields: {value!r}')
            parsed = datetime.datetime.fromisoformat(value['timestamp'].replace('Z','+00:00'))
            if parsed.utcoffset() != datetime.timedelta(0):
                raise SystemExit(f'{service} record timestamp is not UTC: {value!r}')
            forbidden = {'payload','envelope','headers','body','query','url','dsn','password','token','cookie','authorization','pit_id','index_name'}
            if forbidden.intersection(value):
                raise SystemExit(f'{service} record contains forbidden fields: {value!r}')
            records.append(value)
    if not records:
        raise SystemExit(f'{service} produced no JSON records')
    records_by_service[service] = records

backend = records_by_service['backend']
worker = records_by_service['business-worker']
indexer = records_by_service['search-indexer']
reindex = records_by_service['search-reindex']

def select(records, message):
    return [value for value in records if value.get('message') == message]

published = select(backend, 'outbox event published')
worker_events = select(worker, 'event processed') + select(worker, 'event ignored') + select(worker, 'event retry scheduled') + select(worker, 'event dead lettered')
indexer_events = select(indexer, 'event processed') + select(indexer, 'event retry scheduled') + select(indexer, 'event dead lettered')
if not published or not worker_events or not indexer_events:
    raise SystemExit('missing Outbox/Worker/Indexer event lifecycle records')
if not select(reindex, 'search reindex started') or not (select(reindex, 'search reindex completed') or select(reindex, 'search reindex skipped')):
    raise SystemExit('missing search-reindex lifecycle records')
for value in published:
    if not isinstance(value.get('outbox_id'), int) or not value.get('event_id') or not value.get('event_type') or 'request_id' in value:
        raise SystemExit(f'invalid Outbox event record: {value!r}')
for value in worker_events + indexer_events:
    if not value.get('event_id') or not value.get('event_type') or not isinstance(value.get('attempt'), int) or not value.get('reason') or 'request_id' in value:
        raise SystemExit(f'invalid worker event record: {value!r}')
for value in select(indexer, 'event processed'):
    if not isinstance(value.get('post_id'), int):
        raise SystemExit(f'search success record is missing post_id: {value!r}')
published_ids = {value['event_id'] for value in published}
if not published_ids.intersection(value['event_id'] for value in worker_events):
    raise SystemExit('Outbox and Business Worker logs have no correlated event ID')
if not published_ids.intersection(value['event_id'] for value in indexer_events):
    raise SystemExit('Outbox and Search Indexer logs have no correlated event ID')
for records in (worker, indexer):
    for message in ('connection unavailable','session interrupted'):
        if len(select(records, message)) > 20:
            raise SystemExit(f'unbounded reconnect logging for {message!r}')

sensitive = []
for raw in open(env_path, encoding='utf-8'):
    line = raw.strip()
    if not line or line.startswith('#') or '=' not in line:
        continue
    key, value = line.split('=', 1)
    if any(marker in key for marker in ('PASSWORD','SECRET','COOKIE_NAME','URL')) and value:
        sensitive.append(value)
if extra_path and os.path.exists(extra_path):
    sensitive.extend(raw.strip() for raw in open(extra_path, encoding='utf-8') if raw.strip())
contents = '\n'.join(all_text)
for secret in sensitive:
    if secret and secret in contents:
        raise SystemExit(f'sensitive value leaked into application logs: {secret!r}')
print(f'Validated Phase 4 process logs: backend={len(backend)}, worker={len(worker)}, indexer={len(indexer)}, reindex={len(reindex)}.')
PYPHASE4LOG
}

run_logging_live_flow() {
  LOGGING_CAPTURE=1
  LOG_USERNAME="loguser_$TOKEN"
  LOG_PASSWORD="logpass-$TOKEN-sensitive"
  LOG_TITLE="log-title-$TOKEN-sensitive"
  LOG_CONTENT="log-content-$TOKEN-sensitive"
  LOG_COMMENT="log-comment-$TOKEN-sensitive"
  LOG_SEARCH="logsearch$TOKEN"
  LOG_FORGED_ID="ffffffffffffffffffffffffffffffff"
  local owner_jar="$TEMP_DIR/log-owner.cookies" actor_jar="$TEMP_DIR/log-actor.cookies"
  local owner_id actor_id post_id comment_id notification_id comment_event post_event self_event self_payload generated

  CLIENT_REQUEST_ID=$LOG_FORGED_ID
  api_for "$owner_jar" POST /auth/register 201 "{\"username\":\"$LOG_USERNAME\",\"password\":\"$LOG_PASSWORD\"}" write
  CLIENT_REQUEST_ID=
  [[ $LAST_REQUEST_ID != "$LOG_FORGED_ID" ]] || fail 'Backend reused the client-supplied request ID'
  owner_id=$(json_get data.id)
  expect_log 'user registered' "$LAST_REQUEST_ID" "user_id=$owner_id"
  api_for "$owner_jar" GET /users/me 200 '' read
  api_for "$owner_jar" POST /auth/logout 204 '' write
  expect_log 'user logged out' "$LAST_REQUEST_ID"
  api_for "$owner_jar" GET /posts 401 '' read
  api_for "$owner_jar" POST /auth/login 200 "{\"username\":\"$LOG_USERNAME\",\"password\":\"$LOG_PASSWORD\"}" write
  expect_log 'user logged in' "$LAST_REQUEST_ID" "user_id=$owner_id"
  api_for "$owner_jar" POST /posts 201 "{\"title\":\"$LOG_TITLE $LOG_SEARCH\",\"content\":\"$LOG_CONTENT\"}" read
  post_id=$(json_get data.id)
  expect_log 'post created' "$LAST_REQUEST_ID" "user_id=$owner_id" "post_id=$post_id"
  post_event=$(outbox_event_for_post "$post_id")
  wait_log_json "$TEMP_DIR/backend.log" 'Backend did not log the published post event' 20 \
    service=backend module=outbox message='outbox event published' event_id="$post_event" event_type=post.created
  wait_log_json "$TEMP_DIR/search-indexer.log" 'Search Indexer did not log the processed post event' 45 \
    service=search-indexer module=search message='event processed' event_id="$post_event" event_type=post.created attempt=0 reason=processed post_id="$post_id"
  api_for "$owner_jar" GET '/posts?limit=20' 200 '' read
  api_for "$owner_jar" GET "/posts/$post_id" 200 '' read

  api_for "$actor_jar" POST /auth/register 201 "{\"username\":\"actor_$TOKEN\",\"password\":\"$LOG_PASSWORD\"}" write
  actor_id=$(json_get data.id)
  expect_log 'user registered' "$LAST_REQUEST_ID" "user_id=$actor_id"
  api_for "$actor_jar" POST "/posts/$post_id/comments" 201 "{\"content\":\"$LOG_COMMENT\"}" read
  comment_id=$(json_get data.id)
  expect_log 'comment created' "$LAST_REQUEST_ID" "user_id=$actor_id" "post_id=$post_id" "comment_id=$comment_id"
  api_for "$actor_jar" GET "/posts/$post_id/comments?limit=20" 200 '' read
  api_for "$actor_jar" PUT "/posts/$post_id/like" 204 '' read
  expect_log 'post liked' "$LAST_REQUEST_ID" "user_id=$actor_id" "post_id=$post_id"
  api_for "$actor_jar" DELETE "/posts/$post_id/like" 204 '' read
  expect_log 'post unliked' "$LAST_REQUEST_ID" "user_id=$actor_id" "post_id=$post_id"

  local previous_jar=$COOKIE_JAR
  COOKIE_JAR=$actor_jar
  wait_search_post "$LOG_SEARCH" "$post_id" 'logging acceptance post was not indexed' 45
  api_request GET "/search/posts?q=$(urlencode "$LOG_SEARCH")&limit=20" 200 '' read
  COOKIE_JAR=$previous_jar

  comment_event=$(outbox_event_for_comment "$comment_id")
  wait_notification "$comment_event" 1 45
  wait_log_json "$TEMP_DIR/backend.log" 'Backend did not log the published comment event' 20 \
    service=backend module=outbox message='outbox event published' event_id="$comment_event" event_type=comment.created
  wait_log_json "$TEMP_DIR/business-worker.log" 'Business Worker did not log the processed comment event' 20 \
    service=business-worker module=worker message='event processed' event_id="$comment_event" event_type=comment.created attempt=0 reason=processed

  generated=$(generate_event comment.created "$owner_id" "$owner_id" "$post_id" "$comment_id")
  self_event=${generated%%$'\t'*}
  self_payload=${generated#*$'\t'}
  publish_message comment.created.v1 comment.created "$self_event" "$self_payload"
  wait_log_json "$TEMP_DIR/business-worker.log" 'Business Worker did not log the ignored self event' 20 \
    service=business-worker module=worker message='event ignored' event_id="$self_event" event_type=comment.created attempt=0 reason=self_event
  wait_notification "$self_event" 0 5
  api_for "$owner_jar" GET /notifications 200 '' read
  notification_id=$(json_get data.0.id)
  api_for "$owner_jar" PATCH "/notifications/$notification_id/read" 204 '' read
  expect_log 'notification marked read' "$LAST_REQUEST_ID" "user_id=$owner_id" "notification_id=$notification_id"

  api_for "$actor_jar" POST /posts 400 '{"title":"","content":"invalid"}' read
  api_for "$actor_jar" GET /posts/999999999 404 '' read

  local elasticsearch_id redis_id
  elasticsearch_id=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")
  docker stop "$elasticsearch_id" >/dev/null
  api_for "$actor_jar" GET "/search/posts?q=$(urlencode "$LOG_SEARCH")&limit=20" 503 '' read
  docker start "$elasticsearch_id" >/dev/null
  wait_service_health elasticsearch
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")

  redis_id=$(verify_service_ownership redis 6379 "$REDIS_PORT")
  docker stop "$redis_id" >/dev/null
  api_for "$actor_jar" GET "/posts/$post_id" 200 '' read
  expect_log 'post detail cache read failed' "$LAST_REQUEST_ID" "post_id=$post_id" 'reason=cache_unavailable'
  docker start "$redis_id" >/dev/null
  wait_service_health redis
  verify_service_ownership redis 6379 "$REDIS_PORT" >/dev/null

  LOGGING_CAPTURE=0
  validate_logging_live
}

cleanup() {
  local status=$?
  ((CLEANUP_DONE == 0)) || return "$status"
  CLEANUP_DONE=1
  set +e
  stop_pid "$FRONTEND_PID"
  stop_pid "$SEARCH_INDEXER_PID"
  stop_pid "$WORKER_PID"
  stop_pid "$BACKEND_PID"
  if ((RESOURCES_STARTED == 1)) && valid_token "$TOKEN" && [[ $PROJECT_NAME == "gopulse-acceptance-$TOKEN" ]] && valid_project "$PROJECT_NAME"; then
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  if ((SNAPSHOT_READY == 1)); then
    snapshot_development_state "$TEMP_DIR/development-after.txt" || true
    if ! cmp -s "$TEMP_DIR/development-before.txt" "$TEMP_DIR/development-after.txt"; then
      printf '[gopulse-acceptance] ERROR: daily development stack state changed during isolated acceptance.\n' >&2
      diff -u "$TEMP_DIR/development-before.txt" "$TEMP_DIR/development-after.txt" >&2 || true
      [[ $status != 0 ]] || status=1
    fi
  fi
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
  trap - EXIT
  exit "$status"
}

on_signal() { exit "$1"; }
trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

main() {
  local mode=full
  if [[ ${1:-} == --self-test ]]; then
    self_test
    return
  elif [[ ${1:-} == --search-rebuild ]]; then
    mode=search-rebuild
    shift
  elif [[ ${1:-} == --search-live ]]; then
    mode=search-live
    shift
  elif [[ ${1:-} == --logging-live ]]; then
    mode=logging-live
    shift
  fi
  [[ $# == 0 ]] || { fail 'usage: verify-business.sh [--self-test|--search-rebuild|--search-live|--logging-live]'; return 2; }
  require_tools
  TOKEN=${ACCEPTANCE_TOKEN:-$(python3 -c 'import secrets; print(secrets.token_hex(6))')}
  PROJECT_NAME="gopulse-acceptance-$TOKEN"
  DATABASE_NAME="gopulse_acceptance_$TOKEN"
  generate_ports
  validate_target "$TOKEN" "$PROJECT_NAME" "$DATABASE_NAME" "$PUBLISHED_HOST" "$MYSQL_PORT" "$REDIS_PORT" "$RABBITMQ_PORT" "$RABBITMQ_MANAGEMENT_PORT" "$ELASTICSEARCH_PORT" "$HTTP_PORT" "$FRONTEND_PORT"

  TEMP_DIR=$(mktemp -d -t gopulse-acceptance-XXXXXXXX)
  ACCEPTANCE_ENV="$TEMP_DIR/acceptance.env"
  COOKIE_JAR="$TEMP_DIR/cookies.txt"
  RESPONSE_FILE="$TEMP_DIR/response.json"
  RESPONSE_HEADERS_FILE="$TEMP_DIR/response.headers"
  LOG_EXPECTATIONS="$TEMP_DIR/log-expectations.jsonl"
  REQUEST_TRACE="$TEMP_DIR/request-trace.tsv"
  : >"$LOG_EXPECTATIONS"
  : >"$REQUEST_TRACE"
  snapshot_development_state "$TEMP_DIR/development-before.txt"
  SNAPSHOT_READY=1
  write_environment

  assert_project_absent
  info "Starting isolated project $PROJECT_NAME with database $DATABASE_NAME."
  RESOURCES_STARTED=1
  compose up --detach
  wait_service_health mysql
  wait_service_health redis
  wait_service_health rabbitmq
  wait_service_health elasticsearch
  MYSQL_CONTAINER_ID=$(verify_service_ownership mysql 3306 "$MYSQL_PORT")
  verify_service_ownership redis 6379 "$REDIS_PORT" >/dev/null
  RABBITMQ_CONTAINER_ID=$(verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT")
  ELASTICSEARCH_CONTAINER_ID=$(verify_service_ownership elasticsearch 9200 "$ELASTICSEARCH_PORT")

  backend_environment bash -c 'cd "$1" && go run ./cmd/migrate up' _ "$BACKEND_DIR"
  (cd "$BACKEND_DIR" && go build -o "$TEMP_DIR/gopulse-backend" ./cmd/server && go build -o "$TEMP_DIR/gopulse-business-worker" ./cmd/business-worker && go build -o "$TEMP_DIR/gopulse-search-indexer" ./cmd/search-indexer && go build -o "$TEMP_DIR/gopulse-search-reindex" ./cmd/search-reindex)

  if [[ $mode == logging-live ]]; then
    run_search_reindex --if-missing
    BACKEND_APP_ENV=development
    start_backend
    start_worker
    start_search_indexer
    wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
    run_logging_live_flow
    info 'Isolated HTTP and business logging acceptance passed; cleanup will now remove only verified acceptance resources.'
    return
  fi

  if [[ $mode == search-live ]]; then
    run_search_reindex --if-missing
    start_backend
    start_search_indexer
    start_frontend
    wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
    run_search_live_flow
    info 'Isolated incremental search acceptance passed; cleanup will now remove only verified acceptance resources.'
    return
  fi

  if [[ $mode == search-rebuild ]]; then
    start_backend
    start_frontend
    wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
    run_search_rebuild_flow
    wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
    info 'Isolated search rebuild acceptance passed; cleanup will now remove only verified acceptance resources.'
    return
  fi

  run_search_reindex --if-missing
  start_backend
  start_worker
  start_search_indexer
  start_frontend
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200

  run_api_flow
  run_browser_flow
  run_search_rebuild_flow
  run_search_live_flow
  run_reliability_matrix
  verify_restart_and_cache
  verify_redis_failure_and_recovery
  api_request GET /users/me 200 '' read
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/health" 200
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
  validate_phase4_process_logs
  info 'Complete isolated business acceptance passed; cleanup will now remove only verified acceptance resources.'
}

main "$@"
