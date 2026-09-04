#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TOPIC=gopulse-observability-v1
GROUP=gopulse-marshaller-metrics-v1
ROUTER_TOKEN=verify-router-token-at-least-32-bytes-long
MONITOR_TOKEN=verify-monitor-token-at-least-32-bytes-long
MARSHALLER_TOKEN=verify-marshaller-token-at-least-32-bytes
VM_USER=gopulse-marshaller
TEMP_DIR= PROJECT= TOKEN_ID= KAFKA_PORT= REDIS_PORT= VM_PORT= ROUTER_PORT= MARSHALLER_PORT= MONITOR_PORT= EXPORTER_PORT= REDIS_PASSWORD= VM_PASSWORD=
ROUTER_PID= MARSHALLER_PID= MONITOR_PID= CLEANED=0

info(){ printf '[verify-marshaller] %s\n' "$*"; }
fail(){ printf '[verify-marshaller] ERROR: %s\n' "$*" >&2; return 1; }
valid_project(){ [[ $1 =~ ^gopulse-marshaller-[a-f0-9]{12}$ ]]; }
valid_topic(){ [[ $1 == "$TOPIC" ]]; }
valid_group(){ [[ $1 == "$GROUP" ]]; }
valid_metric(){ [[ $1 == gopulse_redis_up || $1 == gopulse_redis_connected_clients || $1 == gopulse_redis_commands_processed_total || $1 == gopulse_redis_cpu_seconds_total || $1 == gopulse_redis_used_memory_bytes || $1 == gopulse_redis_db_keys || $1 == gopulse_redis_db_expiring_keys ]]; }
ports_unique(){ python3 - "$@" <<'PY'
import sys
p=sys.argv[1:]
raise SystemExit(0 if len(p)==7 and len(set(p))==7 and all(x.isdigit() and 1024<int(x)<65536 for x in p) else 1)
PY
}
allocate_ports(){
  local values
  values=$(python3 - <<'PY'
import socket
ss=[]
try:
  for _ in range(7):
    s=socket.socket();s.bind(('127.0.0.1',0));ss.append(s)
  print(' '.join(str(s.getsockname()[1]) for s in ss))
finally:
  for s in ss:s.close()
PY
) || return 1
  read -r KAFKA_PORT REDIS_PORT VM_PORT ROUTER_PORT MARSHALLER_PORT MONITOR_PORT EXPORTER_PORT <<<"$values"
  ports_unique "$KAFKA_PORT" "$REDIS_PORT" "$VM_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT"
}
compose(){ docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" "$@"; }
container_id(){ docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --filter "label=com.docker.compose.service=$1" --format '{{.ID}}'; }
wait_health(){ local service=$1 id state; for _ in {1..120};do id=$(container_id "$service");state=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id" 2>/dev/null||true);[[ $state == healthy ]]&&return 0;sleep .25;done;compose ps >&2;fail "$service did not become healthy."; }
wait_http(){ local url=$1 token=${2:-};for _ in {1..100};do if [[ -n $token ]];then curl -fsS --max-time 1 -H "Authorization: Bearer $token" "$url" >/dev/null 2>&1&&return 0;else curl -fsS --max-time 1 "$url" >/dev/null 2>&1&&return 0;fi;sleep .1;done;return 1; }
start_process(){ local var=$1 cwd=$2 binary=$3 log=$4;shift 4;(cd "$cwd"&&exec env "$@" setsid "$binary") >"$log" 2>&1 & local pid=$!;printf -v "$var" '%s' "$pid";sleep .2;kill -0 "$pid" 2>/dev/null||{ cat "$log" >&2;fail "$binary exited during startup.";}; }
stop_process(){ local pid=$1 binary=$2;[[ -n $pid ]]||return 0;[[ -r /proc/$pid/exe && $(readlink -f "/proc/$pid/exe") == "$(readlink -f "$binary")" ]]||return 0;kill -TERM -- "-$pid" 2>/dev/null||true;for _ in {1..50};do kill -0 "$pid" 2>/dev/null||return 0;sleep .1;done;kill -KILL -- "-$pid" 2>/dev/null||true; }
cleanup(){
  local code=$?;((CLEANED==0))||return "$code";CLEANED=1
  stop_process "$MONITOR_PID" "$TEMP_DIR/monitor" || true
  stop_process "$MARSHALLER_PID" "$TEMP_DIR/marshaller" || true
  stop_process "$ROUTER_PID" "$TEMP_DIR/router" || true
  if [[ -n $PROJECT && -n $TEMP_DIR && -f $TEMP_DIR/compose.yaml ]]&&valid_project "$PROJECT";then compose down --volumes --remove-orphans >/dev/null 2>&1||true;fi
  [[ -z $TEMP_DIR ]]||rm -rf -- "$TEMP_DIR"
  return "$code"
}
trap cleanup EXIT INT TERM

self_test(){
  local d rejected=0 pid
  d=$(mktemp -d);trap 'rm -rf -- "$d"' RETURN
  (cd "$REPO_ROOT/marshaller"&&go build -o "$d/marshaller" ./cmd/marshaller)
  if MARSHALLER_API_TOKEN=short MARSHALLER_VM_PASSWORD=password-password "$d/marshaller" >/dev/null 2>&1;then fail 'Marshaller accepted a short token.';return 1;fi;((rejected+=1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_KAFKA_TOPIC=other "$d/marshaller" >/dev/null 2>&1;then fail 'Marshaller accepted an unsafe Topic.';return 1;fi;((rejected+=1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_KAFKA_GROUP=other "$d/marshaller" >/dev/null 2>&1;then fail 'Marshaller accepted an unsafe group.';return 1;fi;((rejected+=1))
  if MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_VM_PASSWORD=password-password MARSHALLER_VM_URL='http://user:pass@127.0.0.1:8428' "$d/marshaller" >/dev/null 2>&1;then fail 'Marshaller accepted credentials in its URL.';return 1;fi;((rejected+=1))
  valid_project gopulse-marshaller-deadbeefcafe||return 1;if valid_project gopulse-marshaller;then return 1;fi;((rejected+=1))
  valid_topic "$TOPIC"&&valid_group "$GROUP"&&valid_metric gopulse_redis_up||return 1
  if valid_topic other||valid_group other||valid_metric arbitrary_query;then return 1;fi;((rejected+=3))
  ports_unique 11001 11002 11003 11004 11005 11006 11007||return 1
  if ports_unique 11001 11002 11003 11004 11001 11006 11007;then return 1;fi;((rejected+=1))
  info "Self-test passed without Docker: $rejected unsafe configuration, project, query, and port cases were rejected."
}
if [[ ${1:-} == --self-test ]];then self_test;exit 0;elif (($#));then fail "Unknown argument: $1";exit 2;fi
for tool in docker curl python3 go setsid ss readlink;do command -v "$tool" >/dev/null||{ fail "$tool is required.";exit 1;};done
docker compose version >/dev/null 2>&1||{ fail 'Docker Compose is required.';exit 1;};docker info >/dev/null 2>&1||{ fail 'Docker daemon is unavailable.';exit 1;}

TEMP_DIR=$(mktemp -d);TOKEN_ID=$(python3 -c 'import secrets;print(secrets.token_hex(6))');PROJECT="gopulse-marshaller-$TOKEN_ID";valid_project "$PROJECT";allocate_ports
REDIS_PASSWORD="redis-$TOKEN_ID-secret";VM_PASSWORD="vm-$TOKEN_ID-secret-password"
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
          wget --spider --quiet --header "Authorization: Basic $$(printf '%s:%s' "$$VM_USER" "$$VM_PASSWORD" | base64)" http://127.0.0.1:8428/health
      interval: 2s
      timeout: 2s
      retries: 60
volumes: {kafka_data: {}, victoriametrics_data: {}}
YAML
compose up -d;wait_health redis;wait_health kafka;wait_health victoriametrics
KAFKA_ID=$(container_id kafka);docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:19092 --create --topic "$TOPIC" --partitions 1 --replication-factor 1 >/dev/null
(cd "$REPO_ROOT/router"&&go build -o "$TEMP_DIR/router" ./cmd/router)
(cd "$REPO_ROOT/marshaller"&&go build -o "$TEMP_DIR/marshaller" ./cmd/marshaller)
(cd "$REPO_ROOT/monitor"&&go build -o "$TEMP_DIR/monitor" ./cmd/monitor)
"$REPO_ROOT/scripts/package-redis-exporter.sh" --output "$TEMP_DIR/exporter.tar.gz" >/dev/null
start_process ROUTER_PID "$REPO_ROOT/router" "$TEMP_DIR/router" "$TEMP_DIR/router.log" ROUTER_HTTP_HOST=127.0.0.1 ROUTER_HTTP_PORT="$ROUTER_PORT" ROUTER_API_TOKEN="$ROUTER_TOKEN" ROUTER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" ROUTER_KAFKA_TOPIC="$TOPIC"
wait_http "http://127.0.0.1:$ROUTER_PORT/ready" "$ROUTER_TOKEN"||{ cat "$TEMP_DIR/router.log" >&2;fail 'Router not ready.';}
start_process MARSHALLER_PID "$REPO_ROOT/marshaller" "$TEMP_DIR/marshaller" "$TEMP_DIR/marshaller.log" MARSHALLER_HTTP_HOST=127.0.0.1 MARSHALLER_HTTP_PORT="$MARSHALLER_PORT" MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" MARSHALLER_KAFKA_TOPIC="$TOPIC" MARSHALLER_KAFKA_GROUP="$GROUP" MARSHALLER_VM_URL="http://127.0.0.1:$VM_PORT" MARSHALLER_VM_USERNAME="$VM_USER" MARSHALLER_VM_PASSWORD="$VM_PASSWORD" MARSHALLER_RETRY_MIN=100ms MARSHALLER_RETRY_MAX=500ms
wait_http "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN"||{ cat "$TEMP_DIR/marshaller.log" >&2;fail 'Marshaller not ready.';}
mkdir -p "$TEMP_DIR/plugins"
start_process MONITOR_PID "$REPO_ROOT/monitor" "$TEMP_DIR/monitor" "$TEMP_DIR/monitor.log" MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" MONITOR_PLUGIN_STARTUP_TIMEOUT=10s MONITOR_PLUGIN_STOP_TIMEOUT=4s MONITOR_SCRAPE_INTERVAL=1s MONITOR_SCRAPE_TIMEOUT=800ms MONITOR_PUBLISH_TIMEOUT=3s MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT" REDIS_EXPORTER_SCRAPE_TIMEOUT=800ms REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s
wait_http "http://127.0.0.1:$MONITOR_PORT/ready" "$MONITOR_TOKEN"||{ cat "$TEMP_DIR/monitor.log" >&2;fail 'Monitor not ready.';}
STATUS=$(curl -sS --max-time 30 -o "$TEMP_DIR/install.json" -w '%{http_code}' -H "Authorization: Bearer $MONITOR_TOKEN" -F "package=@$TEMP_DIR/exporter.tar.gz" "http://127.0.0.1:$MONITOR_PORT/internal/v1/exporter-plugins/install");[[ $STATUS == 201 ]]||{ cat "$TEMP_DIR/install.json" >&2;fail "plugin install returned $STATUS";}

vm_query(){ local metric=$1 output=$2;valid_metric "$metric"||return 1;curl -fsS --max-time 3 --user "$VM_USER:$VM_PASSWORD" --data-urlencode "query=$metric{source=\"redis\",target_id=\"redis-exporter-local\"}" "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" >"$output"; }
wait_metric_value(){ local metric=$1 expected=$2 output="$TEMP_DIR/query.json";for _ in {1..120};do if vm_query "$metric" "$output"&&python3 - "$output" "$expected" <<'PY'
import json,sys
v=json.load(open(sys.argv[1],encoding='utf-8'));want=float(sys.argv[2]);rows=v.get('data',{}).get('result',[])
raise SystemExit(0 if any(float(r['value'][1])==want for r in rows) else 1)
PY
then return 0;fi;sleep .25;done;return 1; }
wait_metric_presence(){ local metric=$1 output="$TEMP_DIR/query.json";for _ in {1..120};do if vm_query "$metric" "$output"&&python3 - "$output" <<'PY'
import json,sys
raise SystemExit(0 if json.load(open(sys.argv[1],encoding='utf-8')).get('data',{}).get('result') else 1)
PY
then return 0;fi;sleep .25;done;return 1; }
wait_metric_value gopulse_redis_up 1||{ cat "$TEMP_DIR/marshaller.log" >&2;fail 'missing success up=1'; }
for metric in gopulse_redis_connected_clients gopulse_redis_commands_processed_total gopulse_redis_cpu_seconds_total gopulse_redis_used_memory_bytes gopulse_redis_db_keys gopulse_redis_db_expiring_keys;do wait_metric_presence "$metric"||{ cat "$TEMP_DIR/marshaller.log" >&2;fail "missing metric $metric"; };done
info 'Real Redis success metrics reached VictoriaMetrics.'

committed_offset(){ docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server 127.0.0.1:19092 --group "$GROUP" --describe 2>/dev/null|awk -v t="$TOPIC" '$2==t&&$3==0{print $4;exit}'; }
end_offset(){ docker exec "$KAFKA_ID" /opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" 2>/dev/null|awk -F: '$2==0{print $3}'; }
wait_commit_after(){ local base=$1 value;for _ in {1..120};do value=$(committed_offset||true);[[ $value =~ ^[0-9]+$ ]]&&((value>base))&&return 0;sleep .25;done;return 1; }
BASE=$(committed_offset);printf 'bad-key:{}\n'|docker exec -i "$KAFKA_ID" /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server 127.0.0.1:19092 --topic "$TOPIC" --property parse.key=true --property key.separator=: >/dev/null
wait_commit_after "$BASE"||{ cat "$TEMP_DIR/marshaller.log" >&2;fail 'permanent invalid record did not commit and continue.';}
grep -q 'message_id_mismatch' "$TEMP_DIR/marshaller.log"||fail 'permanent rejection reason was not logged.'
info 'Representative permanent invalid record was skipped safely.'

compose stop redis >/dev/null;wait_metric_value gopulse_redis_up 0||fail 'target_unavailable up=0 did not reach VictoriaMetrics.';compose start redis >/dev/null;wait_health redis;wait_metric_value gopulse_redis_up 1||fail 'Redis recovery up=1 did not reach VictoriaMetrics.'
info 'Target unavailable and recovery metrics were queried.'

RETRIES_BEFORE=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true);compose stop victoriametrics >/dev/null
for _ in {1..80};do RETRIES_NOW=$(grep -c 'write_retry' "$TEMP_DIR/marshaller.log" || true);((RETRIES_NOW>RETRIES_BEFORE))&&break;sleep .25;done
((RETRIES_NOW>RETRIES_BEFORE))||fail 'Marshaller did not observe the VictoriaMetrics outage.'
BEFORE=$(committed_offset);for _ in {1..80};do END=$(end_offset);((END>BEFORE))&&break;sleep .25;done;((END>BEFORE))||fail 'Kafka did not receive a record during the VictoriaMetrics outage.'
sleep 1;DURING=$(committed_offset);[[ $DURING == "$BEFORE" ]]||fail "offset advanced during VictoriaMetrics outage ($BEFORE -> $DURING).";compose start victoriametrics >/dev/null;wait_health victoriametrics;wait_commit_after "$BEFORE"||fail 'offset did not advance after VictoriaMetrics recovery.'
info 'Temporary storage failure retained the offset and recovered.'

NOW=$(date +%s);START=$((NOW-120));curl -fsS --max-time 5 --user "$VM_USER:$VM_PASSWORD" --data-urlencode 'query=gopulse_redis_up{source="redis",target_id="redis-exporter-local"}' --data-urlencode "start=$START" --data-urlencode "end=$NOW" --data-urlencode 'step=1s' "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query_range" >"$TEMP_DIR/range.json";python3 - "$TEMP_DIR/range.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1]));assert v.get('status')=='success' and v.get('data',{}).get('result')
PY
curl -fsS --max-time 5 --user "$VM_USER:$VM_PASSWORD" --data-urlencode 'query=vm_rows_invalid_total' "http://127.0.0.1:$VM_PORT/prometheus/api/v1/query" >"$TEMP_DIR/invalid.json";python3 - "$TEMP_DIR/invalid.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1]));rows=v.get('data',{}).get('result',[]);assert all(float(r['value'][1])==0 for r in rows)
PY
kill -0 "$ROUTER_PID"&&kill -0 "$MARSHALLER_PID"&&kill -0 "$MONITOR_PID"||fail 'an isolated process exited unexpectedly.'
cleanup
[[ -z $(docker ps -a --filter "label=com.docker.compose.project=$PROJECT" --format '{{.ID}}') ]]||fail 'isolated containers remained.'
[[ -z $(docker volume ls --filter "label=com.docker.compose.project=$PROJECT" --format '{{.Name}}') ]]||fail 'isolated volumes remained.'
for p in "$KAFKA_PORT" "$REDIS_PORT" "$VM_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$MONITOR_PORT" "$EXPORTER_PORT";do [[ -z $(ss -ltnH "sport = :$p" 2>/dev/null) ]]||fail "port $p remained open.";done
info 'Acceptance passed: real upstream metrics, strict invalid-record continuation, manual offset safety, VictoriaMetrics queries, temporary storage failure, and cleanup were verified.'
