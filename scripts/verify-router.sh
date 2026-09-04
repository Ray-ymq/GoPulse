#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TOPIC=gopulse-observability-v1
ROUTER_TOKEN=verify-router-token-at-least-32-bytes-long
MONITOR_TOKEN=verify-monitor-token-at-least-32-bytes-long
TEMP_DIR= PROJECT= KAFKA_PORT= REDIS_PORT= ROUTER_PORT= MONITOR_PORT= EXPORTER_PORT=
ROUTER_PID= MONITOR_PID= CLEANED=0

info(){ printf '[verify-router] %s\n' "$*"; }
fail(){ printf '[verify-router] ERROR: %s\n' "$*" >&2; return 1; }
free_port(){ python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
}
compose(){ docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" "$@"; }
container_id(){ docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --filter "label=com.docker.compose.service=$1" --format '{{.ID}}'; }

stop_owned(){
  local name=$1 pid=$2 executable=$3 actual
  [[ -n $pid ]] || return 0
  kill -0 "$pid" 2>/dev/null || return 0
  actual=$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)
  if [[ $actual != "$(readlink -f "$executable")" ]]; then
    fail "refusing to stop $name PID $pid because executable ownership changed"
    return 1
  fi
  kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  for _ in {1..50}; do kill -0 "$pid" 2>/dev/null || return 0; sleep .1; done
  kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
}

cleanup_resources(){
  local status=${1:-0}
  ((CLEANED == 0)) || return "$status"
  CLEANED=1
  stop_owned Monitor "${MONITOR_PID:-}" "$TEMP_DIR/monitor" || status=1
  stop_owned Router "${ROUTER_PID:-}" "$TEMP_DIR/router" || status=1
  if [[ -n ${PROJECT:-} && $PROJECT =~ ^gopulse-router-[a-f0-9]{12}$ && -f ${TEMP_DIR:-}/compose.yaml ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1 || status=1
  fi
  return "$status"
}

cleanup(){
  local status=$?
  trap - EXIT INT TERM
  cleanup_resources "$status" || status=1
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
  exit "$status"
}
trap cleanup EXIT INT TERM

wait_kafka(){
  local id state
  for _ in {1..120}; do
    id=$(container_id kafka)
    if [[ -n $id ]]; then
      state=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null || true)
      [[ $state == healthy ]] && return 0
    fi
    sleep .25
  done
  compose ps >&2 || true
  fail 'Kafka did not become healthy.'
}

wait_router_ready(){
  local expected=${1:-200} status
  for _ in {1..80}; do
    status=$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ROUTER_TOKEN" "http://127.0.0.1:$ROUTER_PORT/ready" 2>/dev/null || true)
    [[ $status == "$expected" ]] && return 0
    sleep .2
  done
  fail "Router readiness did not become HTTP $expected."
}

start_router(){
  env ROUTER_HTTP_HOST=127.0.0.1 ROUTER_HTTP_PORT="$ROUTER_PORT" ROUTER_API_TOKEN="$ROUTER_TOKEN" \
    ROUTER_REQUEST_TIMEOUT=5s ROUTER_SHUTDOWN_TIMEOUT=5s ROUTER_MAX_MESSAGE_BYTES=1048576 \
    ROUTER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" ROUTER_KAFKA_TOPIC="$TOPIC" ROUTER_KAFKA_PRODUCE_TIMEOUT=3s \
    ROUTER_KAFKA_MAX_BUFFERED_RECORDS=256 ROUTER_KAFKA_MAX_BUFFERED_BYTES=8388608 \
    setsid "$TEMP_DIR/router" >"$TEMP_DIR/router.log" 2>&1 &
  ROUTER_PID=$!
  for _ in {1..50}; do
    kill -0 "$ROUTER_PID" 2>/dev/null || { cat "$TEMP_DIR/router.log" >&2; fail 'Router exited during startup.'; return; }
    curl -fsS --max-time 1 "http://127.0.0.1:$ROUTER_PORT/health" >/dev/null 2>&1 && break
    sleep .1
  done
  wait_router_ready 200
}

start_monitor(){
  env MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" \
    MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" MONITOR_PLUGIN_STARTUP_TIMEOUT=10s MONITOR_PLUGIN_STOP_TIMEOUT=4s \
    MONITOR_SCRAPE_INTERVAL=1s MONITOR_SCRAPE_TIMEOUT=800ms MONITOR_PUBLISH_TIMEOUT=4s \
    MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" \
    REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 \
    REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT" \
    REDIS_EXPORTER_SCRAPE_TIMEOUT=800ms REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s \
    setsid "$TEMP_DIR/monitor" >"$TEMP_DIR/monitor.log" 2>&1 &
  MONITOR_PID=$!
  for _ in {1..100}; do
    kill -0 "$MONITOR_PID" 2>/dev/null || { cat "$TEMP_DIR/monitor.log" >&2; fail 'Monitor exited during startup.'; return; }
    curl -fsS --max-time 1 -H "Authorization: Bearer $MONITOR_TOKEN" "http://127.0.0.1:$MONITOR_PORT/ready" >/dev/null 2>&1 && return 0
    sleep .1
  done
  cat "$TEMP_DIR/monitor.log" >&2
  fail 'Monitor did not become ready.'
}

end_offset(){
  local id output
  id=$(container_id kafka); [[ -n $id ]] || return 1
  output=$(docker exec "$id" /opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" 2>/dev/null) || return 1
  awk -F: -v topic="$TOPIC" '$1==topic && $2==0 {print $3}' <<<"$output"
}

wait_end_after(){
  local baseline=$1 value
  for _ in {1..120}; do
    value=$(end_offset 2>/dev/null || true)
    [[ $value =~ ^[0-9]+$ ]] && ((value > baseline)) && { printf '%s\n' "$value"; return 0; }
    sleep .25
  done
  fail "Kafka end offset did not advance beyond $baseline."
}

consume_range(){
  local start=$1 end=$2 output=$3
  "$TEMP_DIR/verify-consumer" --brokers "127.0.0.1:$KAFKA_PORT" --topic "$TOPIC" --client-id "gopulse-verify-$TOKEN_ID" --partition 0 --start "$start" --end "$end" --timeout 15s > "$output"
}

assert_status(){
  local path=$1 expected=$2
  python3 - "$path" "$expected" <<'PY'
import base64,json,sys
path,expected=sys.argv[1:]
for line in open(path, encoding='utf-8'):
    row=json.loads(line); value=json.loads(base64.b64decode(row['value_base64']))
    if value.get('schema_version')==1 and value.get('type')=='metrics' and value.get('source')=='redis' and value.get('payload',{}).get('scrape_status')==expected:
        assert row['key']==value['message_id'] and row['partition']==0 and row['offset']>=0
        raise SystemExit(0)
raise SystemExit(f'missing real Monitor envelope with scrape_status={expected}')
PY
}

emit_record_evidence(){
  local label=$1 start=$2 end=$3 path=$4 expected=${5:-} body_path=${6:-} evidence
  evidence=$(python3 - "$label" "$start" "$end" "$path" "$expected" "$body_path" <<'PY'
import base64, hashlib, json, sys
label, start, end, path, expected, body_path = sys.argv[1:]
rows = [json.loads(line) for line in open(path, encoding='utf-8')]
selected = None
for row in rows:
    raw = base64.b64decode(row['value_base64'])
    value = json.loads(raw)
    if not expected or value.get('payload', {}).get('scrape_status') == expected:
        selected = (row, raw, value)
        break
if selected is None:
    raise SystemExit(f'no evidence record matched {label}')
row, raw, value = selected
evidence = {
    'kind': 'kafka_record',
    'label': label,
    'start_offset': int(start),
    'end_offset': int(end),
    'record_offset': row['offset'],
    'message_id': value['message_id'],
    'key': row['key'],
    'value_sha256': hashlib.sha256(raw).hexdigest(),
}
if expected:
    evidence['scrape_status'] = expected
if body_path:
    body = open(body_path, 'rb').read()
    evidence['body_sha256'] = hashlib.sha256(body).hexdigest()
    evidence['byte_equal'] = body == raw
print(json.dumps(evidence, separators=(',', ':'), sort_keys=True))
PY
  )
  info "evidence $evidence"
}

wait_status_after(){
  local baseline=$1 expected=$2 output=$3 end
  for _ in {1..120}; do
    end=$(end_offset 2>/dev/null || true)
    if [[ $end =~ ^[0-9]+$ ]] && ((end > baseline)); then
      consume_range "$baseline" "$end" "$output" >/dev/null 2>&1 || true
      if assert_status "$output" "$expected" >/dev/null 2>&1; then
        printf '%s\n' "$end"
        return 0
      fi
    fi
    sleep .25
  done
  fail "Kafka did not contain a real Monitor $expected envelope after offset $baseline."
}

post_body(){
  local body_file=$1 output_file=$2
  curl --silent --show-error --max-time 8 --output "$output_file" --write-out '%{http_code}' \
    -H "Authorization: Bearer $ROUTER_TOKEN" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: $(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["message_id"])' "$body_file")" \
    --data-binary "@$body_file" "http://127.0.0.1:$ROUTER_PORT/internal/v1/messages"
}

make_body(){
  python3 - "$1" <<'PY'
import json,secrets,sys
value={'schema_version':1,'message_id':secrets.token_hex(16),'type':'metrics','source':'redis','timestamp':'2026-09-03T12:34:56.123456789Z','payload':{'probe':'byte-for-byte'}}
with open(sys.argv[1],'w',encoding='utf-8') as f:
    f.write('{ "schema_version": 1, "message_id": '+json.dumps(value['message_id'])+', "type": "metrics", "source": "redis", "timestamp": "2026-09-03T12:34:56.123456789Z", "payload": {"probe":"byte-for-byte"} }')
PY
}

self_test(){
  local d
  d=$(mktemp -d)
  trap 'rm -rf -- "$d"' RETURN
  (cd "$REPO_ROOT/router" && go build -o "$d/router" ./cmd/router && go build -o "$d/consumer" ./cmd/verify-consumer)
  if ROUTER_API_TOKEN=short "$d/router" >/dev/null 2>&1; then fail 'Router accepted a short API token.'; return 1; fi
  if "$d/consumer" --end 0 >/dev/null 2>&1; then fail 'Consumer accepted an empty offset range.'; return 1; fi
  docker compose --env-file "$REPO_ROOT/.env.example" --file "$REPO_ROOT/deploy/compose.yaml" config --quiet
  grep -q 'KAFKA_AUTO_CREATE_TOPICS_ENABLE: "false"' "$REPO_ROOT/deploy/compose.yaml" || fail 'Compose does not disable Kafka topic auto-creation.'
  info 'Self-test passed.'
}

if [[ ${1:-} == --self-test ]]; then self_test; exit 0; elif (($#)); then fail "Unknown argument: $1"; exit 2; fi
for tool in docker curl python3 go setsid ss; do command -v "$tool" >/dev/null || { fail "$tool is required."; exit 1; }; done
docker compose version >/dev/null 2>&1 || { fail 'Docker Compose is required.'; exit 1; }
docker info >/dev/null 2>&1 || { fail 'Docker daemon is unavailable.'; exit 1; }

TEMP_DIR=$(mktemp -d)
TOKEN_ID=$(python3 -c 'import secrets; print(secrets.token_hex(6))')
PROJECT="gopulse-router-$TOKEN_ID"
KAFKA_PORT=$(free_port); REDIS_PORT=$(free_port); ROUTER_PORT=$(free_port); MONITOR_PORT=$(free_port); EXPORTER_PORT=$(free_port)
REDIS_PASSWORD="redis-$TOKEN_ID-secret"
cat > "$TEMP_DIR/compose.yaml" <<YAML
services:
  redis:
    image: redis:7.2.5-alpine
    command: ["redis-server","--requirepass","$REDIS_PASSWORD"]
    ports: ["127.0.0.1:$REDIS_PORT:6379"]
    healthcheck:
      test: ["CMD-SHELL","redis-cli --no-auth-warning -a '$REDIS_PASSWORD' ping | grep -q PONG"]
      interval: 2s
      timeout: 2s
      retries: 30
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
    healthcheck:
      test: ["CMD-SHELL","/opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --list >/dev/null 2>&1"]
      interval: 2s
      timeout: 3s
      retries: 60
volumes:
  kafka_data:
YAML

info "Starting isolated Kafka and Redis project $PROJECT."
compose up -d >/dev/null
wait_kafka
KAFKA_ID=$(container_id kafka); [[ $(wc -w <<<"$KAFKA_ID") == 1 ]] || fail 'Kafka ownership is ambiguous.'
docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --create --if-not-exists --topic "$TOPIC" --partitions 1 --replication-factor 1 >/dev/null
DESCRIBE=$(docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --describe --topic "$TOPIC")
[[ $DESCRIBE == *'PartitionCount: 1'* && $DESCRIBE == *'ReplicationFactor: 1'* ]] || fail 'Topic contract is invalid.'

(cd "$REPO_ROOT/router" && go build -o "$TEMP_DIR/router" ./cmd/router && go build -o "$TEMP_DIR/verify-consumer" ./cmd/verify-consumer)
(cd "$REPO_ROOT/monitor" && go build -o "$TEMP_DIR/monitor" ./cmd/monitor)
PACKAGE=$($REPO_ROOT/scripts/package-redis-exporter.sh --output "$TEMP_DIR/redis-exporter.tar.gz")
start_router

BASE=$(end_offset); make_body "$TEMP_DIR/direct.json"
STATUS=$(post_body "$TEMP_DIR/direct.json" "$TEMP_DIR/direct-response.json")
[[ $STATUS == 202 ]] || fail "valid publish returned HTTP $STATUS"
END=$(wait_end_after "$BASE"); consume_range "$BASE" "$END" "$TEMP_DIR/direct-records.jsonl"
python3 - "$TEMP_DIR/direct.json" "$TEMP_DIR/direct-records.jsonl" <<'PY'
import base64,json,sys
body=open(sys.argv[1],'rb').read(); rows=[json.loads(x) for x in open(sys.argv[2])]
assert len(rows)==1 and base64.b64decode(rows[0]['value_base64'])==body
message=json.loads(body); assert rows[0]['key']==message['message_id'] and rows[0]['partition']==0
PY
emit_record_evidence direct "$BASE" "$END" "$TEMP_DIR/direct-records.jsonl" '' "$TEMP_DIR/direct.json"

INVALID_BASE=$(end_offset)
python3 - "$TEMP_DIR/direct.json" "$TEMP_DIR/invalid.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1])); value['topic']='client-controlled'; json.dump(value,open(sys.argv[2],'w'),separators=(',',':'))
PY
STATUS=$(post_body "$TEMP_DIR/invalid.json" "$TEMP_DIR/invalid-response.json")
[[ $STATUS == 400 ]] || fail "invalid request returned HTTP $STATUS"
sleep 1
INVALID_END=$(end_offset)
[[ $INVALID_END == "$INVALID_BASE" ]] || fail 'invalid request wrote a Kafka record.'
INVALID_MESSAGE_ID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["message_id"])' "$TEMP_DIR/invalid.json")
info "evidence {\"end_offset\":$INVALID_END,\"kind\":\"rejected_non_write\",\"message_id\":\"$INVALID_MESSAGE_ID\",\"start_offset\":$INVALID_BASE}"

start_monitor
STATUS=$(curl -sS --max-time 30 -o "$TEMP_DIR/install.json" -w '%{http_code}' -H "Authorization: Bearer $MONITOR_TOKEN" -F "package=@$PACKAGE" "http://127.0.0.1:$MONITOR_PORT/internal/v1/exporter-plugins/install")
[[ $STATUS == 201 ]] || { cat "$TEMP_DIR/install.json" >&2; fail "plugin install returned HTTP $STATUS"; }
SUCCESS_BASE=$(end_offset); SUCCESS_END=$(wait_status_after "$SUCCESS_BASE" success "$TEMP_DIR/success.jsonl"); assert_status "$TEMP_DIR/success.jsonl" success
emit_record_evidence monitor_success "$SUCCESS_BASE" "$SUCCESS_END" "$TEMP_DIR/success.jsonl" success

compose stop redis >/dev/null
UNAVAILABLE_BASE=$(end_offset); UNAVAILABLE_END=$(wait_status_after "$UNAVAILABLE_BASE" target_unavailable "$TEMP_DIR/unavailable.jsonl"); assert_status "$TEMP_DIR/unavailable.jsonl" target_unavailable
emit_record_evidence monitor_target_unavailable "$UNAVAILABLE_BASE" "$UNAVAILABLE_END" "$TEMP_DIR/unavailable.jsonl" target_unavailable

BEFORE_FAILURE=$(end_offset)
ROUTER_PID_BEFORE=$ROUTER_PID
MONITOR_PID_BEFORE=$MONITOR_PID
compose stop kafka >/dev/null
OUTAGE_HEALTH=$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$ROUTER_PORT/health")
[[ $OUTAGE_HEALTH == 200 ]] || fail 'Router health failed while Kafka was stopped.'
wait_router_ready 503
OUTAGE_READY=$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ROUTER_TOKEN" "http://127.0.0.1:$ROUTER_PORT/ready")
make_body "$TEMP_DIR/failure.json"
STATUS=$(post_body "$TEMP_DIR/failure.json" "$TEMP_DIR/failure-response.json")
[[ $STATUS == 503 ]] || fail "publish during Kafka outage returned HTTP $STATUS"
compose start kafka >/dev/null
wait_kafka
wait_router_ready 200
compose start redis >/dev/null
RECOVERY_END=$(wait_status_after "$BEFORE_FAILURE" success "$TEMP_DIR/recovery.jsonl"); assert_status "$TEMP_DIR/recovery.jsonl" success
kill -0 "$ROUTER_PID_BEFORE" && kill -0 "$MONITOR_PID_BEFORE" || fail 'Router or Monitor restarted during Kafka recovery.'
[[ $ROUTER_PID == "$ROUTER_PID_BEFORE" && $MONITOR_PID == "$MONITOR_PID_BEFORE" ]] || fail 'Router or Monitor PID changed during Kafka recovery.'
emit_record_evidence monitor_recovery "$BEFORE_FAILURE" "$RECOVERY_END" "$TEMP_DIR/recovery.jsonl" success
info "evidence {\"health_status\":$OUTAGE_HEALTH,\"kind\":\"kafka_outage_recovery\",\"monitor_pid\":$MONITOR_PID,\"publish_status\":$STATUS,\"ready_status\":$OUTAGE_READY,\"router_pid\":$ROUTER_PID}"

info 'Stopping isolated processes and resources to verify cleanup.'
cleanup_resources 0
[[ -z $(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.ID}}') ]] || fail 'isolated containers remained after cleanup.'
[[ -z $(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Name}}') ]] || fail 'isolated volumes remained after cleanup.'
for port in "$KAFKA_PORT" "$REDIS_PORT" "$ROUTER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT"; do
  [[ -z $(ss -ltnH "sport = :$port" 2>/dev/null) ]] || fail "port $port remained listened on after cleanup"
done
info 'Acceptance passed: Router authentication, strict routing, Kafka acknowledgement, real Monitor success/unavailable messages, byte integrity, outage recovery, bounded consumer evidence, and cleanup were verified.'
