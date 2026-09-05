#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TOKEN_ID=
PROJECT=
TEMP_DIR=
PIDS=()
LAST_STARTED_PID=

info(){ printf '[verify-events] %s\n' "$*"; }
fail(){ printf '[verify-events] ERROR: %s\n' "$*" >&2; return 1; }
valid_project(){ [[ ${1:-} =~ ^gopulse-events-[0-9a-f]{12}$ ]]; }
free_ports(){ python3 - "$1" <<'PY'
import socket,sys
sockets=[]
try:
 for _ in range(int(sys.argv[1])):
  sock=socket.socket(); sock.bind(('127.0.0.1',0)); sockets.append(sock)
 print(' '.join(str(sock.getsockname()[1]) for sock in sockets))
finally:
 for sock in sockets: sock.close()
PY
}
stop_process(){
  local pid=$1 binary=$2
  [[ -n $pid && -r /proc/$pid/exe ]] || return 0
  [[ $(readlink -f "/proc/$pid/exe") == "$(readlink -f "$binary")" ]] || return 0
  kill -TERM -- "-$pid" 2>/dev/null || true
  for _ in {1..50}; do
    if ! kill -0 "$pid" 2>/dev/null; then wait "$pid" 2>/dev/null || true; return 0; fi
    sleep .1
  done
  kill -KILL -- "-$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}
cleanup(){
  local code=$?
  trap - EXIT INT TERM
  if [[ -n ${TEMP_DIR:-} ]]; then
    for ((i=${#PIDS[@]}-1;i>=0;i--)); do
      [[ -n ${PIDS[$i]} ]] || continue
      stop_process "${PIDS[$i]%%:*}" "${PIDS[$i]#*:}" || true
    done
  fi
  if [[ -n ${PROJECT:-} && -n ${TEMP_DIR:-} && -f $TEMP_DIR/compose.yaml ]] && valid_project "$PROJECT"; then
    docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
  exit "$code"
}
trap cleanup EXIT INT TERM

self_test(){
  local directory monitor
  directory=$(mktemp -d)
  trap 'rm -rf -- "$directory"' RETURN
  valid_project gopulse-events-012345abcdef || fail 'owned Compose project was rejected.'
  if valid_project gopulse-events-production; then fail 'unowned Compose project was accepted.'; fi
  grep -q 'gopulse-events-v1-read' "$REPO_ROOT/backend/internal/eventquery/eventquery.go" || fail 'fixed Events read alias is missing.'
  grep -q 'gopulse-events-v1-template' "$REPO_ROOT/marshaller/internal/elasticsearch/events_client.go" || fail 'fixed Events template is missing.'
  grep -q 'MONITOR_EVENT_MAX_BYTES=16384' "$REPO_ROOT/.env.example" || fail 'fixed EventMonitor size is missing.'
  (cd "$REPO_ROOT/monitor" && go build -o "$directory/monitor" ./cmd/monitor)
  monitor=(env MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT=19090 MONITOR_API_TOKEN=01234567890123456789012345678901 LOG_MONITOR_INGEST_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF MONITOR_PLUGIN_ROOT="$directory/plugins" REDIS_HOST=127.0.0.1 REDIS_PORT=6379 REDIS_DB=0)
  if "${monitor[@]}" MONITOR_EVENT_MAX_BYTES=1 "$directory/monitor" >/dev/null 2>&1; then fail 'Monitor accepted an unsafe event size.'; fi
  if "${monitor[@]}" MONITOR_HTTP_HOST=0.0.0.0 "$directory/monitor" >/dev/null 2>&1; then fail 'Monitor accepted a non-loopback listener.'; fi
  if "${monitor[@]}" MONITOR_HTTP_PORT=0 "$directory/monitor" >/dev/null 2>&1; then fail 'Monitor accepted an unsafe port.'; fi
  if "${monitor[@]}" MONITOR_API_TOKEN=short "$directory/monitor" >/dev/null 2>&1; then fail 'Monitor accepted a short token.'; fi
  if "${monitor[@]}" MONITOR_PLUGIN_ROOT=/ "$directory/monitor" >/dev/null 2>&1; then fail 'Monitor accepted an unsafe plugin root.'; fi
  sleep 5 & local probe=$!
  stop_process "$probe" /bin/false
  kill -0 "$probe" 2>/dev/null || fail 'PID cleanup accepted a mismatched binary.'
  kill "$probe" 2>/dev/null || true; wait "$probe" 2>/dev/null || true
  info 'Self-test passed.'
}
if [[ ${1:-} == --self-test ]]; then self_test; exit 0; elif (($#)); then fail "unknown argument: $1"; exit 2; fi

for tool in docker curl python3 go; do command -v "$tool" >/dev/null || { fail "$tool is required"; exit 1; }; done
docker compose version >/dev/null
TEMP_DIR=$(mktemp -d)
TOKEN_ID=$(python3 -c 'import secrets; print(secrets.token_hex(6))')
PROJECT="gopulse-events-$TOKEN_ID"
valid_project "$PROJECT" || fail 'generated Compose project is invalid.'
read -r MYSQL_PORT REDIS_PORT RABBIT_PORT KAFKA_PORT ES_PORT VM_PORT ROUTER_PORT MARSHALLER_PORT MONITOR_PORT BACKEND_PORT EXPORTER_PORT <<<"$(free_ports 11)"
MYSQL_PASSWORD="mysql-$TOKEN_ID"
REDIS_PASSWORD="redis-$TOKEN_ID"
ROUTER_TOKEN="router-$TOKEN_ID-0123456789abcdef0123456789"
MARSHALLER_TOKEN="marshaller-$TOKEN_ID-0123456789abcdef"
MONITOR_TOKEN="monitor-$TOKEN_ID-0123456789abcdef0123"
INGEST_TOKEN="ingest-$TOKEN_ID-0123456789abcdef012345"
JWT_SECRET="jwt-$TOKEN_ID-0123456789abcdef0123456789"
VM_PASSWORD="vm-$TOKEN_ID-0123456789abcdef"
VM_BASIC=$(printf '%s:%s' gopulse-marshaller "$VM_PASSWORD" | base64 -w0)
SENTINEL="sensitive-$TOKEN_ID"

cat >"$TEMP_DIR/compose.yaml" <<YAML
services:
  mysql:
    image: mysql:8.4.0
    environment: {MYSQL_DATABASE: gopulse_events, MYSQL_USER: gopulse, MYSQL_PASSWORD: "$MYSQL_PASSWORD", MYSQL_ROOT_PASSWORD: "$MYSQL_PASSWORD"}
    ports: ["127.0.0.1:$MYSQL_PORT:3306"]
    healthcheck: {test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -uroot -p$MYSQL_PASSWORD --silent"], interval: 2s, timeout: 2s, retries: 40}
  redis:
    image: redis:7.2.5-alpine
    command: ["redis-server","--requirepass","$REDIS_PASSWORD"]
    ports: ["127.0.0.1:$REDIS_PORT:6379"]
    healthcheck: {test: ["CMD-SHELL", "redis-cli --no-auth-warning -a '$REDIS_PASSWORD' ping | grep -q PONG"], interval: 2s, timeout: 2s, retries: 30}
  rabbitmq:
    image: rabbitmq:3.13.3-management-alpine
    environment: {RABBITMQ_DEFAULT_USER: gopulse, RABBITMQ_DEFAULT_PASS: "$MYSQL_PASSWORD"}
    ports: ["127.0.0.1:$RABBIT_PORT:5672"]
    healthcheck: {test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"], interval: 3s, timeout: 3s, retries: 40}
  kafka:
    image: apache/kafka:4.3.1
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_LISTENERS: CONTROLLER://:9093,INTERNAL://:19092,EXTERNAL://:9092
      KAFKA_ADVERTISED_LISTENERS: INTERNAL://kafka:19092,EXTERNAL://127.0.0.1:$KAFKA_PORT
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
    ports: ["127.0.0.1:$KAFKA_PORT:9092"]
    healthcheck: {test: ["CMD-SHELL", "/opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --list >/dev/null"], interval: 3s, timeout: 3s, retries: 50}
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:9.5.2
    environment: {discovery.type: single-node, xpack.security.enabled: "false", ES_JAVA_OPTS: "-Xms512m -Xmx512m"}
    ports: ["127.0.0.1:$ES_PORT:9200"]
    healthcheck: {test: ["CMD-SHELL", "curl -fsS 'http://127.0.0.1:9200/_cluster/health?wait_for_status=yellow&timeout=1s' >/dev/null"], interval: 4s, timeout: 3s, retries: 50}
  victoriametrics:
    image: victoriametrics/victoria-metrics:v1.151.0
    command: ["-storageDataPath=/data","-httpAuth.username=gopulse-marshaller","-httpAuth.password=$VM_PASSWORD"]
    ports: ["127.0.0.1:$VM_PORT:8428"]
    healthcheck: {test: ["CMD-SHELL", "wget --spider --quiet --header 'Authorization: Basic $VM_BASIC' http://127.0.0.1:8428/health"], interval: 3s, timeout: 3s, retries: 40}
YAML

info 'Starting isolated infrastructure.'
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" up -d --wait
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --create --if-not-exists --topic gopulse-observability-v1 --partitions 1 --replication-factor 1 >/dev/null
mkdir -p "$TEMP_DIR/bin" "$TEMP_DIR/plugins" "$TEMP_DIR/packages"
(cd "$REPO_ROOT/backend" && go build -o "$TEMP_DIR/bin/backend" ./cmd/server && go build -o "$TEMP_DIR/bin/migrate" ./cmd/migrate && go build -o "$TEMP_DIR/bin/admin-role" ./cmd/admin-role)
(cd "$REPO_ROOT/router" && go build -o "$TEMP_DIR/bin/router" ./cmd/router)
(cd "$REPO_ROOT/monitor" && go build -o "$TEMP_DIR/bin/monitor" ./cmd/monitor)
(cd "$REPO_ROOT/marshaller" && go build -o "$TEMP_DIR/bin/marshaller" ./cmd/marshaller)
"$REPO_ROOT/scripts/package-redis-exporter.sh" --version 1.7.0 --output "$TEMP_DIR/packages/redis-exporter-1.7.0.tar.gz" >/dev/null
"$REPO_ROOT/scripts/package-redis-exporter.sh" --version 1.7.1 --output "$TEMP_DIR/packages/redis-exporter-1.7.1.tar.gz" >/dev/null

COMMON=(APP_ENV=test MYSQL_HOST=127.0.0.1 MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE=gopulse_events MYSQL_USER=gopulse MYSQL_PASSWORD="$MYSQL_PASSWORD" REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 RABBITMQ_URL="amqp://gopulse:$MYSQL_PASSWORD@127.0.0.1:$RABBIT_PORT/" ELASTICSEARCH_URL="http://127.0.0.1:$ES_PORT" AUTH_JWT_SECRET="$JWT_SECRET" AUTH_COOKIE_NAME=gopulse_events_session AUTH_COOKIE_SECURE=false MONITOR_URL="http://127.0.0.1:$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN")
env "${COMMON[@]}" "$TEMP_DIR/bin/migrate" up >/dev/null

start(){
  local binary=$1 log=$2; shift 2
  (exec env "$@" setsid "$binary") >"$log" 2>&1 &
  local pid=$!
  LAST_STARTED_PID=$pid
  PIDS+=("$pid:$binary")
  sleep .3
  kill -0 "$pid" 2>/dev/null || { cat "$log" >&2; fail "$binary failed to start"; }
}
wait_url(){
  local url=$1 token=${2:-}
  for _ in {1..150}; do
    if [[ -n $token ]]; then curl -fsS --max-time 1 -H "Authorization: Bearer $token" "$url" >/dev/null 2>&1 && return 0
    else curl -fsS --max-time 1 "$url" >/dev/null 2>&1 && return 0; fi
    sleep .2
  done
  return 1
}

start "$TEMP_DIR/bin/router" "$TEMP_DIR/router.log" ROUTER_HTTP_HOST=127.0.0.1 ROUTER_HTTP_PORT="$ROUTER_PORT" ROUTER_API_TOKEN="$ROUTER_TOKEN" ROUTER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT"
wait_url "http://127.0.0.1:$ROUTER_PORT/ready" "$ROUTER_TOKEN" || { cat "$TEMP_DIR/router.log"; fail 'Router not ready'; }
start "$TEMP_DIR/bin/marshaller" "$TEMP_DIR/marshaller.log" MARSHALLER_HTTP_HOST=127.0.0.1 MARSHALLER_HTTP_PORT="$MARSHALLER_PORT" MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" MARSHALLER_VM_URL="http://127.0.0.1:$VM_PORT" MARSHALLER_VM_USERNAME=gopulse-marshaller MARSHALLER_VM_PASSWORD="$VM_PASSWORD" MARSHALLER_ELASTICSEARCH_URL="http://127.0.0.1:$ES_PORT"
wait_url "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || { cat "$TEMP_DIR/marshaller.log"; fail 'Marshaller not ready'; }
start "$TEMP_DIR/bin/monitor" "$TEMP_DIR/monitor.log" MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" LOG_MONITOR_INGEST_TOKEN="$INGEST_TOKEN" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" MONITOR_EVENT_QUEUE_CAPACITY=16 MONITOR_EVENT_RETRY_MIN=100ms MONITOR_EVENT_RETRY_MAX=100ms REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT"
wait_url "http://127.0.0.1:$MONITOR_PORT/ready" "$MONITOR_TOKEN" || { cat "$TEMP_DIR/monitor.log"; fail 'Monitor not ready'; }
start "$TEMP_DIR/bin/backend" "$TEMP_DIR/backend.log" "${COMMON[@]}" HTTP_HOST=127.0.0.1 HTTP_PORT="$BACKEND_PORT"
wait_url "http://127.0.0.1:$BACKEND_PORT/health" || { cat "$TEMP_DIR/backend.log"; fail 'Backend not healthy'; }

ADMIN_COOKIE="$TEMP_DIR/admin.cookie"
USER_COOKIE="$TEMP_DIR/user.cookie"
curl -fsS -c "$ADMIN_COOKIE" -H 'Content-Type: application/json' -d '{"username":"events_admin","password":"events-password-123"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
env "${COMMON[@]}" "$TEMP_DIR/bin/admin-role" promote --username events_admin >/dev/null
curl -fsS -c "$USER_COOKIE" -H 'Content-Type: application/json' -d '{"username":"events_user","password":"events-password-123"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
[[ $(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/events") == 401 ]] || fail 'Unauthenticated Events query did not return 401.'
[[ $(curl -sS -b "$USER_COOKIE" -o /dev/null -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/events") == 403 ]] || fail 'Ordinary-user Events query did not return 403.'

api(){ local expected=$1; shift; local code; code=$(curl -sS -b "$ADMIN_COOKIE" -o "$TEMP_DIR/api.json" -w '%{http_code}' "$@"); [[ $code == "$expected" ]] || { cat "$TEMP_DIR/api.json" >&2; fail "API returned $code, expected $expected"; }; }
api 201 -F "package=@$TEMP_DIR/packages/redis-exporter-1.7.0.tar.gz" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/install"
api 200 -X POST "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/stop"
api 200 -X POST "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/start"
api 200 -F "package=@$TEMP_DIR/packages/redis-exporter-1.7.1.tar.gz" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/update"

FOUND=0
for _ in {1..160}; do
  status=$(curl -sS -b "$ADMIN_COOKIE" -o "$TEMP_DIR/events.json" -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/events?source=monitor&plugin_id=redis-exporter&limit=100")
  if [[ $status == 200 ]] && python3 - "$TEMP_DIR/events.json" <<'PY' 2>/dev/null; then FOUND=1; break; fi
import json,sys
rows=json.load(open(sys.argv[1])).get('data',[])
names=[row.get('event_name') for row in rows]
required={'exporter_plugin_installed','exporter_plugin_stopped','exporter_plugin_started','exporter_plugin_updated'}
assert required <= set(names)
assert all(set(row)=={'timestamp','event_name','source','severity','message','metadata'} for row in rows)
assert all(row['source']=='monitor' and row['severity']=='info' and row['metadata']['plugin_id']=='redis-exporter' for row in rows)
assert names.count('exporter_plugin_installed')==1 and names.count('exporter_plugin_stopped')==1 and names.count('exporter_plugin_started')==1 and names.count('exporter_plugin_updated')==1
PY
  sleep .25
done
((FOUND==1)) || { cat "$TEMP_DIR/events.json" >&2 || true; curl -sS "http://127.0.0.1:$ES_PORT/gopulse-events-v1-read/_search?size=100" >&2 || true; cat "$TEMP_DIR/monitor.log" "$TEMP_DIR/router.log" "$TEMP_DIR/marshaller.log" >&2 || true; fail 'Admin query did not observe all lifecycle events.'; }

curl -fsS "http://127.0.0.1:$ES_PORT/_index_template/gopulse-events-v1-template" >"$TEMP_DIR/template.json"
curl -fsS "http://127.0.0.1:$ES_PORT/_alias/gopulse-events-v1-read" >"$TEMP_DIR/alias.json"
curl -fsS "http://127.0.0.1:$ES_PORT/gopulse-events-v1-read/_search?size=100" >"$TEMP_DIR/documents.json"
PHYSICAL_INDEX=$(python3 - "$TEMP_DIR/template.json" "$TEMP_DIR/alias.json" "$TEMP_DIR/documents.json" <<'PY'
import json,re,sys
template,aliases,documents=(json.load(open(path)) for path in sys.argv[1:])
entry=template['index_templates'][0]['index_template']
assert entry['index_patterns']==['gopulse-events-v1-*']
assert entry['template']['mappings']['dynamic']=='strict'
assert entry['template']['mappings']['properties']['metadata']['dynamic']=='strict'
assert 'gopulse-events-v1-read' in entry['template']['aliases']
assert aliases and all(re.fullmatch(r'gopulse-events-v1-\d{4}\.\d{2}\.\d{2}',index) for index in aliases)
allowed={'@timestamp','event_schema_version','event_name','source','severity','message','metadata'}
hits=documents.get('hits',{}).get('hits',[])
assert len(hits)==4 and all(set(hit['_source'])==allowed for hit in hits)
print(next(iter(aliases)))
PY
)
STRICT_STATUS=$(curl -sS -o "$TEMP_DIR/strict.json" -w '%{http_code}' -X PUT -H 'Content-Type: application/json' --data-binary '{"unknown_field":true}' "http://127.0.0.1:$ES_PORT/$PHYSICAL_INDEX/_doc/strict-probe")
[[ $STRICT_STATUS == 400 ]] || fail 'Strict Events mapping accepted an unknown field.'
if grep -Fq -- "$SENTINEL" "$TEMP_DIR"/*.log "$TEMP_DIR/events.json" "$TEMP_DIR/documents.json"; then fail 'Sensitive sentinel leaked into Events artifacts.'; fi
info "Lifecycle Events query closed end to end through index $PHYSICAL_INDEX."
