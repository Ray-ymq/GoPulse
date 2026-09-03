#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
EXPORTER_DIR="$REPO_ROOT/exporters/redis"
TEMP_DIR=
TOKEN=
PROJECT_NAME=
COMPOSE_FILE=
REDIS_PORT=
EXPORTER_PORT=
AUTH_EXPORTER_PORT=
TIMEOUT_EXPORTER_PORT=
PASSWORD=
CONTAINER_ID=
EXPORTER_PID=
AUTH_EXPORTER_PID=
TIMEOUT_EXPORTER_PID=
TIMEOUT_SERVER_PID=
DAILY_BEFORE=
CLEANED=0

info() { printf '[gopulse-exporter] %s\n' "$*"; }
fail() { printf '[gopulse-exporter] ERROR: %s\n' "$*" >&2; return 1; }

random_port() {
  python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
}

valid_project() { [[ $1 =~ ^gopulse-exporter-[a-f0-9]{12}$ ]]; }
process_start_ticks() {
  python3 - "$1" <<'PY'
import sys
value=open(f'/proc/{sys.argv[1]}/stat', encoding='utf-8').read().strip()
print(value[value.rfind(')')+2:].split()[19])
PY
}

write_record() {
  local pid=$1 record=$2 cwd=$3 executable=$4 marker=$5 start actual
  start=$(process_start_ticks "$pid")
  actual=$(readlink -f "/proc/$pid/exe")
  python3 - "$record" "$pid" "$start" "$actual" "$cwd" "$marker" <<'PY'
import json, os, sys
path, pid, start, executable, cwd, marker=sys.argv[1:]
with open(path, 'w', encoding='utf-8') as handle:
    json.dump({'pid':int(pid),'startTicks':start,'executablePath':os.path.realpath(executable),'workingDirectory':os.path.realpath(cwd),'commandLineMarker':marker}, handle, separators=(',',':'))
PY
}

validate_record() {
  local record=$1 expected_cwd=$2 expected_executable=$3 expected_marker=$4
  python3 - "$record" "$expected_cwd" "$expected_executable" "$expected_marker" <<'PY'
import json, os, sys
path, cwd, executable, marker=sys.argv[1:]
try:
    value=json.load(open(path, encoding='utf-8'))
    pid=int(value['pid'])
    expected_start=str(value['startTicks'])
    recorded_executable=os.path.realpath(value['executablePath'])
    recorded_cwd=os.path.realpath(value['workingDirectory'])
    recorded_marker=str(value['commandLineMarker'])
    stat=open(f'/proc/{pid}/stat', encoding='utf-8').read().strip()
    actual_start=stat[stat.rfind(')')+2:].split()[19]
    actual_executable=os.path.realpath(f'/proc/{pid}/exe')
    actual_cwd=os.path.realpath(f'/proc/{pid}/cwd')
    command=open(f'/proc/{pid}/cmdline','rb').read().replace(b'\0',b' ').decode(errors='replace')
except Exception:
    raise SystemExit(1)
valid=(recorded_cwd == os.path.realpath(cwd) == actual_cwd and
       recorded_executable == os.path.realpath(executable) == actual_executable and
       recorded_marker == marker and marker in command and expected_start == actual_start)
if not valid: raise SystemExit(1)
print(pid)
PY
}

start_exporter() {
  local port=$1 password=$2 redis_port=$3 record=$4 log=$5 pid_name=$6 timeout=${7:-500ms}
  local binary="$TEMP_DIR/gopulse-redis-exporter"
  env REDIS_HOST=127.0.0.1 REDIS_PORT="$redis_port" REDIS_PASSWORD="$password" REDIS_DB=5 \
    REDIS_EXPORTER_HTTP_HOST=127.0.0.1 REDIS_EXPORTER_HTTP_PORT="$port" \
    REDIS_EXPORTER_SCRAPE_TIMEOUT="$timeout" REDIS_EXPORTER_SHUTDOWN_TIMEOUT=3s \
    python3 - "$EXPORTER_DIR" "$binary" "$log" <<'PY' &
import os, sys
cwd, executable, log=sys.argv[1:]
os.chdir(cwd)
os.setsid()
stream=os.open(log, os.O_WRONLY|os.O_CREAT|os.O_TRUNC, 0o600)
os.dup2(stream, 1); os.dup2(stream, 2)
os.execve(executable, [executable], os.environ)
PY
  local pid=$!
  sleep 0.25
  kill -0 "$pid" 2>/dev/null || { fail "Exporter on port $port exited during startup."; return 1; }
  write_record "$pid" "$record" "$EXPORTER_DIR" "$binary" "$binary"
  printf -v "$pid_name" '%s' "$pid"
}

stop_exporter() {
  local pid=$1 record=$2
  [[ -n $pid && -f $record ]] || return 0
  validate_record "$record" "$EXPORTER_DIR" "$TEMP_DIR/gopulse-redis-exporter" "$TEMP_DIR/gopulse-redis-exporter" >/dev/null || { fail 'Refusing to stop an exporter whose process identity does not match.'; return 1; }
  kill -TERM -- "-$pid" 2>/dev/null || true
  for _ in {1..40}; do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  if kill -0 "$pid" 2>/dev/null; then fail "Exporter PID $pid did not stop within the shutdown boundary."; return 1; fi
  rm -f -- "$record"
}

container_owned() {
  valid_project "$PROJECT_NAME" || return 1
  [[ -n $CONTAINER_ID ]] || return 1
  local labels binding
  labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}' "$CONTAINER_ID" 2>/dev/null) || return 1
  [[ $labels == "$PROJECT_NAME|redis" ]] || return 1
  binding=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "6379/tcp") 0).HostIp}}:{{(index (index .NetworkSettings.Ports "6379/tcp") 0).HostPort}}' "$CONTAINER_ID" 2>/dev/null) || return 1
  [[ $binding == "127.0.0.1:$REDIS_PORT" ]]
}

compose() { docker compose --project-name "$PROJECT_NAME" --file "$COMPOSE_FILE" "$@"; }

snapshot_daily() {
  {
    docker ps -a --filter 'label=com.docker.compose.project=gopulse' --format '{{.ID}}|{{.Names}}|{{.Status}}' 2>/dev/null | sort
    docker volume ls --filter 'label=com.docker.compose.project=gopulse' --format '{{.Name}}' 2>/dev/null | sort
    find "$REPO_ROOT/.run" -maxdepth 1 -type f -name '*.json' -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum
  }
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ -n ${TIMEOUT_EXPORTER_PID:-} ]]; then stop_exporter "$TIMEOUT_EXPORTER_PID" "$TEMP_DIR/timeout.json" || status=1; fi
  if [[ -n ${AUTH_EXPORTER_PID:-} ]]; then stop_exporter "$AUTH_EXPORTER_PID" "$TEMP_DIR/auth.json" || status=1; fi
  if [[ -n ${EXPORTER_PID:-} ]]; then stop_exporter "$EXPORTER_PID" "$TEMP_DIR/exporter.json" || status=1; fi
  if [[ -n ${TIMEOUT_SERVER_PID:-} ]]; then kill "$TIMEOUT_SERVER_PID" 2>/dev/null || true; wait "$TIMEOUT_SERVER_PID" 2>/dev/null || true; fi
  if [[ -n ${CONTAINER_ID:-} ]]; then
    if container_owned; then compose down --volumes --remove-orphans >/dev/null || status=1
    else fail 'Refusing to remove isolation resources because ownership validation failed.' || true; status=1
    fi
  fi
  if [[ -n ${DAILY_BEFORE:-} && -d ${TEMP_DIR:-} ]]; then
    snapshot_daily > "$TEMP_DIR/daily-after"
    if ! cmp -s "$DAILY_BEFORE" "$TEMP_DIR/daily-after"; then
      fail 'Daily gopulse resources changed during exporter acceptance.' || true
      diff -u "$DAILY_BEFORE" "$TEMP_DIR/daily-after" >&2 || true
      status=1
    fi
  fi
  [[ -z ${TEMP_DIR:-} ]] || rm -rf -- "$TEMP_DIR"
  CLEANED=1
  exit "$status"
}
trap cleanup EXIT INT TERM

wait_health() {
  local port=$1
  for _ in {1..60}; do
    if [[ $(curl -sS --max-time 1 -o "$TEMP_DIR/health" -w '%{http_code}' "http://127.0.0.1:$port/health" 2>/dev/null || true) == 200 ]]; then
      python3 - "$TEMP_DIR/health" <<'PY' && return 0
import json,sys
raise SystemExit(0 if json.load(open(sys.argv[1],encoding='utf-8')) == {'status':'ok','service':'redis-exporter'} else 1)
PY
    fi
    sleep 0.1
  done
  return 1
}

scrape() {
  local port=$1 output=$2 headers=$3
  curl -sS --max-time 3 --dump-header "$headers" --output "$output" --write-out '%{http_code}' "http://127.0.0.1:$port/metrics"
}

validate_success_metrics() {
  python3 - "$1" <<'PY'
import math,re,sys
text=open(sys.argv[1],encoding='utf-8').read()
expected={
'gopulse_redis_up':'gauge','gopulse_redis_uptime_seconds':'gauge','gopulse_redis_connected_clients':'gauge',
'gopulse_redis_used_memory_bytes':'gauge','gopulse_redis_commands_processed_total':'counter',
'gopulse_redis_keyspace_hits_total':'counter','gopulse_redis_keyspace_misses_total':'counter',
'gopulse_redis_cpu_seconds_total':'counter','gopulse_redis_db_keys':'gauge','gopulse_redis_db_expiring_keys':'gauge'}
for name, kind in expected.items():
    if not re.search(rf'^# HELP {name} .+$',text,re.M) or not re.search(rf'^# TYPE {name} {kind}$',text,re.M): raise SystemExit(1)
samples={}
for line in text.splitlines():
    if not line or line.startswith('#'): continue
    name,value=line.rsplit(' ',1)
    number=float(value)
    if not math.isfinite(number) or name in samples: raise SystemExit(1)
    samples[name]=number
required=set(expected)-{'gopulse_redis_cpu_seconds_total'}
if not required.issubset({name.split('{')[0] for name in samples}): raise SystemExit(1)
if set(name for name in samples if name.startswith('gopulse_redis_cpu_seconds_total')) != {'gopulse_redis_cpu_seconds_total{mode="user"}','gopulse_redis_cpu_seconds_total{mode="system"}'}: raise SystemExit(1)
if samples.get('gopulse_redis_up') != 1 or samples.get('gopulse_redis_db_keys{db="5"}') != 2 or samples.get('gopulse_redis_db_expiring_keys{db="5"}') != 1: raise SystemExit(1)
PY
}

run_self_test() {
  TEMP_DIR=$(mktemp -d)
  local binary=/bin/sleep record="$TEMP_DIR/bad.json" pid
  setsid "$binary" 30 & pid=$!
  sleep 0.05
  write_record "$pid" "$record" "$TEMP_DIR" "$binary" wrong-marker
  if validate_record "$record" "$TEMP_DIR" "$binary" "$binary" >/dev/null 2>&1; then
    kill -- "-$pid" 2>/dev/null || true
    fail 'Process identity validator accepted a mismatched marker.'
    return 1
  fi
  kill -0 "$pid" 2>/dev/null || { fail 'Negative identity check stopped an unrelated process.'; return 1; }
  kill -- "-$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  valid_project 'gopulse-exporter-deadbeefcafe' || { fail 'Valid project token was rejected.'; return 1; }
  if valid_project 'gopulse'; then fail 'Unsafe project name was accepted.'; return 1; fi
  rm -rf -- "$TEMP_DIR"; TEMP_DIR=
  info 'Self-test passed: malformed ownership data is rejected without Docker or unrelated process termination.'
}

main() {
  if [[ ${1:-} == --self-test ]]; then run_self_test; return; fi
  [[ $# == 0 ]] || { fail 'Usage: scripts/verify-exporter.sh [--self-test]'; return 1; }
  local tool
  for tool in docker go curl python3 sha256sum; do command -v "$tool" >/dev/null || { fail "Missing required tool: $tool"; return 1; }; done
  docker compose version >/dev/null 2>&1 || { fail 'Docker Compose is unavailable.'; return 1; }
  docker info >/dev/null 2>&1 || { fail 'Docker daemon is unavailable.'; return 1; }
  TEMP_DIR=$(mktemp -d)
  TOKEN=$(printf '%s' "$RANDOM-$$-$(date +%s%N)" | sha256sum | cut -c1-12)
  PROJECT_NAME="gopulse-exporter-$TOKEN"
  valid_project "$PROJECT_NAME" || { fail 'Generated unsafe project name.'; return 1; }
  PASSWORD="exporter-$TOKEN-secret"
  REDIS_PORT=$(random_port); EXPORTER_PORT=$(random_port); AUTH_EXPORTER_PORT=$(random_port); TIMEOUT_EXPORTER_PORT=$(random_port)
  [[ $REDIS_PORT != "$EXPORTER_PORT" && $EXPORTER_PORT != "$AUTH_EXPORTER_PORT" && $AUTH_EXPORTER_PORT != "$TIMEOUT_EXPORTER_PORT" ]] || { fail 'Random ports collided; rerun acceptance.'; return 1; }
  COMPOSE_FILE="$TEMP_DIR/compose.yaml"
  cat > "$COMPOSE_FILE" <<YAML
services:
  redis:
    image: redis:7.2.5-alpine
    command: ["redis-server", "--appendonly", "yes", "--requirepass", "$PASSWORD"]
    ports: ["127.0.0.1:$REDIS_PORT:6379"]
    volumes: ["redis_data:/data"]
volumes:
  redis_data:
YAML
  snapshot_daily > "$TEMP_DIR/daily-before"; DAILY_BEFORE="$TEMP_DIR/daily-before"
  info "Building isolated exporter and starting project $PROJECT_NAME."
  (cd "$EXPORTER_DIR" && go build -o "$TEMP_DIR/gopulse-redis-exporter" ./cmd/redis-exporter)
  compose up -d
  CONTAINER_ID=$(compose ps -q redis)
  container_owned || { fail 'Redis isolation container ownership validation failed.'; return 1; }
  for _ in {1..60}; do docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" ping 2>/dev/null | grep -q PONG && break; sleep 0.2; done
  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" ping 2>/dev/null | grep -q PONG || { fail 'Redis did not become available.'; return 1; }
  start_exporter "$EXPORTER_PORT" "$PASSWORD" "$REDIS_PORT" "$TEMP_DIR/exporter.json" "$TEMP_DIR/exporter.log" EXPORTER_PID
  wait_health "$EXPORTER_PORT" || { fail 'Exporter health endpoint did not become ready.'; return 1; }

  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" -n 5 set persistent value >/dev/null
  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" -n 5 set expiring value EX 120 >/dev/null
  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" -n 5 get persistent >/dev/null
  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" -n 5 get missing >/dev/null
  local status
  status=$(scrape "$EXPORTER_PORT" "$TEMP_DIR/metrics" "$TEMP_DIR/headers")
  [[ $status == 200 ]] || { fail "Initial scrape returned HTTP $status."; return 1; }
  grep -qi '^content-type: text/plain; version=0.0.4; charset=utf-8' "$TEMP_DIR/headers" || { fail 'Prometheus Content-Type mismatch.'; return 1; }
  validate_success_metrics "$TEMP_DIR/metrics" || { fail 'Metric family/type/value contract failed.'; return 1; }
  docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" -n 5 info stats keyspace > "$TEMP_DIR/info"
  python3 - "$TEMP_DIR/metrics" "$TEMP_DIR/info" <<'PY'
import re,sys
metrics=open(sys.argv[1],encoding='utf-8').read(); info=open(sys.argv[2],encoding='utf-8').read()
def metric(name): return float(re.search(rf'^{re.escape(name)}(?:\{{[^}}]+\}})? ([0-9.eE+-]+)$',metrics,re.M).group(1))
def field(name): return int(re.search(rf'^{name}:(\d+)$',info,re.M).group(1))
if metric('gopulse_redis_keyspace_hits_total') != field('keyspace_hits'): raise SystemExit(1)
if metric('gopulse_redis_keyspace_misses_total') != field('keyspace_misses'): raise SystemExit(1)
match=re.search(r'^db5:keys=(\d+),expires=(\d+),',info,re.M)
if not match or metric('gopulse_redis_db_keys') != int(match.group(1)) or metric('gopulse_redis_db_expiring_keys') != int(match.group(2)): raise SystemExit(1)
PY
  info 'Validated live Redis INFO values and all fixed metric families.'

  local original_pid=$EXPORTER_PID start_time elapsed
  container_owned || { fail 'Ownership changed before Redis stop.'; return 1; }
  compose stop redis >/dev/null
  start_time=$(date +%s%N); status=$(scrape "$EXPORTER_PORT" "$TEMP_DIR/down-metrics" "$TEMP_DIR/down-headers"); elapsed=$((($(date +%s%N)-start_time)/1000000))
  [[ $status == 503 && $elapsed -lt 2500 ]] || { fail "Stopped Redis scrape status/time was $status/${elapsed}ms."; return 1; }
  grep -q '^gopulse_redis_up 0$' "$TEMP_DIR/down-metrics" && [[ $(grep -vc '^#\|^gopulse_redis_up 0$\|^$' "$TEMP_DIR/down-metrics") -eq 0 ]] || { fail 'Failure response contained partial or stale metrics.'; return 1; }
  wait_health "$EXPORTER_PORT" || { fail 'Health failed while Redis was stopped.'; return 1; }
  [[ $(validate_record "$TEMP_DIR/exporter.json" "$EXPORTER_DIR" "$TEMP_DIR/gopulse-redis-exporter" "$TEMP_DIR/gopulse-redis-exporter") == "$original_pid" ]] || { fail 'Exporter identity changed during target failure.'; return 1; }

  compose start redis >/dev/null
  for _ in {1..60}; do docker exec "$CONTAINER_ID" redis-cli --no-auth-warning -a "$PASSWORD" ping 2>/dev/null | grep -q PONG && break; sleep 0.2; done
  for _ in {1..30}; do status=$(scrape "$EXPORTER_PORT" "$TEMP_DIR/recovered" "$TEMP_DIR/recovered-headers" || true); [[ $status == 200 ]] && break; sleep 0.2; done
  [[ $status == 200 ]] && grep -q '^gopulse_redis_up 1$' "$TEMP_DIR/recovered" || { fail 'Exporter did not recover without restart.'; return 1; }

  start_exporter "$AUTH_EXPORTER_PORT" wrong-password "$REDIS_PORT" "$TEMP_DIR/auth.json" "$TEMP_DIR/auth.log" AUTH_EXPORTER_PID
  wait_health "$AUTH_EXPORTER_PORT" || { fail 'Wrong-password exporter health failed.'; return 1; }
  status=$(scrape "$AUTH_EXPORTER_PORT" "$TEMP_DIR/auth-metrics" "$TEMP_DIR/auth-headers")
  [[ $status == 503 ]] && grep -q '^gopulse_redis_up 0$' "$TEMP_DIR/auth-metrics" || { fail 'Authentication failure contract mismatch.'; return 1; }
  grep -q '"reason":"redis_auth_failed"' "$TEMP_DIR/auth.log" || { fail 'Authentication reason code missing.'; return 1; }
  if grep -Fq 'wrong-password' "$TEMP_DIR/auth.log" || grep -Fq "127.0.0.1:$REDIS_PORT" "$TEMP_DIR/auth.log"; then fail 'Authentication log leaked target data.'; return 1; fi
  stop_exporter "$AUTH_EXPORTER_PID" "$TEMP_DIR/auth.json"; AUTH_EXPORTER_PID=

  local hang_port
  hang_port=$(random_port)
  python3 - "$hang_port" <<'PY' &
import socket,sys,time
sock=socket.socket(); sock.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); sock.bind(('127.0.0.1',int(sys.argv[1]))); sock.listen()
while True:
    connection,_=sock.accept(); time.sleep(5); connection.close()
PY
  TIMEOUT_SERVER_PID=$!; sleep 0.1
  start_exporter "$TIMEOUT_EXPORTER_PORT" no-secret "$hang_port" "$TEMP_DIR/timeout.json" "$TEMP_DIR/timeout.log" TIMEOUT_EXPORTER_PID 300ms
  status=$(scrape "$TIMEOUT_EXPORTER_PORT" "$TEMP_DIR/timeout-metrics" "$TEMP_DIR/timeout-headers")
  [[ $status == 503 ]] && grep -q '^gopulse_redis_up 0$' "$TEMP_DIR/timeout-metrics" && grep -q '"reason":"redis_timeout"' "$TEMP_DIR/timeout.log" || { fail 'Timeout failure contract mismatch.'; return 1; }
  stop_exporter "$TIMEOUT_EXPORTER_PID" "$TEMP_DIR/timeout.json"; TIMEOUT_EXPORTER_PID=
  kill "$TIMEOUT_SERVER_PID" 2>/dev/null || true; wait "$TIMEOUT_SERVER_PID" 2>/dev/null || true; TIMEOUT_SERVER_PID=

  stop_exporter "$EXPORTER_PID" "$TEMP_DIR/exporter.json"; EXPORTER_PID=
  if ss -ltn "sport = :$EXPORTER_PORT" 2>/dev/null | tail -n +2 | grep -q .; then fail 'Exporter port remained open after SIGTERM.'; return 1; fi
  info 'Acceptance passed: success, target stop, auth failure, timeout, recovery, SIGTERM, and ownership cleanup are verified.'
}

main "$@"
