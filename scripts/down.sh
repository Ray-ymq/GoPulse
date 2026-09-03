#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
BACKEND_DIR="$REPO_ROOT/backend"
REDIS_EXPORTER_DIR="$REPO_ROOT/exporters/redis"
FRONTEND_DIR="$REPO_ROOT/frontend"
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
ENV_FILE="$REPO_ROOT/.env"
ENV_EXAMPLE_FILE="$REPO_ROOT/.env.example"
RUN_DIR="$REPO_ROOT/.run"
LOCK_PATH="$RUN_DIR/dev.lock"
BACKEND_RECORD="$RUN_DIR/backend.json"
WORKER_RECORD="$RUN_DIR/business-worker.json"
SEARCH_INDEXER_RECORD="$RUN_DIR/search-indexer.json"
REDIS_EXPORTER_RECORD="$RUN_DIR/redis-exporter.json"
FRONTEND_RECORD="$RUN_DIR/frontend.json"
BACKEND_BINARY="$RUN_DIR/bin/gopulse-backend"
WORKER_BINARY="$RUN_DIR/bin/gopulse-business-worker"
SEARCH_INDEXER_BINARY="$RUN_DIR/bin/gopulse-search-indexer"
REDIS_EXPORTER_BINARY="$RUN_DIR/bin/gopulse-redis-exporter"
VITE_CONFIG="$FRONTEND_DIR/vite.config.ts"
PROJECT_NAME=gopulse
COMPOSE_KEYS=(
  MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PORT
  REDIS_PASSWORD REDIS_PORT RABBITMQ_USER RABBITMQ_PASSWORD
  RABBITMQ_PORT RABBITMQ_MANAGEMENT_PORT ELASTICSEARCH_PORT
)
declare -A DOTENV=()
declare -A DEFAULTS=([ELASTICSEARCH_PORT]=9200)

info() {
  printf '[gopulse] %s\n' "$*"
}

fail() {
  printf '[gopulse] ERROR: %s\n' "$*" >&2
  return 1
}

read_dotenv() {
  local path=$1 raw line key value quote line_number=0
  DOTENV=()
  [[ -f "$path" ]] || { fail "Environment file not found: $path"; return 1; }
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    ((line_number += 1))
    raw=${raw%$'\r'}
    line=${raw#"${raw%%[![:space:]]*}"}
    line=${line%"${line##*[![:space:]]}"}
    [[ -z "$line" || ${line:0:1} == '#' ]] && continue
    if [[ $line =~ ^export([[:space:]]|$) ]]; then
      fail "Unsupported dotenv syntax at line $line_number: export is not allowed."
      return 1
    fi
    if [[ ! $line =~ ^([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
      fail "Invalid dotenv assignment at line $line_number."
      return 1
    fi
    key=${BASH_REMATCH[1]}
    value=${BASH_REMATCH[2]}
    value=${value#"${value%%[![:space:]]*}"}
    value=${value%"${value##*[![:space:]]}"}
    if [[ -n "$value" && (${value:0:1} == "'" || ${value:0:1} == '"') ]]; then
      quote=${value:0:1}
      if ((${#value} < 2)) || [[ ${value: -1} != "$quote" ]]; then
        fail "Unterminated quoted dotenv value for $key at line $line_number."
        return 1
      fi
      value=${value:1:${#value}-2}
      [[ $value != *"$quote"* ]] || { fail "Embedded quote syntax is not supported for $key at line $line_number."; return 1; }
    elif [[ $value == *"'" || $value == *'"' ]]; then
      fail "Mismatched quote in dotenv value for $key at line $line_number."
      return 1
    fi
    DOTENV["$key"]=$value
  done < "$path"
}

validate_record() {
  local path=$1 expected_cwd=$2 expected_marker=$3 expected_executable=${4:-}
  python3 - "$path" "$expected_cwd" "$expected_marker" "$expected_executable" <<'PY'
import json
import os
import sys
path, expected_cwd, expected_marker, expected_executable = sys.argv[1:]
try:
    record = json.load(open(path, encoding='utf-8'))
    pid = int(record['pid'])
    start_ticks = str(record['startTicks'])
    executable = os.path.realpath(str(record['executablePath']))
    cwd = os.path.realpath(str(record['workingDirectory']))
    marker = str(record['commandLineMarker'])
except Exception:
    print('record is malformed')
    raise SystemExit(1)
if cwd != os.path.realpath(expected_cwd) or marker != expected_marker:
    print('record identity does not match this repository')
    raise SystemExit(1)
if expected_executable and executable != os.path.realpath(expected_executable):
    print('recorded executable does not match the expected application')
    raise SystemExit(1)
try:
    stat = open(f'/proc/{pid}/stat', encoding='utf-8').read().strip()
    command_end = stat.rfind(')')
    fields = stat[command_end + 2:].split()
    actual_start_ticks = fields[19]
    actual_executable = os.path.realpath(f'/proc/{pid}/exe')
    command_line = open(f'/proc/{pid}/cmdline', 'rb').read().replace(b'\0', b' ').decode(errors='replace')
except Exception:
    print('recorded process is not running')
    raise SystemExit(1)
if actual_start_ticks != start_ticks:
    print('process start identity does not match')
    raise SystemExit(1)
if actual_executable != executable:
    print('process executable does not match')
    raise SystemExit(1)
if expected_marker not in command_line:
    print('process command line does not match the recorded project context')
    raise SystemExit(1)
print(pid)
PY
}

stop_recorded_application() {
  local name=$1 path=$2 cwd=$3 marker=$4 executable=${5:-} result pid
  if [[ ! -f "$path" ]]; then
    info "$name is not recorded as running."
    return 0
  fi
  if result=$(validate_record "$path" "$cwd" "$marker" "$executable"); then
    pid=$result
    kill -TERM -- "-$pid" 2>/dev/null || true
    for _ in {1..30}; do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
    kill -KILL -- "-$pid" 2>/dev/null || true
    info "Stopped $name (PID $pid)."
  else
    if [[ $result != 'recorded process is not running' ]]; then
      fail "$name record cannot be proven stale; refusing to signal a process or remove the record ($result)."
      return 1
    fi
    info "Removed stale $name record without stopping a process ($result)."
  fi
  rm -f -- "$path"
}

clear_lock() {
  [[ -f "$LOCK_PATH" ]] || return 0
  local attempt platform
  for attempt in {1..50}; do
    if exec 9<>"$LOCK_PATH" 2>/dev/null && flock -n 9; then
      platform=$(python3 - "$LOCK_PATH" <<'PY'
import json
import sys
try:
    print(json.load(open(sys.argv[1], encoding='utf-8')).get('platform', ''))
except Exception:
    print('')
PY
)
      if [[ -n "$platform" && $platform != unix ]]; then
        flock -u 9 || true
        exec 9>&-
        fail 'The development run lock belongs to another platform. Run the matching down script.'
        return 1
      fi
      rm -f -- "$LOCK_PATH"
      flock -u 9 || true
      exec 9>&-
      info 'Removed the development run lock.'
      return 0
    fi
    exec 9>&- 2>/dev/null || true
    sleep 0.1
  done
  fail 'The development run lock is still active. Stop the foreground dev script with Ctrl+C, then retry.'
}

compose_down() {
  command -v docker >/dev/null 2>&1 || { fail 'Docker is required to stop Compose infrastructure.'; return 1; }
  docker compose version >/dev/null 2>&1 || { fail 'Docker Compose is unavailable.'; return 1; }
  local env_path key
  if [[ -f "$ENV_FILE" ]]; then env_path=$ENV_FILE; else env_path=$ENV_EXAMPLE_FILE; fi
  [[ -f "$env_path" ]] || { fail 'Neither .env nor .env.example is available for Compose interpolation.'; return 1; }
  read_dotenv "$env_path"
  for key in "${COMPOSE_KEYS[@]}"; do
    if [[ ! -v $key && -v DOTENV[$key] ]]; then
      export "$key=${DOTENV[$key]}"
    elif [[ ! -v $key && -v DEFAULTS[$key] ]]; then
      export "$key=${DEFAULTS[$key]}"
    fi
  done
  docker compose --project-name "$PROJECT_NAME" --env-file "$env_path" --file "$COMPOSE_FILE" down
  info 'Compose infrastructure is stopped; named volumes were preserved.'
}

main() {
  command -v python3 >/dev/null 2>&1 || { fail 'python3 is required to validate process records.'; return 1; }
  command -v flock >/dev/null 2>&1 || { fail 'flock is required to manage the development run lock.'; return 1; }
  stop_recorded_application Frontend "$FRONTEND_RECORD" "$FRONTEND_DIR" "$VITE_CONFIG" "$(command -v node 2>/dev/null || true)"
  stop_recorded_application "Redis Exporter" "$REDIS_EXPORTER_RECORD" "$REDIS_EXPORTER_DIR" "$REDIS_EXPORTER_BINARY" "$REDIS_EXPORTER_BINARY"
  stop_recorded_application "Search Indexer" "$SEARCH_INDEXER_RECORD" "$BACKEND_DIR" "$SEARCH_INDEXER_BINARY" "$SEARCH_INDEXER_BINARY"
  stop_recorded_application "Business Worker" "$WORKER_RECORD" "$BACKEND_DIR" "$WORKER_BINARY" "$WORKER_BINARY"
  stop_recorded_application Backend "$BACKEND_RECORD" "$BACKEND_DIR" "$BACKEND_BINARY" "$BACKEND_BINARY"
  clear_lock
  compose_down
}

main
