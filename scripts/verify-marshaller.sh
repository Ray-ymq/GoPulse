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
MONITOR_PID=
CLEANED=0

info() { printf '[verify-marshaller] %s\n' "$*"; }
fail() { printf '[verify-marshaller] ERROR: %s\n' "$*" >&2; return 1; }
valid_project() { [[ $1 =~ ^gopulse-marshaller-[a-f0-9]{12}$ ]]; }
valid_topic() { [[ $1 == "$TOPIC" ]]; }
valid_group() { [[ $1 == "$GROUP" ]]; }
valid_metric() {
  [[ $1 == gopulse_redis_up || $1 == gopulse_redis_connected_clients || $1 == gopulse_redis_commands_processed_total || $1 == gopulse_redis_cpu_seconds_total || $1 == gopulse_redis_used_memory_bytes || $1 == gopulse_redis_db_keys || $1 == gopulse_redis_db_expiring_keys ]]
}
ports_unique() {
  python3 - "$@" <<'PY'
import sys
ports = sys.argv[1:]
raise SystemExit(0 if len(ports) == 7 and len(set(ports)) == 7 and all(value.isdigit() and 1024 < int(value) < 65536 for value in ports) else 1)
PY
}
allocate_ports() {
  local values
  values=$(python3 - <<'PY'
import socket
sockets = []
try:
    for _ in range(7):
        sock = socket.socket()
        sock.bind(('127.0.0.1', 0))
        sockets.append(sock)
    print(' '.join(str(sock.getsockname()[1]) for sock in sockets))
finally:
    for sock in sockets:
        sock.close()
PY
  ) || return 1
  read -r KAFKA_PORT REDIS_PORT VM_PORT ROUTER_PORT MARSHALLER_PORT MONITOR_PORT EXPORTER_PORT <<<"$values"
  ports_unique "$KAFKA_PORT" "$REDIS_PORT" "$VM_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT"
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
  ports_unique 11001 11002 11003 11004 11005 11006 11007 || return 1
  if ports_unique 11001 11002 11003 11004 11001 11006 11007; then return 1; fi
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
volumes: {kafka_data: {}, victoriametrics_data: {}}
YAML

compose up -d
wait_health redis
wait_health kafka
wait_health victoriametrics
refresh_container_id kafka

docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --create --topic "$TOPIC" --partitions 1 --replication-factor 1 >/dev/null
(cd "$REPO_ROOT/router" && go build -o "$TEMP_DIR/router" ./cmd/router)
(cd "$REPO_ROOT/marshaller" && go build -o "$TEMP_DIR/marshaller" ./cmd/marshaller)
(cd "$REPO_ROOT/monitor" && go build -o "$TEMP_DIR/monitor" ./cmd/monitor)
"$REPO_ROOT/scripts/package-redis-exporter.sh" --output "$TEMP_DIR/exporter.tar.gz" >/dev/null
: >"$TEMP_DIR/router.log"
: >"$TEMP_DIR/marshaller.log"
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
    MARSHALLER_RETRY_MIN=100ms MARSHALLER_RETRY_MAX=500ms
  wait_http "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN" || { cat "$TEMP_DIR/marshaller.log" >&2; fail 'Marshaller not ready.'; }
}
start_monitor() {
  mkdir -p "$TEMP_DIR/plugins"
  start_process MONITOR_PID "$REPO_ROOT/monitor" "$TEMP_DIR/monitor" "$TEMP_DIR/monitor.log" \
    MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" \
    MONITOR_PLUGIN_STARTUP_TIMEOUT=10s MONITOR_PLUGIN_STOP_TIMEOUT=4s MONITOR_SCRAPE_INTERVAL=1s MONITOR_SCRAPE_TIMEOUT=800ms \
    MONITOR_PUBLISH_TIMEOUT=3s MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" \
    REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 \
    REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT" REDIS_EXPORTER_SCRAPE_TIMEOUT=800ms REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s
  wait_http "http://127.0.0.1:$MONITOR_PORT/ready" "$MONITOR_TOKEN" || { cat "$TEMP_DIR/monitor.log" >&2; fail 'Monitor not ready.'; }
  local status
  status=$(curl -sS --max-time 30 -o "$TEMP_DIR/install.json" -w '%{http_code}' -H "Authorization: Bearer $MONITOR_TOKEN" -F "package=@$TEMP_DIR/exporter.tar.gz" "http://127.0.0.1:$MONITOR_PORT/internal/v1/exporter-plugins/install")
  [[ $status == 201 ]] || { cat "$TEMP_DIR/install.json" >&2; fail "plugin install returned $status"; }
}
vm_query() {
  local metric=$1 output=$2
  valid_metric "$metric" || return 1
  curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" --data-urlencode "query=$metric{source=\"redis\",target_id=\"redis-exporter-local\"}" "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" >"$output"
}
vm_invalid_total() {
  curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" --data-urlencode 'query=sum(vm_rows_invalid_total)' "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" | python3 -c 'import json,sys; rows=json.load(sys.stdin).get("data",{}).get("result",[]); print(rows[0]["value"][1] if rows else "0")'
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
produce_record() {
  local key=$1 value_file=$2
  { printf '%s:' "$key"; cat "$value_file"; printf '\n'; } | docker exec -i "$KAFKA_ID" /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" --property parse.key=true --property key.separator=: >/dev/null
}

INVALID_BEFORE=$(vm_invalid_total)
start_router
start_marshaller
start_monitor
wait_group_assignment || fail 'Formal Marshaller group did not receive the fixed partition.'

wait_metric_value gopulse_redis_up 1 || { cat "$TEMP_DIR/marshaller.log" >&2; fail 'missing success up=1'; }
for metric in gopulse_redis_connected_clients gopulse_redis_commands_processed_total gopulse_redis_cpu_seconds_total gopulse_redis_used_memory_bytes gopulse_redis_db_keys gopulse_redis_db_expiring_keys; do
  wait_metric_presence "$metric" || { cat "$TEMP_DIR/marshaller.log" >&2; fail "missing metric $metric"; }
done
info 'Real Redis success metrics reached VictoriaMetrics.'

BASE=$(committed_offset)
printf 'bad-key:{}\n' | docker exec -i "$KAFKA_ID" /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" --property parse.key=true --property key.separator=: >/dev/null
wait_commit_after "$BASE" || { cat "$TEMP_DIR/marshaller.log" >&2; fail 'permanent invalid record did not commit and continue.'; }
grep -q 'message_id_mismatch' "$TEMP_DIR/marshaller.log" || fail 'permanent rejection reason was not logged.'
info 'Representative permanent invalid record was skipped safely.'

compose stop redis >/dev/null
wait_metric_value gopulse_redis_up 0 || fail 'target_unavailable up=0 did not reach VictoriaMetrics.'
compose start redis >/dev/null
wait_health redis
wait_metric_value gopulse_redis_up 1 || fail 'Redis recovery up=1 did not reach VictoriaMetrics.'
info 'Target unavailable and recovery metrics were queried.'

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
python3 - "$TEMP_DIR/replay.json" "$TEMP_DIR/replay.meta" "$TOKEN_ID" <<'PY'
import datetime
import hashlib
import json
import sys
value_path, meta_path, token = sys.argv[1:]
now = datetime.datetime.now(datetime.timezone.utc)
now = now.replace(microsecond=(now.microsecond // 1000) * 1000)
message_id = hashlib.sha256((token + '-deterministic-replay').encode()).hexdigest()[:32]
samples = [
    {'name':'gopulse_redis_up','kind':'gauge','labels':{},'value':1},
    {'name':'gopulse_redis_uptime_seconds','kind':'gauge','labels':{},'value':12},
    {'name':'gopulse_redis_connected_clients','kind':'gauge','labels':{},'value':2},
    {'name':'gopulse_redis_used_memory_bytes','kind':'gauge','labels':{},'value':1000},
    {'name':'gopulse_redis_commands_processed_total','kind':'counter','labels':{},'value':8},
    {'name':'gopulse_redis_keyspace_hits_total','kind':'counter','labels':{},'value':5},
    {'name':'gopulse_redis_keyspace_misses_total','kind':'counter','labels':{},'value':1},
    {'name':'gopulse_redis_cpu_seconds_total','kind':'counter','labels':{'mode':'user'},'value':1.25},
    {'name':'gopulse_redis_cpu_seconds_total','kind':'counter','labels':{'mode':'system'},'value':0.5},
    {'name':'gopulse_redis_db_keys','kind':'gauge','labels':{'db':'0'},'value':3},
    {'name':'gopulse_redis_db_expiring_keys','kind':'gauge','labels':{'db':'0'},'value':1},
]
document = {'schema_version':1,'message_id':message_id,'type':'metrics','source':'redis','timestamp':now.isoformat(timespec='milliseconds').replace('+00:00','Z'),'payload':{'plugin_id':'redis-exporter','plugin_version':'1.5.2','target_id':'redis-exporter-local','scrape_status':'success','samples':samples}}
open(value_path, 'w', encoding='utf-8').write(json.dumps(document, separators=(',', ':')))
open(meta_path, 'w', encoding='utf-8').write(message_id + '\n' + str(int(now.timestamp() * 1000)) + '\n')
PY
mapfile -t REPLAY_META <"$TEMP_DIR/replay.meta"
REPLAY_KEY=${REPLAY_META[0]}
REPLAY_TIMESTAMP_MS=${REPLAY_META[1]}
BEFORE=$(committed_offset)
produce_record "$REPLAY_KEY" "$TEMP_DIR/replay.json"
wait_commit_after "$BEFORE" || fail 'First deterministic replay fixture was not committed.'
BEFORE=$(committed_offset)
produce_record "$REPLAY_KEY" "$TEMP_DIR/replay.json"
wait_commit_after "$BEFORE" || fail 'Second deterministic replay fixture was not committed.'
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
info 'The same valid Envelope was committed twice and queried as one stable millisecond point.'

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
info 'Acceptance passed: broker/group recovery, same-process and restart storage recovery, deterministic replay, offset safety, fixed queries, invalid-row stability, and owned cleanup were verified.'
