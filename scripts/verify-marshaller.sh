#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TOPIC=gopulse-observability-v1
GROUP=gopulse-marshaller-metrics-v1
ROUTER_TOKEN=verify-router-token-at-least-32-bytes-long
MONITOR_TOKEN=verify-monitor-token-at-least-32-bytes-long
MARSHALLER_TOKEN=verify-marshaller-token-at-least-32-bytes
VM_USER=gopulse-marshaller
TEMP_DIR=
PROJECT=
TOKEN_ID=
KAFKA_PORT=
REDIS_PORT=
VM_PORT=
ES_PORT=
ROUTER_PORT=
MARSHALLER_PORT=
MONITOR_PORT=
EXPORTER_PORT=
REDIS_PASSWORD=
VM_PASSWORD=
VM_BASIC=
KAFKA_ID=
ROUTER_PID=
MARSHALLER_PID=
GROUP_PEER_PID=
MONITOR_PID=
CLEANED=0

info() { printf '[verify-marshaller] %s\n' "$*"; }
fail() { printf '[verify-marshaller] ERROR: %s\n' "$*" >&2; return 1; }
valid_project() { [[ $1 =~ ^gopulse-marshaller-[a-f0-9]{12}$ ]]; }
valid_topic() { [[ $1 == "$TOPIC" ]]; }
valid_group() { [[ $1 == "$GROUP" ]]; }
valid_metric() {
  case $1 in
    gopulse_redis_up|gopulse_redis_uptime_seconds|gopulse_redis_connected_clients|gopulse_redis_used_memory_bytes|gopulse_redis_commands_processed_total|gopulse_redis_keyspace_hits_total|gopulse_redis_keyspace_misses_total|gopulse_redis_cpu_seconds_total|gopulse_redis_db_keys|gopulse_redis_db_expiring_keys) return 0 ;;
    *) return 1 ;;
  esac
}
ports_unique() {
  python3 - "$@" <<'PY'
import sys
ports = sys.argv[1:]
raise SystemExit(0 if len(ports) == 8 and len(set(ports)) == 8 and all(value.isdigit() and 1024 < int(value) < 65536 for value in ports) else 1)
PY
}
allocate_ports() {
  local values
  values=$(python3 - <<'PY'
import socket
sockets = []
try:
    for _ in range(8):
        sock = socket.socket()
        sock.bind(('127.0.0.1', 0))
        sockets.append(sock)
    print(' '.join(str(sock.getsockname()[1]) for sock in sockets))
finally:
    for sock in sockets:
        sock.close()
PY
  ) || return 1
  read -r KAFKA_PORT REDIS_PORT VM_PORT ES_PORT ROUTER_PORT MARSHALLER_PORT MONITOR_PORT EXPORTER_PORT <<<"$values"
  ports_unique "$KAFKA_PORT" "$REDIS_PORT" "$VM_PORT" "$ES_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT"
}
compose() { docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" "$@"; }
container_ids() {
  docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --filter "label=com.docker.compose.service=$1" --format '{{.ID}}'
}
refresh_container_id() {
  local service=$1 ids count
  ids=$(container_ids "$service")
  count=$(sed '/^[[:space:]]*$/d' <<<"$ids" | wc -l | tr -d ' ')
  [[ $count == 1 ]] || { fail "Expected exactly one owned $service container, found $count."; return 1; }
  if [[ $service == kafka ]]; then KAFKA_ID=$ids; fi
}
wait_health() {
  local service=$1 id state
  for _ in {1..120}; do
    refresh_container_id "$service" || return 1
    id=$(container_ids "$service")
    state=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id" 2>/dev/null || true)
    [[ $state == healthy ]] && return 0
    sleep .25
  done
  compose ps >&2
  fail "$service did not become healthy."
}
http_status() {
  local url=$1 token=${2:-}
  if [[ -n $token ]]; then
    curl -sS --max-time 2 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "$url" 2>/dev/null || printf '000'
  else
    curl -sS --max-time 2 -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || printf '000'
  fi
}
wait_http() {
  local url=$1 token=${2:-}
  for _ in {1..100}; do
    [[ $(http_status "$url" "$token") == 200 ]] && return 0
    sleep .1
  done
  return 1
}
wait_not_ready() {
  local url=$1 token=$2 status
  for _ in {1..100}; do
    status=$(http_status "$url" "$token")
    [[ $status == 503 || $status == 000 ]] && return 0
    sleep .1
  done
  return 1
}
start_process() {
  local var=$1 cwd=$2 binary=$3 log=$4
  shift 4
  (cd "$cwd" && exec env "$@" setsid "$binary") >>"$log" 2>&1 &
  local pid=$!
  printf -v "$var" '%s' "$pid"
  sleep .2
  kill -0 "$pid" 2>/dev/null || { cat "$log" >&2; fail "$binary exited during startup."; }
}
stop_process() {
  local pid=$1 binary=$2
  [[ -n $pid ]] || return 0
  [[ -r /proc/$pid/exe ]] || return 0
  [[ $(readlink -f "/proc/$pid/exe") == "$(readlink -f "$binary")" ]] || { fail "Refusing to stop PID $pid because its executable identity changed."; return 1; }
  kill -TERM -- "-$pid" 2>/dev/null || true
  for _ in {1..50}; do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep .1
  done
  kill -KILL -- "-$pid" 2>/dev/null || true
  for _ in {1..20}; do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep .1
  done
  fail "Owned process $pid did not stop."
}
cleanup() {
  local code=$?
  ((CLEANED == 0)) || return "$code"
  CLEANED=1
  stop_process "$MONITOR_PID" "$TEMP_DIR/monitor" || true
  stop_process "$MARSHALLER_PID" "$TEMP_DIR/marshaller" || true
  stop_process "$GROUP_PEER_PID" "$TEMP_DIR/verify-group-member" || true
  stop_process "$ROUTER_PID" "$TEMP_DIR/router" || true
  if [[ -n $PROJECT && -n $TEMP_DIR && -f $TEMP_DIR/compose.yaml ]] && valid_project "$PROJECT"; then
    compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  [[ -z $TEMP_DIR ]] || rm -rf -- "$TEMP_DIR"
  return "$code"
}
trap cleanup EXIT INT TERM

self_test() {
  local directory rejected=0
  directory=$(mktemp -d)
  trap 'rm -rf -- "$directory"' RETURN
  (cd "$REPO_ROOT/marshaller" && go build -o "$directory/marshaller" ./cmd/marshaller)
  if MARSHALLER_API_TOKEN=short MARSHALLER_VM_PASSWORD=password-password "$directory/marshaller" >/dev/null 2>&1; then fail 'Marshaller accepted a short token.'; return 1; fi
  ((rejected += 1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_KAFKA_TOPIC=other "$directory/marshaller" >/dev/null 2>&1; then fail 'Marshaller accepted an unsafe Topic.'; return 1; fi
  ((rejected += 1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_KAFKA_GROUP=other "$directory/marshaller" >/dev/null 2>&1; then fail 'Marshaller accepted an unsafe group.'; return 1; fi
  ((rejected += 1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_VM_URL='http://user:pass@127.0.0.1:8428' "$directory/marshaller" >/dev/null 2>&1; then fail 'Marshaller accepted credentials in its URL.'; return 1; fi
  ((rejected += 1))
  valid_project gopulse-marshaller-deadbeefcafe || return 1
  if valid_project gopulse-marshaller; then return 1; fi
  ((rejected += 1))
  valid_topic "$TOPIC" && valid_group "$GROUP" && valid_metric gopulse_redis_up || return 1
  if valid_topic other || valid_group other || valid_metric arbitrary_query; then return 1; fi
  ((rejected += 3))
  ports_unique 11001 11002 11003 11004 11005 11006 11007 11008 || return 1
  if ports_unique 11001 11002 11003 11004 11001 11006 11007 11008; then return 1; fi
  ((rejected += 1))
  info "Self-test passed without Docker: $rejected unsafe configuration, project, query, and port cases were rejected."
}
if [[ ${1:-} == --self-test ]]; then self_test; exit 0; elif (($#)); then fail "Unknown argument: $1"; exit 2; fi

for tool in docker curl python3 go setsid ss readlink base64; do command -v "$tool" >/dev/null || { fail "$tool is required."; exit 1; }; done
docker compose version >/dev/null 2>&1 || { fail 'Docker Compose is required.'; exit 1; }
docker info >/dev/null 2>&1 || { fail 'Docker daemon is unavailable.'; exit 1; }

TEMP_DIR=$(mktemp -d)
TOKEN_ID=$(python3 -c 'import secrets; print(secrets.token_hex(6))')
PROJECT="gopulse-marshaller-$TOKEN_ID"
valid_project "$PROJECT"
allocate_ports
REDIS_PASSWORD="redis-$TOKEN_ID-secret"
VM_PASSWORD="vm-$TOKEN_ID-secret-password"
VM_BASIC=$(printf '%s:%s' "$VM_USER" "$VM_PASSWORD" | base64 | tr -d '\n')
[[ -z $(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.ID}}') ]] || fail 'Random project already owns containers.'
[[ -z $(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Name}}') ]] || fail 'Random project already owns volumes.'

cat >"$TEMP_DIR/compose.yaml" <<YAML
services:
  redis:
    image: redis:7.2.5-alpine
    command: ["redis-server","--requirepass","$REDIS_PASSWORD"]
    ports: ["127.0.0.1:$REDIS_PORT:6379"]
    healthcheck: {test: ["CMD-SHELL","redis-cli --no-auth-warning -a '$REDIS_PASSWORD' ping | grep -q PONG"], interval: 2s, timeout: 2s, retries: 30}
  kafka:
    image: apache/kafka:4.3.1
    hostname: kafka
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
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: 0
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"
    ports: ["127.0.0.1:$KAFKA_PORT:9092"]
    volumes: ["kafka_data:/var/lib/kafka/data"]
    healthcheck: {test: ["CMD-SHELL","/opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --list >/dev/null 2>&1"], interval: 2s, timeout: 3s, retries: 60}
  victoriametrics:
    image: victoriametrics/victoria-metrics:v1.151.0
    environment: {VM_USER: "$VM_USER", VM_PASSWORD: "$VM_PASSWORD"}
    command: ["-storageDataPath=/victoria-metrics-data","-httpAuth.username=$VM_USER","-httpAuth.password=$VM_PASSWORD","-dedup.minScrapeInterval=1ms"]
    ports: ["127.0.0.1:$VM_PORT:8428"]
    volumes: ["victoriametrics_data:/victoria-metrics-data"]
    healthcheck:
      test:
        - CMD-SHELL
        - >-
          wget --spider --quiet --header "Authorization: Basic $VM_BASIC" http://127.0.0.1:8428/health
      interval: 2s
      timeout: 2s
      retries: 60
  elasticsearch:
    image: docker.elastic.co/elasticsearch/elasticsearch:9.5.2
    environment: {discovery.type: single-node, xpack.security.enabled: "false", ES_JAVA_OPTS: "-Xms512m -Xmx512m"}
    ports: ["127.0.0.1:$ES_PORT:9200"]
    volumes: ["elasticsearch_data:/usr/share/elasticsearch/data"]
    healthcheck: {test: ["CMD-SHELL", "curl -fsS 'http://127.0.0.1:9200/_cluster/health?wait_for_status=yellow&timeout=1s' >/dev/null"], interval: 3s, timeout: 3s, retries: 60}
volumes: {kafka_data: {}, victoriametrics_data: {}, elasticsearch_data: {}}
YAML

compose up -d
wait_health redis
wait_health kafka
wait_health victoriametrics
wait_health elasticsearch
refresh_container_id kafka

docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --create --topic "$TOPIC" --partitions 1 --replication-factor 1 >/dev/null
(cd "$REPO_ROOT/router" && go build -o "$TEMP_DIR/router" ./cmd/router && go build -o "$TEMP_DIR/verify-consumer" ./cmd/verify-consumer)
(cd "$REPO_ROOT/marshaller" && go build -o "$TEMP_DIR/marshaller" ./cmd/marshaller && go build -o "$TEMP_DIR/verify-group-member" ./cmd/verify-group-member)
(cd "$REPO_ROOT/monitor" && go build -o "$TEMP_DIR/monitor" ./cmd/monitor)
"$REPO_ROOT/scripts/package-redis-exporter.sh" --output "$TEMP_DIR/exporter.tar.gz" >/dev/null
: >"$TEMP_DIR/router.log"
: >"$TEMP_DIR/marshaller.log"
: >"$TEMP_DIR/group-peer.log"
: >"$TEMP_DIR/monitor.log"

start_router() {
  start_process ROUTER_PID "$REPO_ROOT/router" "$TEMP_DIR/router" "$TEMP_DIR/router.log" \
    ROUTER_HTTP_HOST=127.0.0.1 ROUTER_HTTP_PORT="$ROUTER_PORT" ROUTER_API_TOKEN="$ROUTER_TOKEN" \
    ROUTER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" ROUTER_KAFKA_TOPIC="$TOPIC"
  wait_http "http://127.0.0.1:$ROUTER_PORT/ready" "$ROUTER_TOKEN" || { cat "$TEMP_DIR/router.log" >&2; fail 'Router not ready.'; }
}
start_marshaller() {
  start_process MARSHALLER_PID "$REPO_ROOT/marshaller" "$TEMP_DIR/marshaller" "$TEMP_DIR/marshaller.log" \
    MARSHALLER_HTTP_HOST=127.0.0.1 MARSHALLER_HTTP_PORT="$MARSHALLER_PORT" MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" \
    MARSHALLER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" MARSHALLER_KAFKA_TOPIC="$TOPIC" MARSHALLER_KAFKA_GROUP="$GROUP" \
    MARSHALLER_VM_URL="http://127.0.0.1:$VM_PORT" MARSHALLER_VM_USERNAME="$VM_USER" MARSHALLER_VM_PASSWORD="$VM_PASSWORD" \
    MARSHALLER_ELASTICSEARCH_URL="http://127.0.0.1:$ES_PORT" MARSHALLER_ELASTICSEARCH_TIMEOUT=3s \
    MARSHALLER_RETRY_MIN=100ms MARSHALLER_RETRY_MAX=500ms
  wait_http "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || { cat "$TEMP_DIR/marshaller.log" >&2; fail 'Marshaller not ready.'; }
}
start_monitor() {
  mkdir -p "$TEMP_DIR/plugins"
  start_process MONITOR_PID "$REPO_ROOT/monitor" "$TEMP_DIR/monitor" "$TEMP_DIR/monitor.log" \
    MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" LOG_MONITOR_INGEST_TOKEN="verify-log-ingest-token-at-least-32-bytes" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" \
    MONITOR_PLUGIN_STARTUP_TIMEOUT=10s MONITOR_PLUGIN_STOP_TIMEOUT=4s MONITOR_SCRAPE_INTERVAL=1s MONITOR_SCRAPE_TIMEOUT=800ms \
    MONITOR_PUBLISH_TIMEOUT=3s MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" \
    REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 \
    REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT" REDIS_EXPORTER_SCRAPE_TIMEOUT=800ms REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s
  wait_http "http://127.0.0.1:$MONITOR_PORT/ready" "$MONITOR_TOKEN" || { cat "$TEMP_DIR/monitor.log" >&2; fail 'Monitor not ready.'; }
  local status
  if [[ ! -f $TEMP_DIR/plugins/registry.json ]]; then
    status=$(curl -sS --max-time 30 -o "$TEMP_DIR/install.json" -w '%{http_code}' -H "Authorization: Bearer $MONITOR_TOKEN" -F "package=@$TEMP_DIR/exporter.tar.gz" "http://127.0.0.1:$MONITOR_PORT/internal/v1/exporter-plugins/install")
    [[ $status == 201 ]] || { cat "$TEMP_DIR/install.json" >&2; fail "plugin install returned $status"; }
  fi
  wait_http "http://127.0.0.1:$EXPORTER_PORT/health" || { cat "$TEMP_DIR/monitor.log" >&2; fail 'Redis Exporter not healthy.'; }
}
vm_query() {
  local metric=$1 output=$2
  valid_metric "$metric" || return 1
  curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" --data-urlencode "query=$metric{source=\"redis\",target_id=\"redis-exporter-local\"}" "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" >"$output"
}
vm_invalid_total() {
  curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" --data-urlencode 'query=sum(vm_rows_invalid_total)' "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" | python3 -c 'import json,sys; rows=json.load(sys.stdin).get("data",{}).get("result",[]); print(rows[0]["value"][1] if rows else "0")'
}
vm_internal_total() {
  local metric=$1
  curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" "http://127.0.0.1:$VM_PORT/metrics" | awk -v metric="$metric" '
    $1 == metric || index($1, metric "{") == 1 { total += $NF }
    END { printf "%.0f\n", total + 0 }
  '
}
check_internal_access() {
  local base="http://127.0.0.1:$MARSHALLER_PORT" vm="http://127.0.0.1:$VM_PORT" status vm_id binding
  for request in     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' '$base/ready'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer wrong-internal-token' '$base/ready'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Cookie: gopulse_session=ordinary-user-fixture' '$base/ready'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Cookie: gopulse_session=admin-user-fixture' '$base/ready'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer backend-jwt-fixture' '$base/ready?token=$MARSHALLER_TOKEN'"; do
    status=$(eval "$request")
    [[ $status == 401 ]] || { fail "Marshaller accepted a non-internal identity (HTTP $status)."; return 1; }
  done
  [[ $(http_status "$base/ready" "$MARSHALLER_TOKEN") == 200 ]] || { fail 'Marshaller rejected the correct internal Bearer token.'; return 1; }
  for path in /metrics /query /offsets /replay /admin; do
    status=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' "$base$path")
    [[ $status == 404 ]] || { fail "Marshaller unexpectedly exposed $path (HTTP $status)."; return 1; }
  done
  for request in     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' '$vm/prometheus/api/v1/query?query=1'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' --user 'wrong:wrong' '$vm/prometheus/api/v1/query?query=1'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Cookie: gopulse_session=ordinary-user-fixture' '$vm/prometheus/api/v1/query?query=1'"     "curl -sS --max-time 3 -o /dev/null -w '%{http_code}' -H 'Cookie: gopulse_session=admin-user-fixture' '$vm/prometheus/api/v1/query?query=1'"; do
    status=$(eval "$request")
    [[ $status == 401 ]] || { fail "VictoriaMetrics accepted a non-internal identity (HTTP $status)."; return 1; }
  done
  status=$(curl -sS --max-time 3 -o /dev/null -w '%{http_code}' --user "$VM_USER:$VM_PASSWORD" "$vm/prometheus/api/v1/query?query=1")
  [[ $status == 200 ]] || { fail 'VictoriaMetrics rejected the correct internal Basic identity.'; return 1; }
  vm_id=$(container_ids victoriametrics)
  binding=$(docker port "$vm_id" 8428/tcp)
  [[ $binding == "127.0.0.1:$VM_PORT" ]] || { fail "VictoriaMetrics was not loopback-only: $binding"; return 1; }
  ss -ltnH "sport = :$MARSHALLER_PORT" | awk '$4 ~ /^127\.0\.0\.1:/ {found=1} END {exit !found}' || { fail 'Marshaller was not loopback-only.'; return 1; }
  info 'Marshaller and VictoriaMetrics rejected browser/user identities and exposed only their internal loopback surfaces.'
}
wait_metric_value() {
  local metric=$1 expected=$2 output="$TEMP_DIR/query.json"
  for _ in {1..120}; do
    if vm_query "$metric" "$output" && python3 - "$output" "$expected" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding='utf-8'))
want = float(sys.argv[2])
rows = value.get('data', {}).get('result', [])
raise SystemExit(0 if any(float(row['value'][1]) == want for row in rows) else 1)
PY
    then return 0; fi
    sleep .25
  done
  return 1
}
wait_metric_presence() {
  local metric=$1 output="$TEMP_DIR/query.json"
  for _ in {1..120}; do
    if vm_query "$metric" "$output" && python3 - "$output" <<'PY'
import json, sys
raise SystemExit(0 if json.load(open(sys.argv[1], encoding='utf-8')).get('data', {}).get('result') else 1)
PY
    then return 0; fi
    sleep .25
  done
  return 1
}
committed_offset() {
  docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server 127.0.0.1:19092 --group "$GROUP" --describe 2>/dev/null | awk -v topic="$TOPIC" '$2==topic && $3==0 {print $4; exit}'
}
end_offset() {
  docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" 2>/dev/null | awk -F: '$2==0 {print $3}'
}
wait_commit_after() {
  local base=$1 value
  for _ in {1..160}; do
    value=$(committed_offset || true)
    [[ $value =~ ^[0-9]+$ ]] && ((value > base)) && return 0
    sleep .25
  done
  return 1
}
wait_new_record_after() {
  local base=$1 value
  for _ in {1..120}; do
    value=$(end_offset || true)
    [[ $value =~ ^[0-9]+$ ]] && ((value > base)) && return 0
    sleep .25
  done
  return 1
}
wait_group_assignment() {
  local details
  for _ in {1..120}; do
    details=$(docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server 127.0.0.1:19092 --group "$GROUP" --describe 2>/dev/null || true)
    [[ $details == *"$TOPIC"* ]] && awk -v topic="$TOPIC" '$2==topic && $3==0 && $4 ~ /^[0-9]+$/ {found=1} END {exit !found}' <<<"$details" && return 0
    sleep .25
  done
  return 1
}
wait_group_member_client() {
  local client_id=$1 details
  for _ in {1..120}; do
    details=$(docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server 127.0.0.1:19092 --group "$GROUP" --describe --members 2>/dev/null || true)
    grep -Fq "$client_id" <<<"$details" && return 0
    sleep .25
  done
  return 1
}
wait_peer_assignment() {
  for _ in {1..120}; do
    grep -Fq "assigned topic=$TOPIC partition=0" "$TEMP_DIR/group-peer.log" && return 0
    sleep .25
  done
  return 1
}
start_group_peer() {
  local client_id="gopulse-marshaller-review-peer-$TOKEN_ID"
  (cd "$REPO_ROOT/marshaller" && exec setsid "$TEMP_DIR/verify-group-member" \
    --brokers "127.0.0.1:$KAFKA_PORT" --topic "$TOPIC" --group "$GROUP" --client-id "$client_id") >>"$TEMP_DIR/group-peer.log" 2>&1 &
  GROUP_PEER_PID=$!
  sleep .2
  kill -0 "$GROUP_PEER_PID" 2>/dev/null || { cat "$TEMP_DIR/group-peer.log" >&2; fail 'Second group member exited during startup.'; return 1; }
  wait_group_member_client "$client_id" || { cat "$TEMP_DIR/group-peer.log" >&2; fail 'Second group member did not join.'; return 1; }
}
produce_record() {
  local key=$1 value_file=$2
  { printf '%s:' "$key"; cat "$value_file"; printf '\n'; } | docker exec -i "$KAFKA_ID" /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" --property parse.key=true --property key.separator=: >/dev/null
}

INVALID_BEFORE=$(vm_invalid_total)
ACCEPTANCE_START_MS=$(date +%s%3N)
start_router
start_marshaller
check_internal_access

REDIS_ID=$(container_ids redis)
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" SET phase8:plain accepted >/dev/null
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" SET phase8:ttl expires EX 300 >/dev/null
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" GET phase8:plain >/dev/null
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" GET phase8:missing >/dev/null
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" INCR phase8:counter >/dev/null

start_monitor
wait_group_assignment || fail 'Formal Marshaller group did not receive the fixed partition.'

SUCCESS_METRICS=(
  gopulse_redis_up gopulse_redis_uptime_seconds gopulse_redis_connected_clients
  gopulse_redis_used_memory_bytes gopulse_redis_commands_processed_total
  gopulse_redis_keyspace_hits_total gopulse_redis_keyspace_misses_total
  gopulse_redis_cpu_seconds_total gopulse_redis_db_keys gopulse_redis_db_expiring_keys
)
wait_metric_value gopulse_redis_up 1 || { cat "$TEMP_DIR/marshaller.log" >&2; fail 'missing success up=1'; }
for metric in "${SUCCESS_METRICS[@]}"; do
  wait_metric_presence "$metric" || { cat "$TEMP_DIR/marshaller.log" >&2; fail "missing metric $metric"; }
  vm_query "$metric" "$TEMP_DIR/$metric.json"
done
curl -fsS --max-time 3 "http://127.0.0.1:$EXPORTER_PORT/metrics" >"$TEMP_DIR/exporter.metrics"
docker exec "$REDIS_ID" redis-cli --no-auth-warning -a "$REDIS_PASSWORD" INFO >"$TEMP_DIR/redis.info"
python3 - "$TEMP_DIR" <<'PYMATRIX'
import json
import math
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
metrics = [
    'gopulse_redis_up', 'gopulse_redis_uptime_seconds', 'gopulse_redis_connected_clients',
    'gopulse_redis_used_memory_bytes', 'gopulse_redis_commands_processed_total',
    'gopulse_redis_keyspace_hits_total', 'gopulse_redis_keyspace_misses_total',
    'gopulse_redis_cpu_seconds_total', 'gopulse_redis_db_keys', 'gopulse_redis_db_expiring_keys',
]
expected_labels = {name: [set()] for name in metrics}
expected_labels['gopulse_redis_cpu_seconds_total'] = [{'mode'}, {'mode'}]
expected_labels['gopulse_redis_db_keys'] = [{'db'}]
expected_labels['gopulse_redis_db_expiring_keys'] = [{'db'}]
vm = {}
for name in metrics:
    payload = json.loads((root / f'{name}.json').read_text())
    rows = payload.get('data', {}).get('result', [])
    expected_count = 2 if name == 'gopulse_redis_cpu_seconds_total' else 1
    assert payload.get('status') == 'success' and len(rows) == expected_count, (name, rows)
    vm[name] = {}
    for row in rows:
        labels = dict(row['metric'])
        assert labels.pop('__name__') == name
        assert labels.pop('source') == 'redis'
        assert labels.pop('target_id') == 'redis-exporter-local'
        assert set(labels) in expected_labels[name], (name, labels)
        value = float(row['value'][1])
        assert math.isfinite(value)
        vm[name][tuple(sorted(labels.items()))] = value
assert vm['gopulse_redis_up'][()] == 1
assert set(dict(key)['mode'] for key in vm['gopulse_redis_cpu_seconds_total']) == {'user', 'system'}
assert set(dict(key)['db'] for key in vm['gopulse_redis_db_keys']) == {'0'}
assert set(dict(key)['db'] for key in vm['gopulse_redis_db_expiring_keys']) == {'0'}
assert vm['gopulse_redis_db_keys'][(('db', '0'),)] >= 3
assert vm['gopulse_redis_db_expiring_keys'][(('db', '0'),)] >= 1
assert vm['gopulse_redis_keyspace_hits_total'][()] >= 1
assert vm['gopulse_redis_keyspace_misses_total'][()] >= 1

sample_re = re.compile(r'^(gopulse_redis_[a-z_]+)(?:\{([^}]*)\})? ([^ ]+)$')
exported = {}
for line in (root / 'exporter.metrics').read_text().splitlines():
    match = sample_re.match(line)
    if not match:
        continue
    name, raw_labels, raw_value = match.groups()
    labels = tuple(sorted(re.findall(r'(\w+)="([^"]*)"', raw_labels or '')))
    exported[(name, labels)] = float(raw_value)
assert len([key for key in exported if key[0] in metrics]) == 11
info = {}
for line in (root / 'redis.info').read_text().splitlines():
    if ':' in line and not line.startswith('#'):
        key, value = line.rstrip('\r').split(':', 1)
        info[key] = value
for field in ('uptime_in_seconds', 'connected_clients', 'used_memory', 'total_commands_processed', 'keyspace_hits', 'keyspace_misses', 'used_cpu_user', 'used_cpu_sys', 'db0'):
    assert field in info, field

def close(name, labels=(), tolerance=0):
    left = vm[name][labels]
    right = exported[(name, labels)]
    assert abs(left - right) <= tolerance, (name, left, right, tolerance)
close('gopulse_redis_up')
close('gopulse_redis_uptime_seconds', tolerance=45)
close('gopulse_redis_connected_clients', tolerance=3)
close('gopulse_redis_used_memory_bytes', tolerance=5 * 1024 * 1024)
close('gopulse_redis_commands_processed_total', tolerance=200)
close('gopulse_redis_keyspace_hits_total', tolerance=20)
close('gopulse_redis_keyspace_misses_total', tolerance=20)
close('gopulse_redis_cpu_seconds_total', (('mode', 'user'),), 2)
close('gopulse_redis_cpu_seconds_total', (('mode', 'system'),), 2)
close('gopulse_redis_db_keys', (('db', '0'),))
close('gopulse_redis_db_expiring_keys', (('db', '0'),))
PYMATRIX
SUCCESS_END_MS=$(date +%s%3N)
info "Real Redis success matrix reached VictoriaMetrics with all 10 families/11 samples in window $ACCEPTANCE_START_MS..$SUCCESS_END_MS."

REAL_END=$(end_offset)
[[ $REAL_END =~ ^[0-9]+$ ]] && ((REAL_END > 0)) || fail 'Real upstream did not create a Kafka record.'
REAL_OFFSET=$((REAL_END - 1))
"$TEMP_DIR/verify-consumer" --brokers "127.0.0.1:$KAFKA_PORT" --topic "$TOPIC" --client-id "gopulse-verify-$TOKEN_ID" --partition 0 --start "$REAL_OFFSET" --end "$REAL_END" --timeout 15s >"$TEMP_DIR/real-record" 2>"$TEMP_DIR/real-record.stderr"
python3 - "$TEMP_DIR/real-record" "$TEMP_DIR/real.key" "$TEMP_DIR/real.json" "$TEMP_DIR/real.meta" <<'PYREAL'
import base64
import datetime
import json
import pathlib
import sys
source, key_path, value_path, meta_path = sys.argv[1:]
record = json.loads(pathlib.Path(source).read_text())
key = record['key']
value = base64.b64decode(record['value_base64']).decode()
document = json.loads(value)
assert key == document['message_id'] and len(key) == 32
stamp = datetime.datetime.fromisoformat(document['timestamp'].replace('Z', '+00:00'))
pathlib.Path(key_path).write_text(key)
pathlib.Path(value_path).write_text(value)
pathlib.Path(meta_path).write_text(f"{key}\n{int(stamp.timestamp() * 1000)}\n{record['offset']}\n")
PYREAL
mapfile -t REAL_META <"$TEMP_DIR/real.meta"
REAL_KEY=${REAL_META[0]}
REAL_TIMESTAMP_MS=${REAL_META[1]}
info "Captured real upstream record message_id=$REAL_KEY partition=0 offset=${REAL_META[2]} timestamp_ms=$REAL_TIMESTAMP_MS for deterministic replay."

stop_process "$MONITOR_PID" "$TEMP_DIR/monitor"
MONITOR_PID=

BEFORE=$(committed_offset)
for _ in {1..120}; do
  [[ $(end_offset) == "$BEFORE" ]] && break
  sleep .25
done
[[ $(end_offset) == "$BEFORE" ]] || fail 'Offsets were not settled before the two-member rebalance scenario.'
start_group_peer
OLD_MARSHALLER_PID=$MARSHALLER_PID
stop_process "$MARSHALLER_PID" "$TEMP_DIR/marshaller"
MARSHALLER_PID=
wait_peer_assignment || { cat "$TEMP_DIR/group-peer.log" >&2; fail 'Second group member did not receive the partition.'; }
produce_record "$REAL_KEY" "$TEMP_DIR/real.json"
wait_new_record_after "$BEFORE" || fail 'Kafka did not receive the rebalance recovery record.'
sleep 1
[[ $(committed_offset) == "$BEFORE" ]] || fail 'Non-committing peer advanced the formal group offset.'
start_marshaller
[[ $MARSHALLER_PID != "$OLD_MARSHALLER_PID" ]] || fail 'Replacement Marshaller did not start for partition handoff.'
stop_process "$GROUP_PEER_PID" "$TEMP_DIR/verify-group-member"
GROUP_PEER_PID=
wait_group_assignment || fail 'Replacement Marshaller did not receive the partition after peer departure.'
wait_commit_after "$BEFORE" || fail 'Replacement Marshaller did not re-fetch and commit from the last formal group offset.'
info 'A real second group member received the partition without committing; the replacement Marshaller re-fetched from the last committed offset after handoff.'
python3 - "$TEMP_DIR/real.json" "$TEMP_DIR" <<'PYBAD'
import datetime
import json
import pathlib
import sys
source, target = sys.argv[1:]
root = pathlib.Path(target)
document = json.loads(pathlib.Path(source).read_text())
root.joinpath('invalid-structure.json').write_text('{')
now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=(datetime.datetime.now(datetime.timezone.utc).microsecond // 1000) * 1000)
mismatch = dict(document)
mismatch['message_id'] = '22222222222222222222222222222222'
mismatch['timestamp'] = now.isoformat(timespec='milliseconds').replace('+00:00', 'Z')
root.joinpath('invalid-mismatch.json').write_text(json.dumps(mismatch, separators=(',', ':')))
payload = json.loads(json.dumps(document))
payload['message_id'] = '44444444444444444444444444444444'
payload['timestamp'] = (now + datetime.timedelta(milliseconds=1)).isoformat(timespec='milliseconds').replace('+00:00', 'Z')
payload['payload']['samples'] = payload['payload']['samples'][:-1]
root.joinpath('invalid-payload.json').write_text(json.dumps(payload, separators=(',', ':')))
PYBAD
run_invalid_fixture() {
  local name=$1 key=$2 value_file=$3 reason=$4 before after rows_before rows_after log_before
  before=$(committed_offset)
  rows_before=$(vm_internal_total vm_rows_inserted_total)
  log_before=$(grep -c "\"reason_code\":\"$reason\"" "$TEMP_DIR/marshaller.log" || true)
  produce_record "$key" "$value_file"
  wait_commit_after "$before" || { cat "$TEMP_DIR/marshaller.log" >&2; fail "$name fixture did not commit and continue."; return 1; }
  sleep .5
  after=$(committed_offset)
  rows_after=$(vm_internal_total vm_rows_inserted_total)
  [[ $rows_after == "$rows_before" ]] || { fail "$name fixture inserted VictoriaMetrics rows ($rows_before -> $rows_after)."; return 1; }
  (( $(grep -c "\"reason_code\":\"$reason\"" "$TEMP_DIR/marshaller.log" || true) > log_before )) || { fail "$name fixture did not log $reason."; return 1; }
  info "$name fixture was rejected without storage rows and committed offset $before -> $after."
}
run_invalid_fixture structural 11111111111111111111111111111111 "$TEMP_DIR/invalid-structure.json" invalid_json
run_invalid_fixture key-mismatch 33333333333333333333333333333333 "$TEMP_DIR/invalid-mismatch.json" message_id_mismatch
run_invalid_fixture payload-contract 44444444444444444444444444444444 "$TEMP_DIR/invalid-payload.json" invalid_sample_set

BEFORE=$(committed_offset)
start_monitor
wait_new_record_after "$BEFORE" || fail 'Real upstream did not publish after permanent invalid fixtures.'
wait_commit_after "$BEFORE" || fail 'A real valid record did not commit after permanent invalid fixtures.'
wait_metric_value gopulse_redis_up 1 || fail 'A real valid sample did not write after permanent invalid fixtures.'
info 'Three representative permanent failures were skipped and the same real upstream partition continued.'

compose stop redis >/dev/null
wait_metric_value gopulse_redis_up 0 || fail 'target_unavailable up=0 did not reach VictoriaMetrics.'
compose start redis >/dev/null
wait_health redis
wait_metric_value gopulse_redis_up 1 || fail 'Redis recovery up=1 did not reach VictoriaMetrics.'
for metric in "${SUCCESS_METRICS[@]}"; do wait_metric_presence "$metric" || fail "recovery missing metric $metric"; done
info 'Target unavailable and recovery returned the complete metric family without restarting Router, Marshaller, or Monitor.'

RETRIES_BEFORE=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true)
SAME_PROCESS_PID=$MARSHALLER_PID
compose stop victoriametrics >/dev/null
for _ in {1..80}; do
  RETRIES_NOW=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true)
  ((RETRIES_NOW > RETRIES_BEFORE)) && break
  sleep .25
done
((RETRIES_NOW > RETRIES_BEFORE)) || fail 'Marshaller did not observe the VictoriaMetrics outage.'
BEFORE=$(committed_offset)
[[ $(http_status "http://127.0.0.1:$MARSHALLER_PORT/health") == 200 ]] || fail 'Marshaller health did not remain live during the VM outage.'
wait_not_ready "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || fail 'Marshaller readiness did not fail during the VM outage.'
wait_new_record_after "$BEFORE" || fail 'Kafka did not receive a record during the VictoriaMetrics outage.'
sleep 1
DURING=$(committed_offset)
[[ $DURING == "$BEFORE" ]] || fail "offset advanced during VictoriaMetrics outage ($BEFORE -> $DURING)."
compose start victoriametrics >/dev/null
wait_health victoriametrics
wait_health elasticsearch
wait_http "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || fail 'Marshaller readiness did not recover with VictoriaMetrics.'
wait_commit_after "$BEFORE" || fail 'offset did not advance after VictoriaMetrics recovery.'
[[ $MARSHALLER_PID == "$SAME_PROCESS_PID" ]] && kill -0 "$MARSHALLER_PID" || fail 'VM recovery replaced the Marshaller process unexpectedly.'
info 'Temporary storage failure retained the offset and recovered in the same Marshaller process.'

RETRIES_BEFORE=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true)
compose stop victoriametrics >/dev/null
for _ in {1..80}; do
  RETRIES_NOW=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true)
  ((RETRIES_NOW > RETRIES_BEFORE)) && break
  sleep .25
done
((RETRIES_NOW > RETRIES_BEFORE)) || fail 'Marshaller did not retry before the restart recovery scenario.'
BEFORE=$(committed_offset)
wait_new_record_after "$BEFORE" || fail 'No uncommitted Kafka record existed before Marshaller termination.'
sleep 1
[[ $(committed_offset) == "$BEFORE" ]] || fail 'offset advanced before the explicit process restart.'
OLD_MARSHALLER_PID=$MARSHALLER_PID
stop_process "$MARSHALLER_PID" "$TEMP_DIR/marshaller"
MARSHALLER_PID=
compose start victoriametrics >/dev/null
wait_health victoriametrics
wait_health elasticsearch
start_marshaller
[[ $MARSHALLER_PID != "$OLD_MARSHALLER_PID" ]] || fail 'Marshaller restart did not create a new owned process.'
wait_commit_after "$BEFORE" || fail 'Restarted Marshaller did not re-fetch and commit the uncommitted record.'
info 'An explicitly uncommitted record was recovered from the formal group offset after Marshaller restart.'

BROKER_PROCESS_PID=$MARSHALLER_PID
BEFORE=$(committed_offset)
compose stop kafka >/dev/null
[[ $(http_status "http://127.0.0.1:$MARSHALLER_PORT/health") == 200 ]] || fail 'Marshaller health did not remain live during broker outage.'
wait_not_ready "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || fail 'Marshaller readiness did not fail during broker outage.'
compose start kafka >/dev/null
wait_health kafka
refresh_container_id kafka
wait_http "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || fail 'Marshaller readiness did not recover after broker restart.'
wait_group_assignment || fail 'Formal consumer group did not rejoin after broker restart.'
[[ $MARSHALLER_PID == "$BROKER_PROCESS_PID" ]] && kill -0 "$MARSHALLER_PID" || fail 'Broker recovery replaced the Marshaller process unexpectedly.'
info 'Kafka broker restart forced a group rejoin while Marshaller stayed live and recovered readiness.'

stop_process "$MONITOR_PID" "$TEMP_DIR/monitor"
MONITOR_PID=
REPLAY_KEY=$REAL_KEY
REPLAY_TIMESTAMP_MS=$REAL_TIMESTAMP_MS
BEFORE=$(committed_offset)
produce_record "$REPLAY_KEY" "$TEMP_DIR/real.json"
wait_commit_after "$BEFORE" || fail 'The captured real Envelope replay was not committed.'
python3 - "$REPLAY_TIMESTAMP_MS" >"$TEMP_DIR/replay-window" <<'PY'
import sys
value = int(sys.argv[1]) / 1000
print(f'{value - 0.001:.3f}')
print(f'{value + 0.001:.3f}')
PY
mapfile -t REPLAY_WINDOW <"$TEMP_DIR/replay-window"
REPLAY_VISIBLE=0
for _ in {1..120}; do
  if curl -fsS --max-time 5 --user "$VM_USER:$VM_PASSWORD" \
    --data-urlencode 'query=gopulse_redis_up{source="redis",target_id="redis-exporter-local"}' \
    --data-urlencode "start=${REPLAY_WINDOW[0]}" --data-urlencode "end=${REPLAY_WINDOW[1]}" --data-urlencode 'step=1ms' \
    "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query_range" >"$TEMP_DIR/replay-query.json" && \
    python3 - "$TEMP_DIR/replay-query.json" "$REPLAY_TIMESTAMP_MS" <<'PYREPLAY'
import json
import sys
value = json.load(open(sys.argv[1], encoding='utf-8'))
target = int(sys.argv[2]) / 1000
rows = value.get('data', {}).get('result', [])
if value.get('status') != 'success' or len(rows) != 1:
    raise SystemExit(1)
points = [point for point in rows[0].get('values', []) if abs(float(point[0]) - target) < 0.0005]
raise SystemExit(0 if len(points) == 1 and float(points[0][1]) == 1 else 1)
PYREPLAY
  then
    REPLAY_VISIBLE=1
    break
  fi
  sleep .25
done
((REPLAY_VISIBLE == 1)) || { cat "$TEMP_DIR/replay-query.json" >&2; fail 'Deterministic replay point did not become query-visible.'; }
info "The captured real Envelope message_id=$REPLAY_KEY was replayed and remained one stable millisecond point under 1ms dedup."

NOW=$(date +%s)
START=$((NOW - 180))
curl -fsS --max-time 5 --user "$VM_USER:$VM_PASSWORD" --data-urlencode 'query=gopulse_redis_up{source="redis",target_id="redis-exporter-local"}' --data-urlencode "start=$START" --data-urlencode "end=$NOW" --data-urlencode 'step=1s' "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query_range" >"$TEMP_DIR/range.json"
python3 - "$TEMP_DIR/range.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding='utf-8'))
assert value.get('status') == 'success' and value.get('data', {}).get('result')
PY
INVALID_AFTER=$(vm_invalid_total)
python3 - "$INVALID_BEFORE" "$INVALID_AFTER" <<'PY'
import sys
before, after = map(float, sys.argv[1:])
assert after == before, (before, after)
PY
kill -0 "$ROUTER_PID" && kill -0 "$MARSHALLER_PID" || fail 'An isolated process exited unexpectedly.'

cleanup
[[ -z $(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.ID}}') ]] || fail 'isolated containers remained.'
[[ -z $(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Name}}') ]] || fail 'isolated volumes remained.'
[[ -z $(docker network ls --filter "label=com.docker.compose.project=$PROJECT" --format '{{.ID}}') ]] || fail 'isolated networks remained.'
for port in "$KAFKA_PORT" "$REDIS_PORT" "$VM_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT"; do
  [[ -z $(ss -ltnH "sport = :$port" 2>/dev/null) ]] || fail "port $port remained open."
done
info 'Acceptance passed: full real success/up0/recovery matrix, three permanent-invalid continuations, internal access boundaries, two-member rebalance, broker/group, and storage recovery, captured-real deterministic replay, offset safety, invalid-row stability, and owned cleanup were verified.'
