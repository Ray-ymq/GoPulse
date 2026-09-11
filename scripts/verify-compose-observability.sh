#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
KEEP=0
PHASE13=0
PHASE14=0
RESOURCES_STARTED=0
TEMP_DIR=
ENV_FILE=
SNAPSHOT_DIR=
SNAPSHOT_READY=0
IMAGE_TAG=
PRODUCT_IMAGES=(backend business-worker search-indexer frontend acceptance router marshaller monitor redis-exporter)

info() { printf '[gopulse-compose] %s\n' "$*"; }
pass() { printf '[gopulse-compose] PASS: %s\n' "$*"; }
fail() { printf '[gopulse-compose] ERROR: %s\n' "$*" >&2; return 1; }
usage() { printf 'Internal full-stack runner. Use scripts/verify-compose.sh [--keep].\n'; }

while (($#)); do
  case $1 in
    --keep) KEEP=1; shift ;;
    --phase14) PHASE14=1; shift ;;
    --phase13) PHASE13=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; exit 2 ;;
  esac
done

# Resolve the small host utility allow-list before creating a PATH that cannot
# expose Go, Node.js, npm, database clients, or project language tooling.
HOST_UTILITIES=(docker git sha256sum tr cut cat chmod sort comm cmp sed wc find sleep grep awk)
for utility in "${HOST_UTILITIES[@]}"; do
  command -v "$utility" >/dev/null 2>&1 || fail "$utility is required"
done

is_allowed_non_build_dirty_path() {
  local path=$1
  [[ $path == dev/*.md ]] && grep -Eq '^dev/?$' "$REPO_ROOT/.dockerignore"
}

assert_rebuildable_source() {
  local path unsafe=0
  while IFS= read -r -d '' path; do
    if ! is_allowed_non_build_dirty_path "$path"; then
      printf '[gopulse-compose] dirty build or runtime source: %s\n' "$path" >&2
      unsafe=1
    fi
  done < <(
    git -C "$REPO_ROOT" diff --name-only -z HEAD --
    git -C "$REPO_ROOT" ls-files --others --exclude-standard -z
  )
  ((unsafe == 0)) || fail 'authoritative acceptance requires clean build and runtime source before Docker access'
}

# The image revision must identify every file that can affect the built image or
# the acceptance behavior. Markdown under dev/ is the sole allowed dirty class
# and is explicitly excluded by the root .dockerignore.
assert_rebuildable_source

docker info >/dev/null 2>&1 || fail 'Docker Engine is unavailable'
docker compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is unavailable'
VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'VERSION must use major.minor.patch'
IFS=. read -r VERSION_MAJOR VERSION_MINOR VERSION_PATCH <<<"$VERSION"
UPDATE_VERSION="$VERSION_MAJOR.$VERSION_MINOR.$((VERSION_PATCH + 1))"
REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD)
TOKEN=$(tr -d '-' </proc/sys/kernel/random/uuid | cut -c1-12)
PROJECT_NAME="gopulse-accept-$TOKEN"
IMAGE_TAG="${VERSION}-accept-${TOKEN}"
[[ $PROJECT_NAME =~ ^gopulse-accept-[a-f0-9]{12}$ ]] || fail 'generated project name is invalid'
[[ $IMAGE_TAG =~ ^[0-9]+\.[0-9]+\.[0-9]+-accept-[a-f0-9]{12}$ ]] || fail 'generated image tag is invalid'
TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gopulse-compose-$TOKEN.XXXXXX")
ENV_FILE="$TEMP_DIR/acceptance.env"
SNAPSHOT_DIR="$TEMP_DIR/snapshot"
HOST_BIN="$TEMP_DIR/host-bin"
early_cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n $TEMP_DIR && $TEMP_DIR == "${TMPDIR:-/tmp}"/gopulse-compose-* ]]; then
    find "$TEMP_DIR" -depth -delete 2>/dev/null || status=1
  fi
  exit "$status"
}
trap early_cleanup EXIT
mkdir -p "$SNAPSHOT_DIR" "$HOST_BIN"
for utility in "${HOST_UTILITIES[@]}"; do
  ln -s "$(command -v "$utility")" "$HOST_BIN/$utility"
done
PATH=$HOST_BIN
export PATH
hash -r
for runtime in go node npm python python3 mysql redis-cli rabbitmqctl kafka-topics.sh curl; do
  ! command -v "$runtime" >/dev/null 2>&1 || fail "host runtime/client unexpectedly available in acceptance PATH: $runtime"
done
ADMIN_USERNAME="admin_$TOKEN"
USER_USERNAME="user_$TOKEN"
PASSWORD="Acceptance-$TOKEN-password"
export GOPULSE_VERSION=$VERSION GOPULSE_REVISION=$REVISION GOPULSE_IMAGE_TAG=$IMAGE_TAG GOPULSE_UPDATE_VERSION=$UPDATE_VERSION GOPULSE_ACCEPTANCE_TOKEN=$TOKEN

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
BACKEND_METRICS_TOKEN=metrics-backend-$TOKEN-0123456789abcdef0123456789
BUSINESS_WORKER_METRICS_TOKEN=metrics-business-worker-$TOKEN-0123456789abcdef0123456789
SEARCH_INDEXER_METRICS_TOKEN=metrics-search-indexer-$TOKEN-0123456789abcdef0123456789
MONITOR_METRICS_TOKEN=metrics-monitor-$TOKEN-0123456789abcdef0123456789
ROUTER_METRICS_TOKEN=metrics-router-$TOKEN-0123456789abcdef0123456789
MARSHALLER_METRICS_TOKEN=metrics-marshaller-$TOKEN-0123456789abcdef0123456789
LOG_MONITOR_INGEST_TOKEN=logs-$TOKEN-0123456789abcdef0123456789ab
ROUTER_API_TOKEN=router-$TOKEN-0123456789abcdef0123456789
MARSHALLER_API_TOKEN=marshaller-$TOKEN-0123456789abcdef012345
VICTORIAMETRICS_USERNAME=vm_$TOKEN
VICTORIAMETRICS_PASSWORD=vm-$TOKEN-0123456789abcdef0123456789abc
GOPULSE_VERSION=$VERSION
GOPULSE_REVISION=$REVISION
GOPULSE_IMAGE_TAG=$IMAGE_TAG
GOPULSE_UPDATE_VERSION=$UPDATE_VERSION
GOPULSE_ACCEPTANCE_TOKEN=$TOKEN
GOPULSE_OBSERVABILITY_ADMIN_USERNAME=$ADMIN_USERNAME
GOPULSE_OBSERVABILITY_USER_USERNAME=$USER_USERNAME
GOPULSE_OBSERVABILITY_PASSWORD=$PASSWORD
ENV
chmod 600 "$ENV_FILE"

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

snapshot_existing_resources() {
  local service ref
  git -C "$REPO_ROOT" status --porcelain=v1 --untracked-files=all -z >"$SNAPSHOT_DIR/git-status"
  docker ps -aq | sort >"$SNAPSHOT_DIR/containers"
  docker network ls -q | sort >"$SNAPSHOT_DIR/networks"
  docker volume ls -q | sort >"$SNAPSHOT_DIR/volumes"
  docker image ls -q --no-trunc | sort -u >"$SNAPSHOT_DIR/images"
  docker image ls --no-trunc --format '{{.Repository}}:{{.Tag}}|{{.ID}}' | awk -F '|' '$1 != "<none>:<none>"' | sort >"$SNAPSHOT_DIR/image-tags"
  for service in "${PRODUCT_IMAGES[@]}"; do
    ref="gopulse/$service:$IMAGE_TAG"
    ! docker image inspect "$ref" >/dev/null 2>&1 || fail "refusing to replace pre-existing acceptance image tag: $ref"
  done
  SNAPSHOT_READY=1
}

assert_snapshot_preserved() {
  local kind id ref expected_id actual_id
  cmp -s "$SNAPSHOT_DIR/git-status" <(git -C "$REPO_ROOT" status --porcelain=v1 --untracked-files=all -z) || fail 'acceptance changed the Git working tree'
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
  while IFS='|' read -r ref expected_id; do
    [[ -n $ref && -n $expected_id ]] || continue
    actual_id=$(docker image inspect --format '{{.Id}}' "$ref" 2>/dev/null || true)
    [[ $actual_id == "$expected_id" ]] || fail "pre-existing image tag mapping changed: $ref"
  done <"$SNAPSHOT_DIR/image-tags"
}

cleanup_acceptance_images() {
  local service ref
  for service in "${PRODUCT_IMAGES[@]}"; do
    ref="gopulse/$service:$IMAGE_TAG"
    if docker image inspect "$ref" >/dev/null 2>&1; then
      docker image rm "$ref" >/dev/null || return 1
    fi
  done
}

assert_project_absent() {
  [[ -z $(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has containers'
  [[ -z $(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has networks'
  [[ -z $(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME") ]] || fail 'acceptance project already has volumes'
}

assert_project_ownership() {
  local id label working_dir config_files
  [[ $PROJECT_NAME =~ ^gopulse-accept-[a-f0-9]{12}$ ]] || fail 'unsafe project at cleanup boundary'
  while IFS= read -r id; do
    [[ -n $id ]] || continue
    label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
    working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
    config_files=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")
    [[ $label == "$PROJECT_NAME" && $working_dir == "$COMPOSE_WORKDIR" && $config_files == *"$COMPOSE_FILE"* ]] || fail "container ownership mismatch: $id"
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
  if ((SNAPSHOT_READY)); then
    cleanup_acceptance_images || status=1
    assert_snapshot_preserved || status=1
  fi
  if [[ -n $TEMP_DIR && $TEMP_DIR == "${TMPDIR:-/tmp}"/gopulse-compose-* ]]; then
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
  local service=$1 id config_files image image_version image_revision
  id=$(service_id "$service")
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id") == "$PROJECT_NAME" ]] || fail "$service project label mismatch"
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id") == "$service" ]] || fail "$service label mismatch"
  [[ $(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id") == "$COMPOSE_WORKDIR" ]] || fail "$service working directory mismatch"
  config_files=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")
  [[ $config_files == *"$COMPOSE_FILE"* ]] || fail "$service config-file label mismatch"
  case $service in
    frontend|backend|business-worker|search-indexer|router|marshaller|monitor|redis-exporter)
      image=$(docker inspect --format '{{.Image}}' "$id")
      image_version=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image")
      image_revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
      [[ $image_version == "$VERSION" && $image_revision == "$REVISION" ]] || fail "$service image ownership mismatch"
      ;;
  esac
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
  local service ref container_id running_image tagged_image user version revision source entrypoint cmd arch daemon_arch stop_signal layers
  local readonly privileged caps binds mounts network_mode pid_mode ipc_mode image_env package_digest image_digest expected_entry expected_cmd expected_signal
  daemon_arch=$(docker info --format '{{.Architecture}}')
  case $daemon_arch in
    x86_64) daemon_arch=amd64 ;;
    aarch64) daemon_arch=arm64 ;;
  esac
  for service in frontend backend business-worker search-indexer router marshaller monitor redis-exporter; do
    ref="gopulse/$service:$IMAGE_TAG"
    tagged_image=$(docker image inspect --format '{{.Id}}' "$ref")
    if [[ $service != redis-exporter ]]; then
      container_id=$(owned_service_id "$service")
      running_image=$(docker inspect --format '{{.Image}}' "$container_id")
      [[ $running_image == "$tagged_image" ]] || fail "$service container does not run the freshly built tag"
    fi
    user=$(docker image inspect --format '{{.Config.User}}' "$ref")
    version=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$ref")
    revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$ref")
    source=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.source"}}' "$ref")
    entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "$ref")
    cmd=$(docker image inspect --format '{{json .Config.Cmd}}' "$ref")
    arch=$(docker image inspect --format '{{.Architecture}}' "$ref")
    stop_signal=$(docker image inspect --format '{{.Config.StopSignal}}' "$ref")
    layers=$(docker image inspect --format '{{len .RootFS.Layers}}' "$ref")
    image_env=$(docker image inspect --format '{{json .Config.Env}}' "$ref")
    case $service in
      frontend) expected_entry='["nginx"]'; expected_cmd='["-g","daemon off;"]'; expected_signal=SIGQUIT ;;
      backend) expected_entry='["/usr/local/bin/server"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      business-worker) expected_entry='["/usr/local/bin/business-worker"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      search-indexer) expected_entry='["/usr/local/bin/search-indexer"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      router) expected_entry='["/usr/local/bin/router"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      marshaller) expected_entry='["/usr/local/bin/marshaller"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      monitor) expected_entry='["/usr/local/bin/monitor"]'; expected_cmd=null; expected_signal=SIGTERM ;;
      redis-exporter) expected_entry='["/usr/local/bin/gopulse-redis-exporter"]'; expected_cmd=null; expected_signal=SIGTERM ;;
    esac
    [[ $user =~ ^[0-9]+:[0-9]+$ ]] || fail "$service image does not use a numeric uid:gid"
    [[ $version == "$VERSION" && $revision == "$REVISION" && $source == https://github.com/Ray-ymq/GoPulse ]] || fail "$service OCI labels mismatch"
    [[ $entrypoint == "$expected_entry" && $cmd == "$expected_cmd" && $stop_signal == "$expected_signal" ]] || fail "$service process contract mismatch"
    [[ $arch == "$daemon_arch" && $layers -gt 0 ]] || fail "$service architecture/layer contract mismatch"
    if grep -Eq '(AUTH_JWT_SECRET|MONITOR_API_TOKEN|LOG_MONITOR_INGEST_TOKEN|ROUTER_API_TOKEN|MARSHALLER_API_TOKEN|MYSQL_PASSWORD|RABBITMQ_PASSWORD|VICTORIAMETRICS_PASSWORD)=' <<<"$image_env"; then
      fail "$service image config contains a runtime credential"
    fi
    if docker history --no-trunc "$ref" | grep -E '(AUTH_JWT_SECRET|MONITOR_API_TOKEN|LOG_MONITOR_INGEST_TOKEN|ROUTER_API_TOKEN|MARSHALLER_API_TOKEN|MYSQL_PASSWORD|RABBITMQ_PASSWORD|VICTORIAMETRICS_PASSWORD)=' >/dev/null; then
      fail "$service image history contains a runtime credential"
    fi
  done

  docker run --rm --entrypoint /bin/sh "gopulse/backend:$IMAGE_TAG" -ec \
    'test -x /usr/local/bin/server && test -x /usr/local/bin/migrate && test -x /usr/local/bin/search-reindex && test -x /usr/local/bin/admin-role && ! command -v go && ! command -v node && test ! -d /src'
  for service in business-worker search-indexer router marshaller monitor redis-exporter; do
    docker run --rm --entrypoint /bin/sh "gopulse/$service:$IMAGE_TAG" -ec '! command -v go && ! command -v node && ! command -v npm && test ! -d /src'
  done
  docker run --rm --entrypoint /bin/sh "gopulse/frontend:$IMAGE_TAG" -ec \
    '! command -v go && ! command -v node && ! command -v npm && test ! -d /src && ! find /usr/share/nginx/html -name "*.map" -print -quit | grep -q . && ! grep -R -E "(mysql|redis|rabbitmq|elasticsearch|kafka|victoriametrics|monitor|router|marshaller):[0-9]+|AUTH_JWT_SECRET|MONITOR_API_TOKEN|LOG_MONITOR_INGEST_TOKEN|ROUTER_API_TOKEN|MARSHALLER_API_TOKEN" /usr/share/nginx/html'

  for service in frontend backend business-worker search-indexer router marshaller monitor; do
    container_id=$(owned_service_id "$service")
    readonly=$(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$container_id")
    privileged=$(docker inspect --format '{{.HostConfig.Privileged}}' "$container_id")
    caps=$(docker inspect --format '{{json .HostConfig.CapAdd}}' "$container_id")
    binds=$(docker inspect --format '{{json .HostConfig.Binds}}' "$container_id")
    mounts=$(docker inspect --format '{{range .Mounts}}{{.Type}}|{{.Name}}|{{.Destination}};{{end}}' "$container_id")
    network_mode=$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$container_id")
    pid_mode=$(docker inspect --format '{{.HostConfig.PidMode}}' "$container_id")
    ipc_mode=$(docker inspect --format '{{.HostConfig.IpcMode}}' "$container_id")
    [[ $privileged == false && $caps == null && $binds != *docker.sock* && $network_mode != host && $pid_mode != host && $ipc_mode != host ]] || fail "$service container privilege/namespace boundary mismatch"
    if [[ $service == monitor ]]; then
      [[ $mounts == "volume|${PROJECT_NAME}_monitor_plugin_data|/var/lib/gopulse-monitor/plugins;" ]] || fail 'Monitor mount contract mismatch'
    else
      [[ -z $mounts ]] || fail "$service unexpectedly mounts host or volume content"
    fi
    if [[ $service == router || $service == marshaller || $service == monitor ]]; then
      [[ $readonly == true ]] || fail "$service root filesystem must be read-only"
    fi
  done

  package_digest=$(docker run --rm --entrypoint /bin/sh "gopulse/monitor:$IMAGE_TAG" -ec 'tar -xOzf /opt/gopulse/packages/gopulse-redis-exporter.tar.gz bin/gopulse-redis-exporter' | sha256sum | awk '{print $1}')
  image_digest=$(docker run --rm --entrypoint sha256sum "gopulse/redis-exporter:$IMAGE_TAG" /usr/local/bin/gopulse-redis-exporter | awk '{print $1}')
  [[ $package_digest == "$image_digest" ]] || fail 'Monitor package and Redis Exporter image binary digest differ'
  pass 'Image tags, OCI metadata, architecture, non-root runtime contents, signals, and privilege boundaries passed.'
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

assert_internal_security() {
  if compose exec -T frontend /bin/sh -ec 'wget --quiet --timeout=3 --output-document=/dev/null http://monitor:9090/health'; then
    fail 'Frontend unexpectedly reached the internal Monitor service'
  fi
  compose exec -T \
    -e "GOPULSE_TEST_ROUTER_TOKEN=router-$TOKEN-0123456789abcdef0123456789" \
    -e "GOPULSE_TEST_MARSHALLER_TOKEN=marshaller-$TOKEN-0123456789abcdef012345" \
    backend /bin/sh -ec '
      http_code() {
        output=$(wget -S -O /dev/null "$@" 2>&1 || true)
        printf "%s\n" "$output" | awk "/HTTP\\// { for (i = 1; i <= NF; i++) if (\$i ~ /^HTTP\\//) code=\$(i + 1) } END { print code }"
      }
      expect_code() {
        expected=$1
        shift
        actual=$(http_code "$@")
        test "$actual" = "$expected" || { printf "expected HTTP %s, got %s\n" "$expected" "$actual" >&2; return 1; }
      }
      expect_code 401 http://monitor:9090/ready
      expect_code 401 --header "Authorization: Bearer wrong-monitor-token" http://monitor:9090/ready
      expect_code 401 --header "Cookie: gopulse_admin_session=not-an-internal-identity" http://monitor:9090/ready
      expect_code 401 http://router:9091/ready
      expect_code 401 --header "Authorization: Bearer wrong-router-token" http://router:9091/ready
      expect_code 401 --header "Cookie: gopulse_admin_session=not-an-internal-identity" http://router:9091/ready
      expect_code 401 http://marshaller:9093/ready
      expect_code 401 --header "Authorization: Bearer wrong-marshaller-token" http://marshaller:9093/ready
      expect_code 401 --header "Cookie: gopulse_admin_session=not-an-internal-identity" http://marshaller:9093/ready
      expect_code 401 http://victoriametrics:8428/api/v1/query?query=up
      expect_code 401 --header "Authorization: Basic $(printf wrong:wrong | base64 | tr -d "\n")" http://victoriametrics:8428/api/v1/query?query=up
      expect_code 200 --header "Authorization: Bearer $MONITOR_API_TOKEN" http://monitor:9090/ready
      expect_code 200 --header "Authorization: Bearer $GOPULSE_TEST_ROUTER_TOKEN" http://router:9091/ready
      expect_code 200 --header "Authorization: Bearer $GOPULSE_TEST_MARSHALLER_TOKEN" http://marshaller:9093/ready
      expect_code 200 --header "Authorization: Basic $(printf "%s:%s" "$BACKEND_VICTORIAMETRICS_USERNAME" "$BACKEND_VICTORIAMETRICS_PASSWORD" | base64 | tr -d "\n")" http://victoriametrics:8428/api/v1/query?query=up
    '
  pass 'Frontend isolation plus Bearer, Basic, and cookie trust boundaries passed.'
}

assert_bootstrap_status() {
  compose exec -T backend /bin/sh -ec '
    status=$(wget --quiet --header "Authorization: Bearer $MONITOR_API_TOKEN" --output-document=- http://monitor:9090/internal/v1/exporter-plugins/redis-exporter)
    printf "%s" "$status" | grep -q "\"version\":\"'"$VERSION"'\""
    printf "%s" "$status" | grep -q "\"desired_state\":\"running\""
    printf "%s" "$status" | grep -q "\"observed_state\":\"running\""
  '
  compose exec -T monitor /bin/sh -ec '
    count=0
    for executable in /proc/[0-9]*/exe; do
      target=$(readlink "$executable" 2>/dev/null || true)
      case $target in */gopulse-redis-exporter) count=$((count + 1)) ;; esac
    done
    test "$count" -eq 1
  '
}

run_observability_scenario() {
  local scenario=$1
  info "Running observability browser scenario: $scenario"
  compose --profile acceptance run --rm --no-deps -e "GOPULSE_ACCEPTANCE_SCENARIO=$scenario" acceptance e2e/compose-observability.spec.ts
}

run_business_scenario() {
  local scenario=$1
  info "Running business browser scenario: $scenario"
  compose --profile acceptance run --rm --no-deps -e "GOPULSE_ACCEPTANCE_SCENARIO=$scenario" acceptance e2e/compose-business.spec.ts
}

rerun_initializers() {
  compose run --rm --no-deps migrate
  compose run --rm --no-deps search-init
  compose run --rm --no-deps kafka-init
  pass 'Migration, search, and Kafka Topic initialization are idempotent.'
}

exercise_redis_fallback() {
  owned_service_id redis >/dev/null
  compose stop redis
  run_business_scenario redis-fallback
  compose start redis
  wait_healthy redis
  pass 'MySQL-backed social reads and writes survived Redis unavailability.'
}

exercise_worker_recovery() {
  owned_service_id business-worker >/dev/null
  compose pause business-worker
  run_business_scenario worker-seed
  compose unpause business-worker
  wait_running business-worker
  run_business_scenario worker-verify
  pass 'Durable notification events converged after Business Worker recovery.'
}

exercise_indexer_recovery() {
  owned_service_id search-indexer >/dev/null
  compose pause search-indexer
  run_business_scenario indexer-seed
  compose unpause search-indexer
  wait_running search-indexer
  run_business_scenario indexer-verify
  pass 'Durable search events converged after Search Indexer recovery.'
}

read_exporter_metrics() {
  local id=$1
  docker exec "$id" /bin/sh -ec \
    'printf "GET /metrics HTTP/1.0\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n" | nc -w 5 127.0.0.1 9121' |
    awk '{ sub(/\r$/, ""); if (body) print; else if ($0 == "") body=1 }'
}

register_and_promote() {
  run_observability_scenario setup
  local bootstrap_id
  bootstrap_id=$(compose exec -T mysql sh -ec 'MYSQL_PWD="$MYSQL_PASSWORD" mysql --user="$MYSQL_USER" --batch --skip-column-names "$MYSQL_DATABASE" --execute "$1"' probe "SELECT id FROM users WHERE username='$ADMIN_USERNAME'")
  [[ $bootstrap_id =~ ^[1-9][0-9]*$ ]] || fail 'Bootstrap user ID is invalid.'
  compose --profile operations run --rm --no-deps admin-role bootstrap --user-id "$bootstrap_id"
}

exercise_failure() {
  local service=$1 scenario=$2
  owned_service_id "$service" >/dev/null
  compose stop "$service"
  run_observability_scenario "$scenario"
  compose start "$service"
  wait_healthy "$service"
  if [[ $service == monitor ]]; then assert_bootstrap_status; fi
  pass "$service failure remained localized and recovered."
}

replace_service() {
  local service=$1 before after
  before=$(owned_service_id "$service")
  compose up --detach --force-recreate --no-deps "$service"
  case $service in
    business-worker|search-indexer) wait_running "$service" ;;
    *) wait_healthy "$service" ;;
  esac
  after=$(owned_service_id "$service")
  [[ $before != "$after" ]] || fail "$service container was not replaced"
  if [[ $service == monitor ]]; then assert_bootstrap_status; fi
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
  rerun_initializers
  assert_bootstrap_status
  run_business_scenario persistence
  run_observability_scenario persistence
  run_observability_scenario post-restart
  pass 'Full project down/up retained facts and accepted new social and observability activity.'
}

exercise_signal_shutdown() {
  local service id exit_code
  for service in frontend backend business-worker search-indexer router marshaller monitor; do
    id=$(owned_service_id "$service")
    compose stop --timeout 25 "$service"
    exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$id")
    [[ $exit_code == 0 ]] || fail "$service did not stop cleanly after its configured signal (exit $exit_code)"
    compose start "$service"
    case $service in
      business-worker|search-indexer) wait_running "$service" ;;
      *) wait_healthy "$service" ;;
    esac
    if [[ $service == monitor ]]; then assert_bootstrap_status; fi
  done
  compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
  pass 'All long-running self-built services completed bounded signal shutdown and restart.'
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
  run_observability_scenario manage
  pass 'Administrator completed official install, stop, start, Metrics, and Events through the browser.'
}

snapshot_existing_resources
assert_project_absent
info "Building isolated GoPulse $VERSION images with unique tag $IMAGE_TAG for $PROJECT_NAME without host Go/Node runtimes."
compose build backend business-worker search-indexer frontend acceptance router marshaller monitor redis-exporter
RESOURCES_STARTED=1
if ! compose up --detach --wait --wait-timeout 420; then
  compose ps --all >&2 || true
  compose logs --tail 180 >&2 || true
  fail 'cold complete Compose startup failed'
fi
assert_full_state
if ((PHASE13 == 1 || PHASE14 == 1)); then
  register_and_promote
  phase13_specs=(e2e/delete.spec.ts e2e/profile.spec.ts e2e/follow.spec.ts e2e/bookmark.spec.ts e2e/edit.spec.ts)
  if ((PHASE14 == 0)); then phase13_specs+=(e2e/compose-business.spec.ts); fi
  compose --profile acceptance run --rm --no-deps \
    -e "GOPULSE_PROFILE_ADMIN_USER=$ADMIN_USERNAME" \
    -e "GOPULSE_PROFILE_ADMIN_PASSWORD=$PASSWORD" \
    acceptance "${phase13_specs[@]}"
  if ((PHASE14 == 0)); then run_observability_scenario admin; fi
  assert_project_ownership
  pass 'Phase 13 Compose social closure and representative administrator regression passed.'
  if ((PHASE14 == 0)); then exit 0; fi
fi
assert_image_contracts
assert_network_and_ports
assert_internal_security
assert_bootstrap_status
rerun_initializers
compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
run_business_scenario business
exercise_redis_fallback
exercise_worker_recovery
exercise_indexer_recovery
if ((PHASE14 == 0)); then register_and_promote; fi
run_observability_scenario ordinary
run_observability_scenario admin
exercise_failure victoriametrics vm-down
exercise_failure monitor monitor-down
exercise_failure router transport-down
for service in backend business-worker search-indexer monitor marshaller; do
  replace_service "$service"
done
assert_bootstrap_status
replace_service redis
replace_service victoriametrics
replace_service kafka
compose up --detach kafka-init
replace_service elasticsearch
run_business_scenario persistence
run_observability_scenario persistence
exercise_signal_shutdown
exercise_persistence
exercise_standalone_exporter
reset_for_management
assert_project_ownership
pass 'Phase 12 authoritative full-stack Compose acceptance passed.'
