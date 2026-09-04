#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TOKEN_ID=
PROJECT=
TEMP_DIR=
PIDS=()

info(){ printf '[verify-logs] %s\n' "$*"; }
fail(){ printf '[verify-logs] ERROR: %s\n' "$*" >&2; return 1; }
free_ports(){ python3 - "$1" <<'PY'
import socket,sys
count=int(sys.argv[1]); sockets=[]
try:
 for _ in range(count):
  s=socket.socket(); s.bind(('127.0.0.1',0)); sockets.append(s)
 print(' '.join(str(s.getsockname()[1]) for s in sockets))
finally:
 for s in sockets:s.close()
PY
}
valid_project(){ [[ ${1:-} =~ ^gopulse-logs-[0-9a-f]{12}$ ]]; }
stop_process(){ local pid=$1 binary=$2; [[ -n $pid && -r /proc/$pid/exe ]] || return 0; [[ $(readlink -f "/proc/$pid/exe") == "$(readlink -f "$binary")" ]] || return 0; kill -TERM -- "-$pid" 2>/dev/null || true; for _ in {1..30};do if ! kill -0 "$pid" 2>/dev/null;then wait "$pid" 2>/dev/null||true;return 0;fi;sleep .1;done; kill -KILL -- "-$pid" 2>/dev/null||true; wait "$pid" 2>/dev/null||true; }
cleanup(){ local code=$?; trap - EXIT INT TERM; if [[ -n $TEMP_DIR ]];then for ((i=${#PIDS[@]}-1;i>=0;i--));do entry=${PIDS[$i]}; [[ -n $entry ]]||continue;stop_process "${entry%%:*}" "${entry#*:}"||true;done;fi; if [[ -n $PROJECT && -n $TEMP_DIR && -f $TEMP_DIR/compose.yaml ]]&&valid_project "$PROJECT";then docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" down --volumes --remove-orphans >/dev/null 2>&1||true;fi; [[ -n $TEMP_DIR ]]&&rm -rf -- "$TEMP_DIR"; exit "$code"; }
trap cleanup EXIT INT TERM

self_test(){
  local directory backend
  directory=$(mktemp -d); trap 'rm -rf -- "$directory"' RETURN
  [[ $(<"$REPO_ROOT/VERSION") =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'VERSION is not SemVer.'
  grep -q 'gopulse-logs-v1-read' "$REPO_ROOT/backend/internal/logquery/logquery.go" || fail 'Fixed read alias is missing.'
  grep -q 'gopulse-logs-v1-template' "$REPO_ROOT/marshaller/internal/elasticsearch/client.go" || fail 'Fixed log template is missing.'
  grep -q 'LOG_MONITOR_INGEST_TOKEN' "$REPO_ROOT/.env.example" || fail 'Dedicated ingest token is missing.'
  (cd "$REPO_ROOT/backend" && go build -o "$directory/backend" ./cmd/server)
  backend=(env APP_ENV=test MYSQL_DATABASE=x MYSQL_USER=x MYSQL_PASSWORD=x REDIS_PASSWORD=x RABBITMQ_URL=amqp://x:x@127.0.0.1:5672/ AUTH_JWT_SECRET=01234567890123456789012345678901 MONITOR_API_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF)
  if "${backend[@]}" LOG_MONITOR_URL=http://example.com LOG_MONITOR_INGEST_TOKEN=01234567890123456789012345678901 "$directory/backend" >/dev/null 2>&1; then fail 'Backend accepted a non-loopback log URL.'; return 1; fi
  if "${backend[@]}" LOG_MONITOR_URL=http://127.0.0.1:9090 LOG_MONITOR_INGEST_TOKEN=01234567890123456789012345678901 LOG_SHIP_QUEUE_CAPACITY=0 "$directory/backend" >/dev/null 2>&1; then fail 'Backend accepted a zero-capacity log queue.'; return 1; fi
  info 'Self-test passed.'
}
if [[ ${1:-} == --self-test ]];then self_test;exit 0;elif (($#));then fail "unknown argument: $1";exit 2;fi
for tool in docker curl python3 go;do command -v "$tool" >/dev/null||{ fail "$tool is required";exit 1;};done
docker compose version >/dev/null
TEMP_DIR=$(mktemp -d); TOKEN_ID=$(python3 -c 'import secrets;print(secrets.token_hex(6))'); PROJECT="gopulse-logs-$TOKEN_ID"; valid_project "$PROJECT"
read -r MYSQL_PORT REDIS_PORT RABBIT_PORT KAFKA_PORT ES_PORT VM_PORT ROUTER_PORT MARSHALLER_PORT MONITOR_PORT BACKEND_PORT <<<"$(free_ports 10)"
MYSQL_PASSWORD="mysql-$TOKEN_ID"; REDIS_PASSWORD="redis-$TOKEN_ID"; ROUTER_TOKEN="router-$TOKEN_ID-0123456789abcdef0123456789"; MARSHALLER_TOKEN="marshaller-$TOKEN_ID-0123456789abcdef"; MONITOR_TOKEN="monitor-$TOKEN_ID-0123456789abcdef0123"; INGEST_TOKEN="ingest-$TOKEN_ID-0123456789abcdef012345"; JWT_SECRET="jwt-$TOKEN_ID-0123456789abcdef0123456789"; VM_PASSWORD="vm-$TOKEN_ID-0123456789abcdef"; VM_BASIC=$(printf '%s:%s' gopulse-marshaller "$VM_PASSWORD" | base64 -w0)
cat >"$TEMP_DIR/compose.yaml" <<YAML
services:
  mysql:
    image: mysql:8.4.0
    environment: {MYSQL_DATABASE: gopulse_logs, MYSQL_USER: gopulse, MYSQL_PASSWORD: "$MYSQL_PASSWORD", MYSQL_ROOT_PASSWORD: "$MYSQL_PASSWORD"}
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
mkdir -p "$TEMP_DIR/bin" "$TEMP_DIR/plugins"
(cd "$REPO_ROOT/backend"&&go build -o "$TEMP_DIR/bin/backend" ./cmd/server&&go build -o "$TEMP_DIR/bin/business-worker" ./cmd/business-worker&&go build -o "$TEMP_DIR/bin/search-indexer" ./cmd/search-indexer&&go build -o "$TEMP_DIR/bin/search-reindex" ./cmd/search-reindex&&go build -o "$TEMP_DIR/bin/migrate" ./cmd/migrate&&go build -o "$TEMP_DIR/bin/admin-role" ./cmd/admin-role)
(cd "$REPO_ROOT/router"&&go build -o "$TEMP_DIR/bin/router" ./cmd/router)
(cd "$REPO_ROOT/monitor"&&go build -o "$TEMP_DIR/bin/monitor" ./cmd/monitor)
(cd "$REPO_ROOT/marshaller"&&go build -o "$TEMP_DIR/bin/marshaller" ./cmd/marshaller)
LOGGING=(LOG_MONITOR_URL="http://127.0.0.1:$MONITOR_PORT" LOG_MONITOR_INGEST_TOKEN="$INGEST_TOKEN" LOG_SHIP_REQUEST_TIMEOUT=500ms LOG_SHIP_QUEUE_CAPACITY=64 LOG_SHIP_RETRY_MIN=20ms LOG_SHIP_RETRY_MAX=100ms LOG_SHIP_SHUTDOWN_TIMEOUT=2s)
COMMON=(APP_ENV=test MYSQL_HOST=127.0.0.1 MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE=gopulse_logs MYSQL_USER=gopulse MYSQL_PASSWORD="$MYSQL_PASSWORD" REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0 RABBITMQ_URL="amqp://gopulse:$MYSQL_PASSWORD@127.0.0.1:$RABBIT_PORT/" ELASTICSEARCH_URL="http://127.0.0.1:$ES_PORT" AUTH_JWT_SECRET="$JWT_SECRET" AUTH_COOKIE_NAME=gopulse_logs_session AUTH_COOKIE_SECURE=false MONITOR_URL="http://127.0.0.1:$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN")
env "${COMMON[@]}" "$TEMP_DIR/bin/migrate" up >/dev/null
start(){ local binary=$1 log=$2;shift 2;(exec env "$@" setsid "$binary") >"$log" 2>&1 & local pid=$!;PIDS+=("$pid:$binary");sleep .3;kill -0 "$pid" 2>/dev/null||{ cat "$log" >&2;fail "$binary failed to start";}; }
wait_url(){ local url=$1 token=${2:-};for _ in {1..120};do if [[ -n $token ]];then curl -fsS --max-time 1 -H "Authorization: Bearer $token" "$url" >/dev/null 2>&1&&return 0;else curl -fsS --max-time 1 "$url" >/dev/null 2>&1&&return 0;fi;sleep .2;done;return 1; }
start "$TEMP_DIR/bin/router" "$TEMP_DIR/router.log" ROUTER_HTTP_HOST=127.0.0.1 ROUTER_HTTP_PORT="$ROUTER_PORT" ROUTER_API_TOKEN="$ROUTER_TOKEN" ROUTER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT"
wait_url "http://127.0.0.1:$ROUTER_PORT/ready" "$ROUTER_TOKEN"||{ cat "$TEMP_DIR/router.log";fail 'Router not ready'; }
start "$TEMP_DIR/bin/marshaller" "$TEMP_DIR/marshaller.log" MARSHALLER_HTTP_HOST=127.0.0.1 MARSHALLER_HTTP_PORT="$MARSHALLER_PORT" MARSHALLER_API_TOKEN="$MARSHALLER_TOKEN" MARSHALLER_KAFKA_BROKERS="127.0.0.1:$KAFKA_PORT" MARSHALLER_VM_URL="http://127.0.0.1:$VM_PORT" MARSHALLER_VM_USERNAME=gopulse-marshaller MARSHALLER_VM_PASSWORD="$VM_PASSWORD" MARSHALLER_ELASTICSEARCH_URL="http://127.0.0.1:$ES_PORT"
wait_url "http://127.0.0.1:$MARSHALLER_PORT/ready" "$MARSHALLER_TOKEN"||{ cat "$TEMP_DIR/marshaller.log";fail 'Marshaller not ready'; }
start "$TEMP_DIR/bin/monitor" "$TEMP_DIR/monitor.log" MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$MONITOR_TOKEN" LOG_MONITOR_INGEST_TOKEN="$INGEST_TOKEN" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" MONITOR_ROUTER_URL="http://127.0.0.1:$ROUTER_PORT" MONITOR_ROUTER_TOKEN="$ROUTER_TOKEN" REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$REDIS_PASSWORD" REDIS_DB=0
wait_url "http://127.0.0.1:$MONITOR_PORT/ready" "$MONITOR_TOKEN"||{ cat "$TEMP_DIR/monitor.log";fail 'Monitor not ready'; }
env "${COMMON[@]}" "${LOGGING[@]}" "$TEMP_DIR/bin/search-reindex" --if-missing >"$TEMP_DIR/search-reindex.log" 2>&1||{ cat "$TEMP_DIR/search-reindex.log" >&2;fail 'search-reindex failed'; }
start "$TEMP_DIR/bin/backend" "$TEMP_DIR/backend.log" "${COMMON[@]}" "${LOGGING[@]}" HTTP_HOST=127.0.0.1 HTTP_PORT="$BACKEND_PORT"
start "$TEMP_DIR/bin/business-worker" "$TEMP_DIR/business-worker.log" "${COMMON[@]}" "${LOGGING[@]}" BUSINESS_WORKER_RECONNECT_MIN=100ms BUSINESS_WORKER_RECONNECT_MAX=200ms
start "$TEMP_DIR/bin/search-indexer" "$TEMP_DIR/search-indexer.log" "${COMMON[@]}" "${LOGGING[@]}" SEARCH_INDEXER_RECONNECT_MIN=100ms SEARCH_INDEXER_RECONNECT_MAX=200ms
wait_url "http://127.0.0.1:$BACKEND_PORT/health"||{ cat "$TEMP_DIR/backend.log";fail 'Backend not healthy'; }


mysql_query(){ docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" exec -T mysql mysql --user=gopulse --password="$MYSQL_PASSWORD" --batch --skip-column-names gopulse_logs --execute "$1" 2>/dev/null; }
json_id(){ python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["data"]["id"])' "$1"; }
wait_admin_log(){
  local service=$1 event_id=$2 message=$3 status output
  output="$TEMP_DIR/query-$service.json"
  for _ in {1..120};do
    status=$(curl -sS -b "$ADMIN_COOKIE" -o "$output" -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/logs?service=$service&event_id=$event_id&limit=100")
    if [[ $status == 200 ]]&&python3 - "$output" "$service" "$event_id" "$message" <<'PYLOG' 2>/dev/null;then return 0;fi
import json,sys
rows=json.load(open(sys.argv[1])).get('data',[])
assert any(row.get('service')==sys.argv[2] and row.get('event_id')==sys.argv[3] and row.get('message')==sys.argv[4] for row in rows)
assert all(value is not None for row in rows for value in row.values())
PYLOG
    sleep .25
  done
  cat "$output" >&2 || true
  return 1
}
wait_document(){
  local id=$1 expected=${2:-1} count=0
  for _ in {1..120};do
    count=$(curl -sS "http://127.0.0.1:$ES_PORT/gopulse-logs-v1-read/_search" -H 'Content-Type: application/json' -d "{\"query\":{\"ids\":{\"values\":[\"$id\"]}}}"|python3 -c 'import json,sys;print(json.load(sys.stdin).get("hits",{}).get("total",{}).get("value",0))' 2>/dev/null||echo 0)
    [[ $count == "$expected" ]]&&return 0
    sleep .25
  done
  return 1
}
ADMIN_HEADERS="$TEMP_DIR/admin.headers"; ADMIN_COOKIE="$TEMP_DIR/admin.cookie"; USER_COOKIE="$TEMP_DIR/user.cookie"
curl -sS -D "$ADMIN_HEADERS" -c "$ADMIN_COOKIE" -H 'Content-Type: application/json' -d '{"username":"logs_admin","password":"logs-password-123"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >"$TEMP_DIR/admin.json"
REQUEST_ID=$(awk 'BEGIN{IGNORECASE=1}/^X-Request-ID:/{gsub("\r","");print $2}' "$ADMIN_HEADERS"|tail -1)
[[ $REQUEST_ID =~ ^[0-9a-f]{32}$ ]]||fail 'Registration did not return a valid X-Request-ID.'
env "${COMMON[@]}" "$TEMP_DIR/bin/admin-role" promote --username logs_admin >/dev/null
curl -sS -c "$USER_COOKIE" -H 'Content-Type: application/json' -d '{"username":"logs_user","password":"logs-password-123"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
[[ $(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/logs") == 401 ]]||fail 'Unauthenticated log query was not rejected with 401.'
[[ $(curl -sS -b "$USER_COOKIE" -o /dev/null -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/logs") == 403 ]]||fail 'Ordinary-user log query was not rejected with 403.'
FOUND=0
for _ in {1..80};do
  status=$(curl -sS -b "$ADMIN_COOKIE" -o "$TEMP_DIR/query.json" -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/logs?request_id=$REQUEST_ID&limit=50")
  if [[ $status == 200 ]]&&python3 - "$TEMP_DIR/query.json" "$REQUEST_ID" 2>/dev/null <<'PY';then FOUND=1;break;fi
import json,sys
value=json.load(open(sys.argv[1])); rows=value.get('data',[]); messages={row.get('message') for row in rows if row.get('request_id')==sys.argv[2]}
assert {'user registered','http request completed'} <= messages
assert all(row.get('service')=='backend' and '_id' not in row and '_index' not in row for row in rows)
PY
  sleep .25
done
((FOUND==1))||{ cat "$TEMP_DIR/query.json" >&2;fail 'Admin query did not observe the HTTP and business logs.'; }

# Real background sources share event IDs with Backend Outbox records.
curl -sS -b "$USER_COOKIE" -H 'Content-Type: application/json' -d '{"title":"logs background delivery","content":"bounded background logging"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/posts" >"$TEMP_DIR/post.json"
POST_ID=$(json_id "$TEMP_DIR/post.json")
for _ in {1..80};do POST_EVENT=$(mysql_query "SELECT event_id FROM business_outbox WHERE event_type='post.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.post_id'))='$POST_ID' ORDER BY id DESC LIMIT 1;"||true); [[ -n $POST_EVENT ]]&&break;sleep .25;done
[[ $POST_EVENT =~ ^[0-9a-f-]{36}$ ]]||fail 'Post event ID was not created.'
wait_admin_log backend "$POST_EVENT" 'outbox event published'||fail 'Backend Outbox post log was not queryable.'
wait_admin_log search-indexer "$POST_EVENT" 'event processed'||fail 'Search Indexer post log was not queryable.'

ACTOR_COOKIE="$TEMP_DIR/actor.cookie"
curl -sS -c "$ACTOR_COOKIE" -H 'Content-Type: application/json' -d '{"username":"logs_actor","password":"logs-password-123"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
curl -sS -b "$ACTOR_COOKIE" -H 'Content-Type: application/json' -d '{"content":"background notification"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/posts/$POST_ID/comments" >"$TEMP_DIR/comment.json"
COMMENT_ID=$(json_id "$TEMP_DIR/comment.json")
for _ in {1..80};do COMMENT_EVENT=$(mysql_query "SELECT event_id FROM business_outbox WHERE event_type='comment.created' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.comment_id'))='$COMMENT_ID' ORDER BY id DESC LIMIT 1;"||true); [[ -n $COMMENT_EVENT ]]&&break;sleep .25;done
[[ $COMMENT_EVENT =~ ^[0-9a-f-]{36}$ ]]||fail 'Comment event ID was not created.'
wait_admin_log backend "$COMMENT_EVENT" 'outbox event published'||fail 'Backend Outbox comment log was not queryable.'
wait_admin_log business-worker "$COMMENT_EVENT" 'event processed'||fail 'Business Worker comment log was not queryable.'

# The one-shot command ships lifecycle output without allowing drain status to change its business result.
REINDEX_FOUND=0
for _ in {1..80};do
  status=$(curl -sS -b "$ADMIN_COOKIE" -o "$TEMP_DIR/reindex-query.json" -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/logs?service=search-reindex&limit=100")
  if [[ $status == 200 ]]&&python3 - "$TEMP_DIR/reindex-query.json" <<'PYREINDEX' 2>/dev/null;then REINDEX_FOUND=1;break;fi
import json,sys
messages={row.get('message') for row in json.load(open(sys.argv[1])).get('data',[])}
assert 'search reindex started' in messages and messages.intersection({'search reindex completed','search reindex skipped'})
PYREINDEX
  sleep .25
done
((REINDEX_FOUND==1))||fail 'search-reindex lifecycle logs were not queryable.'

# A permanently invalid Kafka log is committed and does not block the following valid record.
BAD_ID=11111111111111111111111111111111; GOOD_ID=22222222222222222222222222222222; TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
BAD_ENVELOPE=$(printf '{"schema_version":1,"message_id":"%s","type":"logs","source":"backend","timestamp":"%s","payload":{"invalid":true}}' "$BAD_ID" "$TS")
[[ $(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ROUTER_TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $BAD_ID" --data-binary "$BAD_ENVELOPE" "http://127.0.0.1:$ROUTER_PORT/internal/v1/messages") == 202 ]]||fail 'Permanent Kafka fixture was not accepted by Router.'
GOOD_BODY=$(printf '{"log_schema_version":1,"timestamp":"%s","level":"info","service":"backend","module":"lifecycle","message":"backend listening"}' "$TS")
[[ $(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $INGEST_TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $GOOD_ID" --data-binary "$GOOD_BODY" "http://127.0.0.1:$MONITOR_PORT/internal/v1/logs") == 202 ]]||fail 'Legal continuation fixture was not accepted.'
wait_document "$GOOD_ID"||fail 'Permanent invalid Kafka record blocked its legal successor.'

# Elasticsearch outage retains the accepted Kafka record until storage recovers.
RECOVERY_ID=33333333333333333333333333333333; TS=$(date -u +%Y-%m-%dT%H:%M:%SZ); RECOVERY_BODY=$(printf '{"log_schema_version":1,"timestamp":"%s","level":"info","service":"search-reindex","module":"search","message":"search reindex skipped","result":"unchanged","batch_size":500}' "$TS")
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" stop elasticsearch >/dev/null
[[ $(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $INGEST_TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $RECOVERY_ID" --data-binary "$RECOVERY_BODY" "http://127.0.0.1:$MONITOR_PORT/internal/v1/logs") == 202 ]]||fail 'Log was not accepted while Elasticsearch was unavailable.'
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" start elasticsearch >/dev/null
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" up -d --wait elasticsearch >/dev/null
wait_document "$RECOVERY_ID"||fail 'Accepted log did not recover after Elasticsearch restart.'

# Controlled same-ID replay proves Elasticsearch idempotency without expanding the public DTO.
REPLAY_ID=abcdef0123456789abcdef0123456789; TS=$(date -u +%Y-%m-%dT%H:%M:%SZ); BODY=$(printf '{"log_schema_version":1,"timestamp":"%s","level":"info","service":"backend","module":"lifecycle","message":"backend listening"}' "$TS")
for _ in 1 2;do [[ $(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $INGEST_TOKEN" -H 'Content-Type: application/json' -H "Idempotency-Key: $REPLAY_ID" --data-binary "$BODY" "http://127.0.0.1:$MONITOR_PORT/internal/v1/logs") == 202 ]]||fail 'Replay fixture was not accepted.';done
for _ in {1..40};do count=$(curl -sS "http://127.0.0.1:$ES_PORT/gopulse-logs-v1-read/_search" -H 'Content-Type: application/json' -d "{\"query\":{\"ids\":{\"values\":[\"$REPLAY_ID\"]}}}"|python3 -c 'import json,sys;print(json.load(sys.stdin).get("hits",{}).get("total",{}).get("value",0))' 2>/dev/null||echo 0); [[ $count == 1 ]]&&break;sleep .25;done
[[ ${count:-0} == 1 ]]||fail 'Same message ID produced more than one stored document or none.'
curl -fsS "http://127.0.0.1:$ES_PORT/_index_template/gopulse-logs-v1-template" >/dev/null
info "End-to-end Backend log query passed for request $REQUEST_ID."
