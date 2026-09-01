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

info() { printf '[gopulse-acceptance] %s\n' "$*"; }
fail() { printf '[gopulse-acceptance] ERROR: %s\n' "$*" >&2; return 1; }

valid_token() { [[ $1 =~ ^[a-f0-9]{12}$ ]]; }
valid_project() { [[ $1 =~ ^gopulse-acceptance-[a-f0-9]{12}$ ]]; }
valid_database() { [[ $1 =~ ^gopulse_acceptance_[a-f0-9]{12}$ ]]; }
valid_port() { [[ $1 =~ ^[0-9]+$ ]] && ((10#$1 >= 1024 && 10#$1 <= 65535)); }

validate_target() {
  local token=$1 project=$2 database=$3 host=$4 mysql_port=$5 redis_port=$6 rabbit_port=$7 rabbit_management_port=$8 http_port=$9 frontend_port=${10}
  valid_token "$token" || { fail 'acceptance token must contain exactly 12 lowercase hexadecimal characters'; return 1; }
  [[ $project == "gopulse-acceptance-$token" ]] && valid_project "$project" || { fail 'Compose project is outside the acceptance whitelist'; return 1; }
  [[ $database == "gopulse_acceptance_$token" ]] && valid_database "$database" || { fail 'database is outside the acceptance whitelist'; return 1; }
  [[ $host == 127.0.0.1 ]] || { fail 'all published acceptance addresses must be 127.0.0.1'; return 1; }
  local port
  for port in "$mysql_port" "$redis_port" "$rabbit_port" "$rabbit_management_port" "$http_port" "$frontend_port"; do
    valid_port "$port" || { fail "invalid acceptance port: $port"; return 1; }
  done
  [[ $mysql_port != 3306 && $redis_port != 6379 && $rabbit_port != 5672 && $rabbit_management_port != 15672 && $http_port != 8080 && $frontend_port != 5173 ]] || {
    fail 'acceptance must not use a default development port'
    return 1
  }
  [[ $(printf '%s\n' "$mysql_port" "$redis_port" "$rabbit_port" "$rabbit_management_port" "$http_port" "$frontend_port" | sort -u | wc -l) == 6 ]] || {
    fail 'acceptance ports must be unique'
    return 1
  }
}

self_test() {
  local token=012345abcdef project=gopulse-acceptance-012345abcdef database=gopulse_acceptance_012345abcdef
  validate_target "$token" "$project" "$database" 127.0.0.1 43306 46379 45672 45673 48080 45173 >/dev/null
  local rejected=0
  for command in \
    "validate_target '' '$project' '$database' 127.0.0.1 43306 46379 45672 45673 48080 45173" \
    "validate_target '$token' gopulse '$database' 127.0.0.1 43306 46379 45672 45673 48080 45173" \
    "validate_target '$token' '$project' gopulse 127.0.0.1 43306 46379 45672 45673 48080 45173" \
    "validate_target '$token' '$project' '$database' 0.0.0.0 43306 46379 45672 45673 48080 45173" \
    "validate_target '$token' '$project' '$database' 127.0.0.1 3306 46379 45672 45673 48080 45173" \
    "validate_target '$token' '$project' '$database' 127.0.0.1 43306 43306 45672 45673 48080 45173"; do
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
for _ in range(6):
    sock=socket.socket()
    sock.bind(('127.0.0.1', 0))
    sockets.append(sock)
    ports.append(sock.getsockname()[1])
print(' '.join(map(str, ports)))
for sock in sockets:
    sock.close()
PY
)
    read -r MYSQL_PORT REDIS_PORT RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT HTTP_PORT FRONTEND_PORT <<<"$values"
    if validate_target "$TOKEN" "$PROJECT_NAME" "$DATABASE_NAME" "$PUBLISHED_HOST" "$MYSQL_PORT" "$REDIS_PORT" "$RABBITMQ_PORT" "$RABBITMQ_MANAGEMENT_PORT" "$HTTP_PORT" "$FRONTEND_PORT" >/dev/null 2>&1; then
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
  published=$(docker port "$id" "$container_port/tcp" 2>/dev/null | tr -d '\r')
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
RABBITMQ_URL=amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/
AUTH_JWT_SECRET=$JWT_SECRET
AUTH_JWT_TTL=2h
AUTH_COOKIE_NAME=$COOKIE_NAME
AUTH_COOKIE_SECURE=false
REDIS_POST_DETAIL_TTL=5m
REDIS_OPERATION_TIMEOUT=200ms
ENV
}

backend_environment() {
  env \
    APP_ENV=test HTTP_HOST="$PUBLISHED_HOST" HTTP_PORT="$HTTP_PORT" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    REDIS_HOST="$PUBLISHED_HOST" REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB="$REDIS_DB" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" \
    AUTH_JWT_SECRET="$JWT_SECRET" AUTH_JWT_TTL=2h AUTH_COOKIE_NAME="$COOKIE_NAME" AUTH_COOKIE_SECURE=false \
    REDIS_POST_DETAIL_TTL=5m REDIS_OPERATION_TIMEOUT=200ms \
    "$@"
}

start_backend() {
  [[ -z ${BACKEND_PID:-} ]] || fail 'Backend is already recorded as running'
  setsid bash -c 'cd "$1"; shift; exec env "$@"' _ "$BACKEND_DIR" \
    APP_ENV=test HTTP_HOST="$PUBLISHED_HOST" HTTP_PORT="$HTTP_PORT" \
    MYSQL_HOST="$PUBLISHED_HOST" MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE="$DATABASE_NAME" MYSQL_USER="$MYSQL_USER" MYSQL_PASSWORD="$MYSQL_PASSWORD" \
    REDIS_HOST="$PUBLISHED_HOST" REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB="$REDIS_DB" \
    RABBITMQ_URL="amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@$PUBLISHED_HOST:$RABBITMQ_PORT/" \
    AUTH_JWT_SECRET="$JWT_SECRET" AUTH_JWT_TTL=2h AUTH_COOKIE_NAME="$COOKIE_NAME" AUTH_COOKIE_SECURE=false \
    REDIS_POST_DETAIL_TTL=5m REDIS_OPERATION_TIMEOUT=200ms \
    "$TEMP_DIR/gopulse-backend" >>"$TEMP_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/health" 200 || { cat "$TEMP_DIR/backend.log" >&2; return 1; }
}

stop_pid() {
  local pid=${1:-}
  [[ -n $pid ]] || return 0
  if kill -0 "$pid" 2>/dev/null; then
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
  local -a args=(--silent --show-error --max-time 10 --request "$method" --output "$RESPONSE_FILE" --write-out '%{http_code}')
  if [[ $cookie_mode == write ]]; then args+=(--cookie "$COOKIE_JAR" --cookie-jar "$COOKIE_JAR"); else args+=(--cookie "$COOKIE_JAR"); fi
  if [[ -n $body ]]; then args+=(--header 'Content-Type: application/json' --data "$body"); fi
  HTTP_STATUS=$(curl "${args[@]}" "http://$PUBLISHED_HOST:$HTTP_PORT/api/v1$path")
  [[ $HTTP_STATUS == "$expected" ]] || {
    printf '[gopulse-acceptance] response body: ' >&2
    cat "$RESPONSE_FILE" >&2 || true
    printf '\n' >&2
    fail "$method $path returned HTTP $HTTP_STATUS, expected $expected"
  }
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

cleanup() {
  local status=$?
  ((CLEANUP_DONE == 0)) || return "$status"
  CLEANUP_DONE=1
  set +e
  stop_pid "$FRONTEND_PID"
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
  if [[ ${1:-} == --self-test ]]; then
    self_test
    return
  fi
  [[ $# == 0 ]] || { fail 'usage: verify-business.sh [--self-test]'; return 2; }
  require_tools
  TOKEN=${ACCEPTANCE_TOKEN:-$(python3 -c 'import secrets; print(secrets.token_hex(6))')}
  PROJECT_NAME="gopulse-acceptance-$TOKEN"
  DATABASE_NAME="gopulse_acceptance_$TOKEN"
  generate_ports
  validate_target "$TOKEN" "$PROJECT_NAME" "$DATABASE_NAME" "$PUBLISHED_HOST" "$MYSQL_PORT" "$REDIS_PORT" "$RABBITMQ_PORT" "$RABBITMQ_MANAGEMENT_PORT" "$HTTP_PORT" "$FRONTEND_PORT"

  TEMP_DIR=$(mktemp -d -t gopulse-acceptance-XXXXXXXX)
  ACCEPTANCE_ENV="$TEMP_DIR/acceptance.env"
  COOKIE_JAR="$TEMP_DIR/cookies.txt"
  RESPONSE_FILE="$TEMP_DIR/response.json"
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
  verify_service_ownership mysql 3306 "$MYSQL_PORT" >/dev/null
  verify_service_ownership redis 6379 "$REDIS_PORT" >/dev/null
  verify_service_ownership rabbitmq 5672 "$RABBITMQ_PORT" >/dev/null

  backend_environment bash -c 'cd "$1" && go run ./cmd/migrate up' _ "$BACKEND_DIR"
  (cd "$BACKEND_DIR" && go build -o "$TEMP_DIR/gopulse-backend" ./cmd/server)
  start_backend
  start_frontend
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200

  run_api_flow
  run_browser_flow
  verify_restart_and_cache
  verify_redis_failure_and_recovery
  api_request GET /users/me 200 '' read
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/health" 200
  wait_http_status "http://$PUBLISHED_HOST:$HTTP_PORT/ready" 200
  info 'Complete isolated business acceptance passed; cleanup will now remove only verified acceptance resources.'
}

main "$@"
