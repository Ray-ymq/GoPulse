#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
KEEP=0
RESOURCES_STARTED=0
TEMP_DIR=
ENV_FILE=
SNAPSHOT_DIR=

info() { printf '[gopulse-observability] %s\n' "$*"; }
pass() { printf '[gopulse-observability] PASS: %s\n' "$*"; }
fail() { printf '[gopulse-observability] ERROR: %s\n' "$*" >&2; return 1; }
usage() { printf 'Usage: scripts/verify-compose-observability.sh [--keep]\n'; }

while (($#)); do
  case $1 in
    --keep) KEEP=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; exit 2 ;;
  esac
done

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v git >/dev/null 2>&1 || fail 'git is required'
command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required'
docker info >/dev/null 2>&1 || fail 'Docker Engine is unavailable'
docker compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is unavailable'
VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'VERSION must use major.minor.patch'
IFS=. read -r VERSION_MAJOR VERSION_MINOR VERSION_PATCH <<<"$VERSION"
UPDATE_VERSION="$VERSION_MAJOR.$VERSION_MINOR.$((VERSION_PATCH + 1))"
REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD)
TOKEN=$(tr -d '-' </proc/sys/kernel/random/uuid | cut -c1-12)
PROJECT_NAME="gopulse-observe-$TOKEN"
[[ $PROJECT_NAME =~ ^gopulse-observe-[a-f0-9]{12}$ ]] || fail 'generated project name is invalid'
TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gopulse-observability-$TOKEN.XXXXXX")
ENV_FILE="$TEMP_DIR/acceptance.env"
SNAPSHOT_DIR="$TEMP_DIR/snapshot"
mkdir -p "$SNAPSHOT_DIR"
ADMIN_USERNAME="admin_$TOKEN"
USER_USERNAME="user_$TOKEN"
PASSWORD="Acceptance-$TOKEN-password"
export GOPULSE_VERSION=$VERSION GOPULSE_REVISION=$REVISION GOPULSE_UPDATE_VERSION=$UPDATE_VERSION

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
ROUTER_API_TOKEN=router-$TOKEN-0123456789abcdef0123456789
MARSHALLER_API_TOKEN=marshaller-$TOKEN-0123456789abcdef012345
VICTORIAMETRICS_USERNAME=vm_$TOKEN
VICTORIAMETRICS_PASSWORD=vm-$TOKEN-0123456789abcdef0123456789abc
GOPULSE_VERSION=$VERSION
GOPULSE_REVISION=$REVISION
GOPULSE_UPDATE_VERSION=$UPDATE_VERSION
GOPULSE_OBSERVABILITY_ADMIN_USERNAME=$ADMIN_USERNAME
GOPULSE_OBSERVABILITY_USER_USERNAME=$USER_USERNAME
GOPULSE_OBSERVABILITY_PASSWORD=$PASSWORD
ENV
chmod 600 "$ENV_FILE"

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

snapshot_existing_resources() {
  local ref id
  local -a replaced_refs=(
    "gopulse/backend:$VERSION"
    "gopulse/business-worker:$VERSION"
    "gopulse/search-indexer:$VERSION"
    "gopulse/frontend:$VERSION"
    "gopulse/acceptance:$VERSION"
    "gopulse/router:$VERSION"
    "gopulse/marshaller:$VERSION"
    "gopulse/monitor:$VERSION"
    "gopulse/redis-exporter:$VERSION"
  )
  docker ps -aq | sort >"$SNAPSHOT_DIR/containers"
  docker network ls -q | sort >"$SNAPSHOT_DIR/networks"
  docker volume ls -q | sort >"$SNAPSHOT_DIR/volumes"
  docker image ls -q --no-trunc | sort -u >"$SNAPSHOT_DIR/all-images"
  : >"$SNAPSHOT_DIR/replaced-images"
  for ref in "${replaced_refs[@]}"; do
    id=$(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null || true)
    if [[ -n $id ]]; then
      printf '%s\n' "$id"
    fi
  done | sort -u >"$SNAPSHOT_DIR/replaced-images"
  # The exact versioned project tags above are intentionally rebuilt. Preserve
  # every other pre-existing image while allowing Docker to replace those IDs.
  comm -23 "$SNAPSHOT_DIR/all-images" "$SNAPSHOT_DIR/replaced-images" >"$SNAPSHOT_DIR/images"
}

assert_snapshot_preserved() {
  local kind id
  for kind in containers networks volumes images; do
    while IFS= read -r id; do
      [[ -n $id ]] || continue
      case $kind in
        containers) docker inspect "$id" >/dev/null 2>&1 || fail "pre-existing container disappeared: $id" ;;
        networks) docker network inspect "$id" >/dev/null 2>&1 || fail "pre-existing network disappeared: $id" ;;
        volumes) docker volume inspect "$id" >/dev/null 2>&1 || fail "pre-existing volume disappeared: $id" ;;
        images) docker image inspect "$id" >/dev/null 2>&1 || fail "pre-existing image disappeared: $id" ;;
      esac
    done <"$SNAPSHOT_DIR/$kind"
  done
}

assert_project_absent() {
  [[ -z $(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has containers'
  [[ -z $(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has networks'
  [[ -z $(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has volumes'
}

assert_project_ownership() {
  local id label working_dir
  [[ $PROJECT_NAME =~ ^gopulse-observe-[a-f0-9]{12}$ ]] || fail 'unsafe project at cleanup boundary'
  while IFS= read -r id; do
    [[ -n $id ]] || continue
    label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
    working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
    [[ $label == "$PROJECT_NAME" && $working_dir == "$COMPOSE_WORKDIR" ]] || fail "container ownership mismatch: $id"
  done < <(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME")
  for kind in network volume; do
    while IFS= read -r id; do
      [[ -n $id ]] || continue
      label=$(docker "$kind" inspect --format '{{index .Labels "com.docker.compose.project"}}' "$id")
      [[ $label == "$PROJECT_NAME" ]] || fail "$kind ownership mismatch: $id"
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
      compose --profile exporter down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    else
      status=1
    fi
  fi
  assert_snapshot_preserved || status=1
  if [[ -n $TEMP_DIR && $TEMP_DIR == "${TMPDIR:-/tmp}"/gopulse-observability-* ]]; then
    find "$TEMP_DIR" -depth -delete 2>/dev/null || status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

service_id() {
  local service=$1 ids count
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service")
  count=$(sed '/^$/d' <<<"$ids" | wc -l | tr -d ' ')
  [[ $count == 1 ]] || { fail "$service must have exactly one project container; found $count"; return 1; }
  printf '%s\n' "$ids"
}

owned_service_id() {
  local service=$1 id
  id=$(service_id "$service")
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id") == "$PROJECT_NAME" ]] || fail "$service project label mismatch"
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id") == "$service" ]] || fail "$service label mismatch"
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id") == "$COMPOSE_WORKDIR" ]] || fail "$service working directory mismatch"
  printf '%s\n' "$id"
}

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
  local service=$1 deadline=$((SECONDS + 90)) id state
  id=$(owned_service_id "$service")
  while ((SECONDS < deadline)); do
    state=$(docker inspect --format '{{.State.Status}}' "$id" 2>/dev/null || true)
    [[ $state == running ]] && return 0
    sleep 1
  done
  compose logs --tail 120 "$service" >&2 || true
  fail "$service did not remain running"
}

assert_full_state() {
  local service id state health
  for service in mysql redis rabbitmq elasticsearch kafka victoriametrics router marshaller monitor backend frontend; do
    id=$(owned_service_id "$service")
    state=$(docker inspect --format '{{.State.Status}}' "$id")
    health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id")
    [[ $state == running && $health == healthy ]] || fail "$service state=$state health=$health"
  done
  for service in business-worker search-indexer; do
    [[ $(docker inspect --format '{{.State.Status}}' "$(owned_service_id "$service")") == running ]] || fail "$service is not running"
  done
  for service in migrate search-init kafka-init; do
    id=$(owned_service_id "$service")
    [[ $(docker inspect --format '{{.State.Status}}|{{.State.ExitCode}}' "$id") == exited\|0 ]] || fail "$service did not complete successfully"
  done
  pass 'Complete business and observability topology reached the expected state.'
}

assert_image_contracts() {
  local service image user version revision source entrypoint expected_entry readonly privileged caps binds
  for service in frontend backend business-worker search-indexer router marshaller monitor; do
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
      router) expected_entry='["/usr/local/bin/router"]' ;;
      marshaller) expected_entry='["/usr/local/bin/marshaller"]' ;;
      monitor) expected_entry='["/usr/local/bin/monitor"]' ;;
    esac
    [[ $user =~ ^[0-9]+:[0-9]+$ && $version == "$VERSION" && $revision == "$REVISION" && $source == https://github.com/Ray-ymq/GoPulse && $entrypoint == "$expected_entry" ]] || fail "$service image contract mismatch"
  done
  user=$(docker image inspect --format '{{.Config.User}}' "gopulse/redis-exporter:$VERSION")
  entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "gopulse/redis-exporter:$VERSION")
  version=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "gopulse/redis-exporter:$VERSION")
  [[ $user =~ ^[0-9]+:[0-9]+$ && $entrypoint == '["/usr/local/bin/gopulse-redis-exporter"]' && $version == "$VERSION" ]] || fail 'Redis Exporter image contract mismatch'
  for service in router marshaller monitor; do
    id=$(owned_service_id "$service")
    readonly=$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$id")
    privileged=$(docker inspect --format '{{.HostConfig.Privileged}}' "$id")
    caps=$(docker inspect --format '{{json .HostConfig.CapAdd}}' "$id")
    binds=$(docker inspect --format '{{json .HostConfig.Binds}}' "$id")
    [[ $readonly == true && $privileged == false && $caps == null && $binds != *docker.sock* ]] || fail "$service container privilege boundary mismatch"
  done
  for image in router marshaller monitor redis-exporter; do
    docker run --rm --entrypoint /bin/sh "gopulse/$image:$VERSION" -ec '! command -v go && test ! -d /src'
    if docker history --no-trunc "gopulse/$image:$VERSION" | grep -E '(MONITOR_API_TOKEN|ROUTER_API_TOKEN|MARSHALLER_API_TOKEN|LOG_MONITOR_INGEST_TOKEN|AUTH_JWT_SECRET)=' >/dev/null; then
      fail "$image history contains a runtime credential"
    fi
  done
  package_digest=$(docker run --rm --entrypoint /bin/sh "gopulse/monitor:$VERSION" -ec 'tar -xOzf /opt/gopulse/packages/gopulse-redis-exporter.tar.gz bin/gopulse-redis-exporter' | sha256sum | awk '{print $1}')
  image_digest=$(docker run --rm --entrypoint sha256sum "gopulse/redis-exporter:$VERSION" /usr/local/bin/gopulse-redis-exporter | awk '{print $1}')
  [[ $package_digest == "$image_digest" ]] || fail 'Monitor package and Redis Exporter image binary digest differ'
  pass 'Image metadata, non-root users, entrypoints, package digest, and privilege boundaries passed.'
}

assert_network_and_ports() {
  local service id networks bindings host_ips
  for service in frontend backend business-worker search-indexer mysql redis rabbitmq elasticsearch kafka victoriametrics router marshaller monitor; do
    id=$(owned_service_id "$service")
    networks=$(docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}} {{end}}' "$id")
    case $service in
      frontend) [[ $networks == *"${PROJECT_NAME}_edge "* && $networks != *"${PROJECT_NAME}_business "* && $networks != *"${PROJECT_NAME}_observability "* ]] || fail 'Frontend network boundary mismatch' ;;
      backend) [[ $networks == *"${PROJECT_NAME}_edge "* && $networks == *"${PROJECT_NAME}_business "* && $networks == *"${PROJECT_NAME}_observability "* ]] || fail 'Backend network boundary mismatch' ;;
      business-worker|search-indexer|elasticsearch) [[ $networks == *"${PROJECT_NAME}_business "* && $networks == *"${PROJECT_NAME}_observability "* && $networks != *"${PROJECT_NAME}_edge "* ]] || fail "$service network boundary mismatch" ;;
      monitor) [[ $networks == *"${PROJECT_NAME}_business "* && $networks == *"${PROJECT_NAME}_observability "* && $networks != *"${PROJECT_NAME}_edge "* ]] || fail 'Monitor network boundary mismatch' ;;
      kafka|victoriametrics|router|marshaller) [[ $networks == *"${PROJECT_NAME}_observability "* && $networks != *"${PROJECT_NAME}_business "* && $networks != *"${PROJECT_NAME}_edge "* ]] || fail "$service network boundary mismatch" ;;
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
  pass 'Edge, business, observability networks and loopback-only user ports match the contract.'
}

assert_bootstrap_status() {
  compose exec -T backend /bin/sh -ec '
    status=$(wget --quiet --header "Authorization: Bearer $MONITOR_API_TOKEN" --output-document=- http://monitor:9090/internal/v1/exporter-plugins/redis-exporter)
    printf "%s" "$status" | grep -q "\"version\":\"'"$VERSION"'\""
    printf "%s" "$status" | grep -q "\"desired_state\":\"running\""
    printf "%s" "$status" | grep -q "\"observed_state\":\"running\""
  '
}

run_scenario() {
  local scenario=$1
  info "Running browser scenario: $scenario"
  compose --profile acceptance run --rm --no-deps -e "GOPULSE_ACCEPTANCE_SCENARIO=$scenario" acceptance e2e/compose-observability.spec.ts
}

read_exporter_metrics() {
  local id=$1
  docker exec "$id" /bin/sh -ec \
    'printf "GET /metrics HTTP/1.0\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n" | nc -w 5 127.0.0.1 9121' |
    awk '{ sub(/\r$/, ""); if (body) print; else if ($0 == "") body=1 }'
}

register_and_promote() {
  run_scenario setup
  compose --profile operations run --rm --no-deps admin-role promote --username "$ADMIN_USERNAME"
}

exercise_failure() {
  local service=$1 scenario=$2
  owned_service_id "$service" >/dev/null
  compose stop "$service"
  run_scenario "$scenario"
  compose start "$service"
  wait_healthy "$service"
  if [[ $service == monitor ]]; then assert_bootstrap_status; fi
  pass "$service failure remained localized and recovered."
}

replace_service() {
  local service=$1 before after
  before=$(owned_service_id "$service")
  compose up --detach --force-recreate --no-deps "$service"
  wait_healthy "$service"
  after=$(owned_service_id "$service")
  [[ $before != "$after" ]] || fail "$service container was not replaced"
}

exercise_persistence() {
  local volume
  assert_project_ownership
  compose down --remove-orphans
  RESOURCES_STARTED=0
  for volume in mysql_data redis_data rabbitmq_data elasticsearch_data kafka_data victoriametrics_data monitor_plugin_data; do
    docker volume inspect "${PROJECT_NAME}_$volume" >/dev/null || fail "persistent volume disappeared: $volume"
  done
  RESOURCES_STARTED=1
  compose up --detach --wait --wait-timeout 420
  assert_full_state
  assert_bootstrap_status
  run_scenario persistence
  pass 'Full project down/up retained business, observability, offset, and plugin desired-state facts.'
}

exercise_standalone_exporter() {
  local id metrics deadline exit_code auth_id auth_metrics
  compose down --remove-orphans
  RESOURCES_STARTED=0
  RESOURCES_STARTED=1
  compose --profile exporter up --detach --wait --wait-timeout 180 redis redis-exporter
  id=$(owned_service_id redis-exporter)
  deadline=$((SECONDS + 20))
  while ((SECONDS < deadline)); do
    metrics=$(read_exporter_metrics "$id" || true)
    grep -q '^gopulse_redis_up 1$' <<<"$metrics" && break
    sleep 1
  done
  grep -q '^gopulse_redis_up 1$' <<<"$metrics" || fail 'standalone Exporter did not become scrape-ready'
  [[ $(grep -c '^# TYPE gopulse_redis_' <<<"$metrics") == 10 ]] || fail 'standalone Exporter metric family count mismatch'
  [[ $(grep -Ev '^#|^$' <<<"$metrics" | wc -l | tr -d ' ') == 11 ]] || fail 'standalone Exporter sample count mismatch'
  grep -q '^gopulse_redis_up 1$' <<<"$metrics" || fail 'standalone Exporter did not report Redis up'
  compose stop redis
  deadline=$((SECONDS + 20))
  while ((SECONDS < deadline)); do
    metrics=$(read_exporter_metrics "$id" || true)
    grep -q '^gopulse_redis_up 0$' <<<"$metrics" && break
    sleep 1
  done
  grep -q '^gopulse_redis_up 0$' <<<"$metrics" || fail 'standalone Exporter target failure did not report up 0'
  compose start redis
  wait_healthy redis
  deadline=$((SECONDS + 20))
  while ((SECONDS < deadline)); do
    metrics=$(read_exporter_metrics "$id" || true)
    grep -q '^gopulse_redis_up 1$' <<<"$metrics" && break
    sleep 1
  done
  grep -q '^gopulse_redis_up 1$' <<<"$metrics" || fail 'standalone Exporter did not recover without restart'
  compose stop --timeout 15 redis-exporter
  exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$id")
  [[ $exit_code == 0 ]] || fail "standalone Exporter SIGTERM exit=$exit_code"
  auth_id=$(compose --profile exporter run --detach --no-deps -e REDIS_PASSWORD=wrong-$TOKEN redis-exporter)
  sleep 2
  auth_metrics=$(read_exporter_metrics "$auth_id" || true)
  grep -q '^gopulse_redis_up 0$' <<<"$auth_metrics" || fail 'standalone Exporter authentication failure did not report up 0'
  docker container stop --time 15 "$auth_id" >/dev/null
  docker container remove "$auth_id" >/dev/null
  pass 'Standalone Redis Exporter real target, failure, recovery, authentication, and SIGTERM matrix passed.'
}

reset_for_management() {
  assert_project_ownership
  compose --profile exporter down --volumes --remove-orphans
  RESOURCES_STARTED=0
  assert_project_absent
  printf 'MONITOR_BOOTSTRAP_PACKAGE=\n' >>"$ENV_FILE"
  RESOURCES_STARTED=1
  compose up --detach --wait --wait-timeout 420
  assert_full_state
  register_and_promote
  run_scenario manage
  pass 'Administrator completed install, stop, start, update, Metrics, and Events through the browser.'
}

snapshot_existing_resources
assert_project_absent
info "Building isolated GoPulse $VERSION images for $PROJECT_NAME."
compose build backend business-worker search-indexer frontend acceptance router marshaller monitor redis-exporter
RESOURCES_STARTED=1
if ! compose up --detach --wait --wait-timeout 420; then
  compose ps --all >&2 || true
  compose logs --tail 180 >&2 || true
  fail 'cold complete Compose startup failed'
fi
assert_full_state
assert_image_contracts
assert_network_and_ports
assert_bootstrap_status
compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
register_and_promote
run_scenario ordinary
run_scenario admin
exercise_failure victoriametrics vm-down
exercise_failure monitor monitor-down
exercise_failure router transport-down
replace_service monitor
assert_bootstrap_status
replace_service marshaller
replace_service victoriametrics
replace_service kafka
compose up --detach kafka-init
replace_service elasticsearch
run_scenario persistence
exercise_persistence
exercise_standalone_exporter
reset_for_management
assert_project_ownership
pass 'Phase-12-02 complete observability container acceptance passed.'
