#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TOKEN='verify-monitor-token-at-least-32-bytes-long'
TEMP_DIR= MONITOR_PID= BACKEND_PID= FOREIGN_PID= PROJECT= EXPORTER_PORT= MONITOR_PORT= BACKEND_PORT= MYSQL_PORT=
fail(){ printf '[verify-monitor] ERROR: %s\n' "$*" >&2; return 1; }
free_port(){ python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
}
cleanup(){
  local status=$?; trap - EXIT INT TERM
  if [[ -n ${FOREIGN_PID:-} ]] && kill -0 "$FOREIGN_PID" 2>/dev/null; then kill "$FOREIGN_PID" 2>/dev/null || true; wait "$FOREIGN_PID" 2>/dev/null || true; fi
  if [[ -n ${BACKEND_PID:-} ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then kill -TERM "$BACKEND_PID" 2>/dev/null || true; wait "$BACKEND_PID" 2>/dev/null || true; fi
  if [[ -n ${MONITOR_PID:-} ]] && kill -0 "$MONITOR_PID" 2>/dev/null; then kill -TERM "$MONITOR_PID" 2>/dev/null || true; wait "$MONITOR_PID" 2>/dev/null || true; fi
  if [[ -n ${PROJECT:-} && $PROJECT =~ ^gopulse-monitor-[a-f0-9]{12}$ ]]; then docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" down -v --remove-orphans >/dev/null 2>&1 || true; fi
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
  exit "$status"
}
api(){ local method=$1 path=$2; shift 2; curl --silent --show-error --max-time 30 -X "$method" -H "Authorization: Bearer $TOKEN" "$@" "http://127.0.0.1:$MONITOR_PORT$path"; }
admin_api(){ local method=$1 path=$2; shift 2; curl --silent --show-error --max-time 30 -X "$method" -b "$TEMP_DIR/admin.cookie" "$@" "http://127.0.0.1:$BACKEND_PORT$path"; }
wait_ready(){ for _ in {1..100}; do curl -fsS --max-time 1 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$MONITOR_PORT/ready" >/dev/null 2>&1 && return 0; sleep .1; done; return 1; }
start_monitor(){
  env MONITOR_HTTP_HOST=127.0.0.1 MONITOR_HTTP_PORT="$MONITOR_PORT" MONITOR_API_TOKEN="$TOKEN" MONITOR_PLUGIN_ROOT="$TEMP_DIR/plugins" MONITOR_PLUGIN_STARTUP_TIMEOUT=10s MONITOR_PLUGIN_STOP_TIMEOUT=4s \
    REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$PASSWORD" REDIS_DB=0 REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$EXPORTER_PORT" REDIS_EXPORTER_SCRAPE_TIMEOUT=1s REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s \
    "$TEMP_DIR/monitor" >"$TEMP_DIR/monitor.log" 2>&1 &
  MONITOR_PID=$!
  wait_ready || { cat "$TEMP_DIR/monitor.log" >&2; fail 'Monitor did not become ready.'; }
}
backend_env(){
  env APP_ENV=test HTTP_HOST=127.0.0.1 HTTP_PORT="$BACKEND_PORT" MYSQL_HOST=127.0.0.1 MYSQL_PORT="$MYSQL_PORT" MYSQL_DATABASE=gopulse_monitor MYSQL_USER=gopulse MYSQL_PASSWORD="$MYSQL_PASSWORD"     REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_PASSWORD="$PASSWORD" REDIS_DB=0 RABBITMQ_URL=amqp://guest:guest@127.0.0.1:1/ ELASTICSEARCH_URL=http://127.0.0.1:1     AUTH_JWT_SECRET=verify-monitor-jwt-secret-at-least-32-bytes AUTH_COOKIE_NAME=verify_monitor_session AUTH_COOKIE_SECURE=false MONITOR_URL="http://127.0.0.1:$MONITOR_PORT" MONITOR_API_TOKEN="$TOKEN" "$@"
}
start_backend(){
  backend_env "$TEMP_DIR/backend" >"$TEMP_DIR/backend.log" 2>&1 & BACKEND_PID=$!
  for _ in {1..100}; do curl -fsS --max-time 1 "http://127.0.0.1:$BACKEND_PORT/health" >/dev/null 2>&1 && return 0; sleep .1; done
  cat "$TEMP_DIR/backend.log" >&2; fail 'Backend did not become healthy.'
}
make_failing_package(){
  local version=$1 output=$2 digest arch
  local stage="$TEMP_DIR/failing-$version"
  mkdir -p "$stage/bin"; cp /bin/false "$stage/bin/gopulse-redis-exporter"; chmod 0755 "$stage/bin/gopulse-redis-exporter"
  digest=$(sha256sum "$stage/bin/gopulse-redis-exporter" | awk '{print $1}'); arch=$(go env GOARCH)
  python3 - "$stage/plugin.json" "$version" "$arch" "$digest" <<'PY'
import json,sys
path,version,arch,digest=sys.argv[1:]
value={"schema_version":1,"id":"redis-exporter","name":"GoPulse Redis Exporter","version":version,"kind":"metrics-exporter","source":"redis","os":"linux","arch":arch,"entrypoint":"bin/gopulse-redis-exporter","entrypoint_sha256":digest,"health_path":"/health","metrics_path":"/metrics"}
with open(path,'w') as handle: json.dump(value,handle,separators=(',',':')); handle.write('\n')
PY
  tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=ustar -C "$stage" -cf "$output.tar" plugin.json bin/gopulse-redis-exporter
  gzip -n -c "$output.tar" > "$output"
}
self_test(){
  local d; d=$(mktemp -d); trap 'rm -rf -- "$d"' RETURN
  (cd "$REPO_ROOT/monitor" && go build -o "$d/monitor" ./cmd/monitor)
  if MONITOR_API_TOKEN=short MONITOR_PLUGIN_ROOT="$d/plugins" REDIS_HOST=127.0.0.1 REDIS_PORT=6379 REDIS_DB=0 "$d/monitor" >/dev/null 2>&1; then fail 'Monitor accepted a short token.'; fi
  if "$REPO_ROOT/scripts/package-redis-exporter.sh" --version invalid --output "$d/invalid.tar.gz" >/dev/null 2>&1; then fail 'Packager accepted invalid SemVer.'; fi
  printf '[verify-monitor] Self-test passed.\n'
}
if [[ ${1:-} == --self-test ]]; then self_test; exit 0; elif (($#)); then fail "Unknown argument: $1"; exit 2; fi
command -v docker >/dev/null && command -v curl >/dev/null && command -v python3 >/dev/null || { fail 'docker, curl, and python3 are required.'; exit 1; }
TEMP_DIR=$(mktemp -d); trap cleanup EXIT INT TERM
TOKEN_ID=$(python3 -c 'import secrets; print(secrets.token_hex(6))'); PROJECT="gopulse-monitor-$TOKEN_ID"; REDIS_PORT=$(free_port); EXPORTER_PORT=$(free_port); MONITOR_PORT=$(free_port); BACKEND_PORT=$(free_port); MYSQL_PORT=$(free_port); PASSWORD="monitor-$TOKEN_ID-secret"; MYSQL_PASSWORD="mysql-$TOKEN_ID-secret"
cat > "$TEMP_DIR/compose.yaml" <<YAML
services:
  mysql:
    image: mysql:8.4.0
    environment:
      MYSQL_DATABASE: gopulse_monitor
      MYSQL_USER: gopulse
      MYSQL_PASSWORD: "$MYSQL_PASSWORD"
      MYSQL_ROOT_PASSWORD: "$MYSQL_PASSWORD"
    ports: ["127.0.0.1:$MYSQL_PORT:3306"]
    healthcheck:
      test: ["CMD-SHELL","mysqladmin ping -h 127.0.0.1 -uroot -p$MYSQL_PASSWORD --silent"]
      interval: 2s
      timeout: 2s
      retries: 30
  redis:
    image: redis:7.2.5-alpine
    command: ["redis-server","--requirepass","$PASSWORD"]
    ports: ["127.0.0.1:$REDIS_PORT:6379"]
YAML
docker compose --project-name "$PROJECT" --file "$TEMP_DIR/compose.yaml" up -d --wait >/dev/null
(cd "$REPO_ROOT/monitor" && go build -o "$TEMP_DIR/monitor" ./cmd/monitor)
(cd "$REPO_ROOT/backend" && go build -o "$TEMP_DIR/backend" ./cmd/server)
(cd "$REPO_ROOT/backend" && backend_env go run ./cmd/migrate up >/dev/null)
PACKAGE1=$($REPO_ROOT/scripts/package-redis-exporter.sh --version 1.3.2 --output "$TEMP_DIR/plugin-1.3.2.tar.gz")
start_monitor
start_backend
curl -fsS -c "$TEMP_DIR/user.cookie" -H 'Content-Type: application/json' -d '{"username":"MonitorUser","password":"monitor-password"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
curl -fsS -c "$TEMP_DIR/admin.cookie" -H 'Content-Type: application/json' -d '{"username":"MonitorAdmin","password":"monitor-password"}' "http://127.0.0.1:$BACKEND_PORT/api/v1/auth/register" >/dev/null
(cd "$REPO_ROOT/backend" && backend_env go run ./cmd/admin-role promote --username MonitorAdmin >/dev/null)
code=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins"); [[ $code == 401 ]] || fail "public unauthenticated status was $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$TEMP_DIR/user.cookie" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins"); [[ $code == 403 ]] || fail "ordinary-user status was $code"
admin_api GET /api/v1/exporter-plugins > "$TEMP_DIR/empty-list.json"; python3 -c 'import json,sys; assert json.load(open(sys.argv[1]))["data"]==[]' "$TEMP_DIR/empty-list.json"
FAIL_INSTALL="$TEMP_DIR/plugin-failing-1.3.1.tar.gz"; make_failing_package 1.3.1 "$FAIL_INSTALL"
code=$(curl -sS -o "$TEMP_DIR/failed-install.json" -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" -F "package=@$FAIL_INSTALL" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/install"); [[ $code == 422 ]] || fail "failing install status was $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter"); [[ $code == 404 ]] || fail 'failed install left a Registry entry'
[[ ! -e "$TEMP_DIR/plugins/registry.json" && ! -e "$TEMP_DIR/plugins/redis-exporter/current" ]] || fail 'failed install left committed state'
code=$(curl -sS -o "$TEMP_DIR/unauthorized" -w '%{http_code}' "http://127.0.0.1:$MONITOR_PORT/internal/v1/exporter-plugins"); [[ $code == 401 ]] || fail "unauthorized status was $code"
code=$(curl -sS -o "$TEMP_DIR/install.json" -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" -F "package=@$PACKAGE1" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/install"); [[ $code == 201 ]] || { cat "$TEMP_DIR/install.json" >&2; fail "install status was $code"; }
python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["data"]; assert v["id"]=="redis-exporter" and v["desired_state"]==v["observed_state"]=="running"' "$TEMP_DIR/install.json"
curl -fsS "http://127.0.0.1:$EXPORTER_PORT/health" >/dev/null
admin_api POST /api/v1/exporter-plugins/redis-exporter/stop > "$TEMP_DIR/stop.json"; python3 -c 'import json,sys; assert json.load(open(sys.argv[1]))["data"]["observed_state"]=="stopped"' "$TEMP_DIR/stop.json"
admin_api POST /api/v1/exporter-plugins/redis-exporter/start > "$TEMP_DIR/start.json"; curl -fsS "http://127.0.0.1:$EXPORTER_PORT/health" >/dev/null
PACKAGE2=$($REPO_ROOT/scripts/package-redis-exporter.sh --version 1.3.3 --output "$TEMP_DIR/plugin-1.3.3.tar.gz")
code=$(curl -sS -o "$TEMP_DIR/update.json" -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" -F "package=@$PACKAGE2" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/update"); [[ $code == 200 ]] || fail "update status was $code"
python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["data"]; assert v["version"]=="1.3.3" and v["observed_state"]=="running"' "$TEMP_DIR/update.json"
FAIL_UPDATE="$TEMP_DIR/plugin-failing-1.3.4.tar.gz"; make_failing_package 1.3.4 "$FAIL_UPDATE"
code=$(curl -sS -o "$TEMP_DIR/rollback.json" -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" -F "package=@$FAIL_UPDATE" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/update"); [[ $code == 422 ]] || fail "failing update status was $code"
admin_api GET /api/v1/exporter-plugins/redis-exporter > "$TEMP_DIR/rollback-state.json"; python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["data"]; assert v["version"]=="1.3.3" and v["observed_state"]=="running"' "$TEMP_DIR/rollback-state.json"
admin_api POST /api/v1/exporter-plugins/redis-exporter/stop >/dev/null
sleep 30 & FOREIGN_PID=$!; mkdir -p "$TEMP_DIR/plugins/redis-exporter/runtime"
python3 - "$TEMP_DIR/plugins/redis-exporter/runtime/process.json" "$FOREIGN_PID" <<'PY'
import json,sys
json.dump({"pid":int(sys.argv[2]),"start_ticks":"invalid","executable_path":"/bin/false","working_directory":"/","command_line_marker":"forged"},open(sys.argv[1],'w'))
PY
code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST -b "$TEMP_DIR/admin.cookie" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/stop"); [[ $code == 422 && -e /proc/$FOREIGN_PID ]] || fail 'forged PID record was not rejected safely'
kill "$FOREIGN_PID"; wait "$FOREIGN_PID" 2>/dev/null || true; FOREIGN_PID=; unlink "$TEMP_DIR/plugins/redis-exporter/runtime/process.json"
PACKAGE3=$($REPO_ROOT/scripts/package-redis-exporter.sh --version 1.3.5 --output "$TEMP_DIR/plugin-1.3.5.tar.gz")
code=$(curl -sS -o "$TEMP_DIR/stopped-update.json" -w '%{http_code}' -b "$TEMP_DIR/admin.cookie" -F "package=@$PACKAGE3" "http://127.0.0.1:$BACKEND_PORT/api/v1/exporter-plugins/redis-exporter/update"); [[ $code == 200 ]] || fail "stopped update status was $code"
python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["data"]; assert v["version"]=="1.3.5" and v["observed_state"]=="stopped"' "$TEMP_DIR/stopped-update.json"
admin_api POST /api/v1/exporter-plugins/redis-exporter/start >/dev/null
kill -TERM "$MONITOR_PID"; wait "$MONITOR_PID"; MONITOR_PID=; start_monitor
admin_api GET /api/v1/exporter-plugins/redis-exporter > "$TEMP_DIR/recovered.json"; python3 -c 'import json,sys; assert json.load(open(sys.argv[1]))["data"]["observed_state"]=="running"' "$TEMP_DIR/recovered.json"
printf '[verify-monitor] Lifecycle acceptance passed.\n'
