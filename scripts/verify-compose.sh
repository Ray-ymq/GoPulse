#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
MODE=
KEEP=0
TEMP_DIR=
ENV_FILE=
PROJECT_NAME=
TOKEN=
VERSION=
REVISION=
RESOURCES_STARTED=0
SNAPSHOT_DIR=

info() { printf '[gopulse-compose] %s\n' "$*"; }
pass() { printf '[gopulse-compose] PASS: %s\n' "$*"; }
fail() { printf '[gopulse-compose] ERROR: %s\n' "$*" >&2; return 1; }
usage() {
  cat <<'USAGE'
Usage: scripts/verify-compose.sh --self-test
       scripts/verify-compose.sh --business [--keep]

--business builds and validates a fresh random Compose project using only host
Docker/Compose orchestration. Browser/API clients run in the acceptance image.
USAGE
}

validate_project_name() {
  [[ ${1:-} =~ ^gopulse-accept-[a-f0-9]{12}$ ]]
}

validate_loopback_binding() {
  [[ ${1:-} == 127.0.0.1 ]]
}

run_self_test() {
  local rejected=0 candidate
  validate_project_name gopulse-accept-012345abcdef || fail 'valid acceptance project was rejected'
  validate_loopback_binding 127.0.0.1 || fail 'loopback binding was rejected'
  for candidate in '' gopulse gopulse-accept-123 ../gopulse gopulse-accept-012345abcdeg GOPULSE-accept-012345abcdef; do
    if validate_project_name "$candidate"; then
      fail "unsafe project accepted: $candidate"
    fi
    rejected=$((rejected + 1))
  done
  if validate_loopback_binding 0.0.0.0 || validate_loopback_binding localhost; then
    fail 'non-literal loopback binding was accepted'
  fi
  pass "$rejected unsafe project names rejected before Docker access."
}

while (($#)); do
  case $1 in
    --self-test|--business) [[ -z $MODE ]] || { fail 'choose exactly one mode'; exit 2; }; MODE=$1; shift ;;
    --keep) KEEP=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; usage >&2; exit 2 ;;
  esac
done
[[ -n $MODE ]] || { usage >&2; exit 2; }
if [[ $MODE == --self-test ]]; then
  ((KEEP == 0)) || fail '--keep is only valid with --business'
  run_self_test
  exit 0
fi

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v git >/dev/null 2>&1 || fail 'git is required'
docker info >/dev/null 2>&1 || fail 'Docker Engine is unavailable'
docker compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is unavailable'
[[ -f $REPO_ROOT/VERSION ]] || fail 'VERSION is missing'
VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'VERSION must use major.minor.patch'
REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD)
TOKEN=$(tr -d '-' </proc/sys/kernel/random/uuid | cut -c1-12)
PROJECT_NAME="gopulse-accept-$TOKEN"
validate_project_name "$PROJECT_NAME" || fail 'generated project name is invalid'
TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gopulse-compose-$TOKEN.XXXXXX")
ENV_FILE="$TEMP_DIR/acceptance.env"
SNAPSHOT_DIR="$TEMP_DIR/snapshot"
mkdir -p "$SNAPSHOT_DIR"
export GOPULSE_VERSION=$VERSION GOPULSE_REVISION=$REVISION GOPULSE_ACCEPTANCE_TOKEN=$TOKEN

cat >"$ENV_FILE" <<ENV
APP_ENV=test
PUBLISHED_HOST=127.0.0.1
HTTP_PORT=0
FRONTEND_PORT=0
MYSQL_DATABASE=gopulse_$TOKEN
MYSQL_USER=user_$TOKEN
MYSQL_PASSWORD=mysql-$TOKEN
MYSQL_ROOT_PASSWORD=root-$TOKEN
REDIS_PASSWORD=redis-$TOKEN
REDIS_DB=0
RABBITMQ_USER=rabbit_$TOKEN
RABBITMQ_PASSWORD=rabbit-$TOKEN
AUTH_JWT_SECRET=jwt-$TOKEN-0123456789abcdef0123456789abcdef
AUTH_COOKIE_NAME=gopulse_$TOKEN
AUTH_COOKIE_SECURE=false
MONITOR_API_TOKEN=monitor-$TOKEN-0123456789abcdef0123456789
LOG_MONITOR_INGEST_TOKEN=logs-$TOKEN-0123456789abcdef0123456789ab
VICTORIAMETRICS_USERNAME=vm_$TOKEN
VICTORIAMETRICS_PASSWORD=vm-$TOKEN-0123456789abcdef0123456789abc
GOPULSE_VERSION=$VERSION
GOPULSE_REVISION=$REVISION
ENV
chmod 600 "$ENV_FILE"

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

snapshot_existing_resources() {
  docker ps -aq | sort >"$SNAPSHOT_DIR/containers"
  docker network ls -q | sort >"$SNAPSHOT_DIR/networks"
  docker volume ls -q | sort >"$SNAPSHOT_DIR/volumes"
}

assert_snapshot_preserved() {
  local kind id
  for kind in containers networks volumes; do
    while IFS= read -r id; do
      [[ -n $id ]] || continue
      case $kind in
        containers) docker inspect "$id" >/dev/null 2>&1 || fail "pre-existing container disappeared: $id" ;;
        networks) docker network inspect "$id" >/dev/null 2>&1 || fail "pre-existing network disappeared: $id" ;;
        volumes) docker volume inspect "$id" >/dev/null 2>&1 || fail "pre-existing volume disappeared: $id" ;;
      esac
    done <"$SNAPSHOT_DIR/$kind"
  done
}

assert_project_absent() {
  local found=0
  [[ -z $(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || found=1
  [[ -z $(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || found=1
  [[ -z $(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || found=1
  ((found == 0)) || fail "refusing to reuse existing resources for $PROJECT_NAME"
}

service_id() {
  local service=$1 ids count
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service")
  count=$(sed '/^$/d' <<<"$ids" | wc -l | tr -d ' ')
  [[ $count == 1 ]] || { fail "$service must have exactly one container; found $count"; return 1; }
  printf '%s\n' "$ids"
}

owned_service_id() {
  local service=$1 id project service_label working_dir config_files
  id=$(service_id "$service")
  project=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
  service_label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id")
  working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
  config_files=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")
  [[ $project == "$PROJECT_NAME" && $service_label == "$service" && $working_dir == "$COMPOSE_WORKDIR" && $config_files == *"$COMPOSE_FILE"* ]] || {
    fail "$service container failed ownership validation"
    return 1
  }
  printf '%s\n' "$id"
}

assert_project_ownership() {
  local id label
  validate_project_name "$PROJECT_NAME" || { fail 'unsafe project name at cleanup boundary'; return 1; }
  while IFS= read -r id; do
    [[ -n $id ]] || continue
    label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
    [[ $label == "$PROJECT_NAME" ]] || { fail "container $id has a mismatched project label"; return 1; }
    [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id") == "$COMPOSE_WORKDIR" ]] || {
      fail "container $id has a mismatched working directory"
      return 1
    }
  done < <(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME")
  for kind in network volume; do
    while IFS= read -r id; do
      [[ -n $id ]] || continue
      label=$(docker "$kind" inspect --format '{{index .Labels "com.docker.compose.project"}}' "$id")
      [[ $label == "$PROJECT_NAME" ]] || { fail "$kind $id has a mismatched project label"; return 1; }
    done < <(docker "$kind" ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME")
  done
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if ((KEEP)); then
    info "Keeping $PROJECT_NAME for diagnosis. Environment: $ENV_FILE"
    exit "$status"
  fi
  if ((RESOURCES_STARTED)); then
    if assert_project_ownership; then
      compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    else
      status=1
    fi
  fi
  assert_snapshot_preserved || status=1
  rm -rf "$TEMP_DIR"
  exit "$status"
}
on_signal() { exit "$1"; }
trap cleanup EXIT
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

wait_healthy() {
  local service=$1 deadline=$((SECONDS + 180)) id state
  id=$(owned_service_id "$service")
  while ((SECONDS < deadline)); do
    state=$(docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id" 2>/dev/null || true)
    [[ $state == running\|healthy ]] && return 0
    sleep 2
  done
  compose logs --tail 120 "$service" >&2 || true
  fail "$service did not become healthy"
}

wait_running() {
  local service=$1 deadline=$((SECONDS + 60)) id state
  id=$(owned_service_id "$service")
  while ((SECONDS < deadline)); do
    state=$(docker inspect --format '{{.State.Status}}' "$id" 2>/dev/null || true)
    [[ $state == running ]] && return 0
    sleep 1
  done
  compose logs --tail 120 "$service" >&2 || true
  fail "$service did not remain running"
}

assert_initial_state() {
  local service id state health exit_code
  for service in mysql redis rabbitmq elasticsearch backend frontend; do
    id=$(owned_service_id "$service")
    state=$(docker inspect --format '{{.State.Status}}' "$id")
    health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id")
    [[ $state == running && $health == healthy ]] || fail "$service is not running and healthy"
  done
  for service in business-worker search-indexer; do
    id=$(owned_service_id "$service")
    [[ $(docker inspect --format '{{.State.Status}}' "$id") == running ]] || fail "$service is not running"
  done
  for service in migrate search-init; do
    id=$(owned_service_id "$service")
    state=$(docker inspect --format '{{.State.Status}}' "$id")
    exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$id")
    [[ $state == exited && $exit_code == 0 ]] || fail "$service did not complete successfully"
  done
  pass 'All business services and initialization jobs reached their expected states.'
}

assert_image_contracts() {
  local service image expected_entry user version revision source entrypoint
  for service in frontend backend business-worker search-indexer; do
    image=$(docker inspect --format '{{.Image}}' "$(owned_service_id "$service")")
    user=$(docker image inspect --format '{{.Config.User}}' "$image")
    version=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image")
    revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
    source=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source"}}' "$image")
    entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "$image")
    case $service in
      frontend) expected_entry='["nginx"]' ;;
      backend) expected_entry='["/usr/local/bin/server"]' ;;
      business-worker) expected_entry='["/usr/local/bin/business-worker"]' ;;
      search-indexer) expected_entry='["/usr/local/bin/search-indexer"]' ;;
    esac
    [[ $user =~ ^[0-9]+:[0-9]+$ ]] || fail "$service image does not use a numeric uid:gid"
    [[ $version == "$VERSION" && $revision == "$REVISION" ]] || fail "$service image metadata mismatch"
    [[ $source == https://github.com/Ray-ymq/GoPulse ]] || fail "$service image source label mismatch"
    [[ $entrypoint == "$expected_entry" ]] || fail "$service entrypoint mismatch: $entrypoint"
  done
  docker run --rm --entrypoint /bin/sh "gopulse/backend:$VERSION" -ec \
    'test -x /usr/local/bin/server && test -x /usr/local/bin/migrate && test -x /usr/local/bin/search-reindex && test -x /usr/local/bin/admin-role && ! command -v go && test ! -d /src'
  docker run --rm --entrypoint /bin/sh "gopulse/frontend:$VERSION" -ec \
    '! command -v node && ! command -v npm && test ! -d /src && ! find /usr/share/nginx/html -name "*.map" -print -quit | grep -q . && ! grep -R -E "(rabbitmq|elasticsearch|victoriametrics|monitor):[0-9]+|local-.*token" /usr/share/nginx/html'
  pass 'Image labels, numeric users, entrypoints, runtime contents, and Frontend bundle boundaries passed.'
}

assert_network_and_ports() {
  local service id networks bindings host_ips
  for service in frontend backend business-worker search-indexer mysql redis rabbitmq elasticsearch; do
    id=$(owned_service_id "$service")
    networks=$(docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}} {{end}}' "$id")
    case $service in
      frontend) [[ $networks == *"${PROJECT_NAME}_edge "* && $networks != *"${PROJECT_NAME}_business "* ]] || fail 'Frontend network boundary mismatch' ;;
      backend) [[ $networks == *"${PROJECT_NAME}_edge "* && $networks == *"${PROJECT_NAME}_business "* ]] || fail 'Backend network boundary mismatch' ;;
      *) [[ $networks == *"${PROJECT_NAME}_business "* && $networks != *"${PROJECT_NAME}_edge "* ]] || fail "$service network boundary mismatch" ;;
    esac
    bindings=$(docker inspect --format '{{json .HostConfig.PortBindings}}' "$id")
    if [[ $service == frontend || $service == backend ]]; then
      host_ips=$(docker inspect --format '{{range $p, $items := .HostConfig.PortBindings}}{{range $items}}{{.HostIp}} {{end}}{{end}}' "$id")
      [[ $host_ips == '127.0.0.1 ' ]] || fail "$service is not bound exactly once to IPv4 loopback"
    else
      [[ $bindings == null || $bindings == '{}' ]] || fail "$service unexpectedly publishes a host port"
    fi
  done
  pass 'edge/business networks and loopback-only user ports match the contract.'
}

run_acceptance() {
  local scenario=$1
  info "Running browser scenario: $scenario"
  compose --profile acceptance run --rm --no-deps -e "GOPULSE_ACCEPTANCE_SCENARIO=$scenario" acceptance e2e/compose-business.spec.ts
}

rerun_initializers() {
  compose run --rm --no-deps migrate
  compose run --rm --no-deps search-init
  pass 'Migration and search initialization are idempotent.'
}

exercise_redis_fallback() {
  owned_service_id redis >/dev/null
  compose stop redis
  run_acceptance redis-fallback
  compose start redis
  wait_healthy redis
  pass 'MySQL-backed browser reads and writes survived Redis unavailability.'
}

exercise_worker_recovery() {
  owned_service_id business-worker >/dev/null
  compose pause business-worker
  run_acceptance worker-seed
  compose unpause business-worker
  wait_running business-worker
  run_acceptance worker-verify
  pass 'Durable notification events converged after Business Worker recovery.'
}

exercise_indexer_recovery() {
  owned_service_id search-indexer >/dev/null
  compose pause search-indexer
  run_acceptance indexer-seed
  compose unpause search-indexer
  wait_running search-indexer
  run_acceptance indexer-verify
  pass 'Durable search events converged after Search Indexer recovery.'
}

recreate_applications() {
  local service before after
  for service in backend business-worker search-indexer; do
    before=$(owned_service_id "$service")
    compose up --detach --force-recreate --no-deps "$service"
    if [[ $service == backend ]]; then wait_healthy "$service"; else wait_running "$service"; fi
    after=$(owned_service_id "$service")
    [[ $before != "$after" ]] || fail "$service container was not replaced"
  done
  compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
  run_acceptance persistence
  pass 'Backend, Worker, and Indexer replacement preserved external business facts.'
}

exercise_signal_shutdown() {
  local service id exit_code
  for service in backend business-worker search-indexer; do
    id=$(owned_service_id "$service")
    compose stop --timeout 20 "$service"
    exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$id")
    [[ $exit_code == 0 ]] || fail "$service did not stop cleanly after SIGTERM (exit $exit_code)"
    compose start "$service"
    if [[ $service == backend ]]; then wait_healthy "$service"; else wait_running "$service"; fi
  done
  compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
  pass 'Backend, Worker, and Indexer handled bounded signal shutdown as PID 1.'
}

exercise_persistent_down_up() {
  assert_project_ownership
  compose down --remove-orphans
  RESOURCES_STARTED=0
  for volume in mysql_data redis_data rabbitmq_data elasticsearch_data; do
    docker volume inspect "${PROJECT_NAME}_$volume" >/dev/null || fail "persistent volume disappeared: $volume"
  done
  RESOURCES_STARTED=1
  compose up --detach --wait --wait-timeout 300 frontend backend business-worker search-indexer
  assert_initial_state
  run_acceptance persistence
  pass 'Project down/up retained business, notification, and search facts.'
}

snapshot_existing_resources
assert_project_absent
info "Building isolated GoPulse $VERSION images for $PROJECT_NAME."
compose build backend business-worker search-indexer frontend acceptance
RESOURCES_STARTED=1
if ! compose up --detach --wait --wait-timeout 300 frontend backend business-worker search-indexer; then
  compose ps >&2 || true
  compose logs --tail 160 >&2 || true
  fail 'cold Compose startup failed'
fi
assert_initial_state
assert_image_contracts
assert_network_and_ports
rerun_initializers
compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
run_acceptance business
exercise_redis_fallback
exercise_worker_recovery
exercise_indexer_recovery
recreate_applications
exercise_signal_shutdown
exercise_persistent_down_up
assert_project_ownership
pass 'Phase-12-01 container-only business acceptance passed.'
