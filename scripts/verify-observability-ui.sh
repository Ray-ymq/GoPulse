#!/usr/bin/env bash
set -Eeuo pipefail
REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
fail(){ printf '[verify-observability-ui] ERROR: %s\n' "$*" >&2; return 1; }
self_test(){
  grep -q 'requiresAdmin' "$REPO_ROOT/frontend/src/router/index.ts" || fail 'admin route guard is missing.'
  grep -q 'gopulse_redis_up' "$REPO_ROOT/backend/internal/metricquery/metricquery.go" || fail 'fixed metric catalog is missing.'
  grep -q 'ordinary user is isolated' "$REPO_ROOT/frontend/e2e/observability.spec.ts" || fail 'ordinary-user browser acceptance is missing.'
  grep -q 'real exporter management loop' "$REPO_ROOT/frontend/e2e/observability.spec.ts" || fail 'real Exporter browser operations are missing.'
  grep -q 'GOPULSE_OBSERVABILITY_UPDATE_PACKAGE' "$REPO_ROOT/scripts/verify-observability-ui.sh" || fail 'browser update package wiring is missing.'
  bash -n "$REPO_ROOT/scripts/verify-observability-ui.sh"
  printf '[verify-observability-ui] Self-test passed.\n'
}
if [[ ${1:-} == --self-test ]]; then self_test; exit 0; elif (($#)); then fail "unknown argument: $1"; exit 2; fi
for tool in curl docker go node npm python3; do command -v "$tool" >/dev/null || { fail "$tool is required"; exit 1; }; done
docker compose version >/dev/null
TMP=$(mktemp -d)
TOKEN=$(python3 -c 'import secrets; print(secrets.token_hex(6))')
PROJECT="gopulse-observability-$TOKEN"
ENV_FILE="$TMP/acceptance.env"
RUN_DIR="$TMP/run"
DEV_PID=
cleanup(){
  local code=$?
  trap - EXIT INT TERM
  if [[ -n ${DEV_PID:-} ]] && kill -0 "$DEV_PID" 2>/dev/null; then kill -TERM -- "-$DEV_PID" 2>/dev/null || true; wait "$DEV_PID" 2>/dev/null || true; fi
  docker compose --project-name "$PROJECT" --env-file "$ENV_FILE" --file "$REPO_ROOT/deploy/compose.yaml" down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "$TMP"
  exit "$code"
}
trap cleanup EXIT INT TERM
read -r MYSQL_PORT REDIS_PORT EXPORTER_PORT RABBIT_PORT RABBIT_MGMT_PORT KAFKA_PORT VM_PORT ROUTER_PORT MARSHALLER_PORT ES_PORT MONITOR_PORT BACKEND_PORT FRONTEND_PORT <<<"$(python3 - <<'PYPORTS'
import socket
sockets=[]
try:
 for _ in range(13):
  s=socket.socket(); s.bind(('127.0.0.1',0)); sockets.append(s)
 print(' '.join(str(s.getsockname()[1]) for s in sockets))
finally:
 for s in sockets: s.close()
PYPORTS
)"
python3 - "$REPO_ROOT/.env.example" "$ENV_FILE" "$TOKEN" "$MYSQL_PORT" "$REDIS_PORT" "$EXPORTER_PORT" "$RABBIT_PORT" "$RABBIT_MGMT_PORT" "$KAFKA_PORT" "$VM_PORT" "$ROUTER_PORT" "$MARSHALLER_PORT" "$ES_PORT" "$MONITOR_PORT" "$BACKEND_PORT" "$FRONTEND_PORT" <<'PYENV'
import sys
source,target,token,*ports=sys.argv[1:]
mysql,redis,exporter,rabbit,rabbit_mgmt,kafka,vm,router,marshaller,es,monitor,backend,frontend=ports
values={
 'HTTP_PORT':backend,'FRONTEND_PORT':frontend,'MYSQL_PORT':mysql,'REDIS_PORT':redis,'REDIS_EXPORTER_HTTP_PORT':exporter,
 'RABBITMQ_PORT':rabbit,'RABBITMQ_MANAGEMENT_PORT':rabbit_mgmt,'KAFKA_PORT':kafka,'VICTORIAMETRICS_PORT':vm,
 'ROUTER_HTTP_PORT':router,'MARSHALLER_HTTP_PORT':marshaller,'ELASTICSEARCH_PORT':es,'MONITOR_HTTP_PORT':monitor,
 'ROUTER_KAFKA_BROKERS':f'127.0.0.1:{kafka}','MARSHALLER_KAFKA_BROKERS':f'127.0.0.1:{kafka}',
 'MYSQL_DATABASE':'gopulse_obs_'+token,'MYSQL_USER':'obs_'+token,'MYSQL_PASSWORD':'mysql-'+token,'MYSQL_ROOT_PASSWORD':'root-'+token,
 'REDIS_PASSWORD':'redis-'+token,'RABBITMQ_USER':'rabbit_'+token,'RABBITMQ_PASSWORD':'rabbit-'+token,
 'RABBITMQ_URL':f'amqp://rabbit_{token}:rabbit-{token}@127.0.0.1:{rabbit}/',
 'ELASTICSEARCH_URL':f'http://127.0.0.1:{es}','MARSHALLER_ELASTICSEARCH_URL':f'http://127.0.0.1:{es}',
 'MARSHALLER_VM_URL':f'http://127.0.0.1:{vm}','BACKEND_VICTORIAMETRICS_URL':f'http://127.0.0.1:{vm}',
 'MONITOR_URL':f'http://127.0.0.1:{monitor}','LOG_MONITOR_URL':f'http://127.0.0.1:{monitor}',
 'MONITOR_ROUTER_URL':f'http://127.0.0.1:{router}',
 'AUTH_JWT_SECRET':'jwt-'+token+'-0123456789abcdef0123456789','MONITOR_API_TOKEN':'monitor-'+token+'-0123456789abcdef0123',
 'LOG_MONITOR_INGEST_TOKEN':'ingest-'+token+'-0123456789abcdef012345','ROUTER_API_TOKEN':'router-'+token+'-0123456789abcdef012345',
 'MONITOR_ROUTER_TOKEN':'router-'+token+'-0123456789abcdef012345','MARSHALLER_API_TOKEN':'marshaller-'+token+'-0123456789abcdef',
 'VICTORIAMETRICS_PASSWORD':'vm-'+token+'-0123456789abcdef','MARSHALLER_VM_PASSWORD':'vm-'+token+'-0123456789abcdef',
 'BACKEND_VICTORIAMETRICS_PASSWORD':'vm-'+token+'-0123456789abcdef','MONITOR_REQUEST_TIMEOUT':'3s',
}
seen=set(); output=[]
for raw in open(source,encoding='utf-8'):
 line=raw.rstrip('\n')
 if '=' in line and not line.lstrip().startswith('#'):
  key=line.split('=',1)[0].strip()
  if key in values: line=f'{key}={values[key]}'; seen.add(key)
 output.append(line)
for key,value in values.items():
 if key not in seen: output.append(f'{key}={value}')
open(target,'w',encoding='utf-8').write('\n'.join(output)+'\n')
PYENV
UPDATE_PACKAGE="$TMP/redis-exporter-1.8.3.tar.gz"
"$REPO_ROOT/scripts/package-redis-exporter.sh" --version 1.8.3 --output "$UPDATE_PACKAGE" >/dev/null
setsid env GOPULSE_PROJECT_NAME="$PROJECT" GOPULSE_ENV_FILE="$ENV_FILE" GOPULSE_RUN_DIR="$RUN_DIR" "$REPO_ROOT/scripts/dev.sh" >"$TMP/dev.log" 2>&1 & DEV_PID=$!
for _ in {1..180}; do
  if curl -fsS --max-time 1 "http://127.0.0.1:$BACKEND_PORT/health" >/dev/null 2>&1 && curl -fsS --max-time 1 "http://127.0.0.1:$FRONTEND_PORT/" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$DEV_PID" 2>/dev/null; then cat "$TMP/dev.log" >&2; fail 'isolated lifecycle exited before readiness'; exit 1; fi
  sleep 1
done
curl -fsS --max-time 2 "http://127.0.0.1:$BACKEND_PORT/health" >/dev/null || { cat "$TMP/dev.log" >&2; fail 'Backend did not become ready'; exit 1; }
curl -fsS --max-time 2 "http://127.0.0.1:$FRONTEND_PORT/" >/dev/null || { cat "$TMP/dev.log" >&2; fail 'Frontend did not become ready'; exit 1; }
ADMIN="obs_admin_$TOKEN"; USER="obs_user_$TOKEN"; PASSWORD="Obs-${TOKEN}-password"
register(){ curl -fsS -c "$2" -H 'Content-Type: application/json' --data "{\"username\":\"$1\",\"password\":\"$PASSWORD\"}" "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null; }
register "$ADMIN" "$TMP/admin.cookie"; register "$USER" "$TMP/user.cookie"
set -a
source "$ENV_FILE"
set +a
(cd "$REPO_ROOT/backend" && go run ./cmd/admin-role promote --username "$ADMIN") >/dev/null
for _ in {1..60}; do
  if curl -fsS -b "$TMP/admin.cookie" "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/metrics?metric=gopulse_redis_up&range=15m" | python3 -c 'import json,sys; raise SystemExit(0 if json.load(sys.stdin).get("data",{}).get("series") else 1)' 2>/dev/null; then break; fi
  sleep 1
done
curl -fsS -b "$TMP/admin.cookie" "http://127.0.0.1:$BACKEND_PORT/api/v1/observability/metrics?metric=gopulse_redis_up&range=15m" | python3 -c 'import json,sys; assert json.load(sys.stdin).get("data",{}).get("series")' || { cat "$TMP/dev.log" >&2; fail 'fresh metric sample did not reach the Backend query'; exit 1; }
# Generate a unique current Backend log through the real HTTP/log ship path.
curl -sS -o /dev/null -H "X-Request-ID: ${TOKEN}${TOKEN}00000000" "http://127.0.0.1:$BACKEND_PORT/api/v1/does-not-exist" || true
sleep 2
(cd "$REPO_ROOT/frontend" && GOPULSE_BASE_URL="http://127.0.0.1:$FRONTEND_PORT" GOPULSE_OBSERVABILITY_ADMIN_USERNAME="$ADMIN" GOPULSE_OBSERVABILITY_USER_USERNAME="$USER" GOPULSE_OBSERVABILITY_PASSWORD="$PASSWORD" GOPULSE_OBSERVABILITY_UPDATE_PACKAGE="$UPDATE_PACKAGE" GOPULSE_OBSERVABILITY_PROJECT="$PROJECT" GOPULSE_OBSERVABILITY_ENV_FILE="$ENV_FILE" GOPULSE_OBSERVABILITY_COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml" GOPULSE_OBSERVABILITY_RUN_DIR="$RUN_DIR" npm run test:e2e -- observability.spec.ts)
printf '[verify-observability-ui] Isolated real-browser observability acceptance passed.\n'
