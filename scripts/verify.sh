#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
PROJECT_NAME=${COMPOSE_PROJECT_NAME:-gopulse}
ENV_FILE=${GOPULSE_ENV_FILE:-$REPO_ROOT/.env}

info() { printf '[gopulse] %s\n' "$*"; }
pass() { printf '[gopulse] PASS: %s\n' "$*"; }
fail() { printf '[gopulse] ERROR: %s\n' "$*" >&2; return 1; }
usage() { printf 'Usage: scripts/verify.sh [--project-name NAME] [--env-file PATH]\n'; }

while (($#)); do
  case $1 in
    --project-name) PROJECT_NAME=${2:?missing project name}; shift 2 ;;
    --env-file) ENV_FILE=${2:?missing environment file}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; exit 2 ;;
  esac
done

[[ $PROJECT_NAME =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "unsafe project name"
[[ -f $ENV_FILE ]] || ENV_FILE="$REPO_ROOT/.env.example"
VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "VERSION must use major.minor.patch"
command -v git >/dev/null 2>&1 || fail "git is required"
REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD)
IMAGE_TAG=$VERSION
IMAGE_SOURCE=https://github.com/Ray-ymq/GoPulse
export GOPULSE_VERSION=$VERSION GOPULSE_REVISION=$REVISION GOPULSE_IMAGE_TAG=$IMAGE_TAG
command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "Docker Engine is unavailable"

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

service_id() {
  local service=$1 ids count
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter "label=com.docker.compose.service=$service")
  count=$(sed '/^$/d' <<<"$ids" | wc -l | tr -d ' ')
  [[ $count == 1 ]] || { fail "$service must have exactly one project container; found $count"; return 1; }
  printf '%s\n' "$ids"
}

verify_owned_service() {
  local service=$1 expected=$2 id project label working_dir state health
  id=$(service_id "$service")
  project=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
  label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$id")
  working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
  [[ $project == "$PROJECT_NAME" && $label == "$service" && $working_dir == "$COMPOSE_WORKDIR" ]] || { fail "$service ownership labels mismatch"; return 1; }
  state=$(docker inspect --format '{{.State.Status}}' "$id")
  [[ $state == "$expected" ]] || fail "$service state is $state, expected $expected"
  case $service in
    mysql|redis|rabbitmq|elasticsearch|kafka|victoriametrics|router|marshaller|monitor|backend|frontend)
      health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id")
      [[ $health == healthy ]] || fail "$service health is $health"
      ;;
  esac
  pass "$service is label-owned and $state"
}

for service in mysql redis rabbitmq elasticsearch kafka victoriametrics router marshaller monitor backend business-worker search-indexer frontend; do
  verify_owned_service "$service" running
done
for service in migrate search-init kafka-init; do
  id=$(service_id "$service")
  verify_owned_service "$service" exited
  [[ $(docker inspect --format '{{.State.ExitCode}}' "$id") == 0 ]] || fail "$service did not exit successfully"
  pass "$service completed successfully"
done

for service in mysql redis rabbitmq elasticsearch kafka victoriametrics router marshaller monitor business-worker search-indexer; do
  id=$(service_id "$service")
  bindings=$(docker inspect --format '{{json .HostConfig.PortBindings}}' "$id")
  [[ $bindings == null || $bindings == '{}' ]] || fail "$service unexpectedly publishes a host port"
done
for service in frontend backend; do
  id=$(service_id "$service")
  host_ips=$(docker inspect --format '{{range $p, $bindings := .HostConfig.PortBindings}}{{range $bindings}}{{.HostIp}} {{end}}{{end}}' "$id")
  [[ $host_ips == '127.0.0.1 ' ]] || fail "$service must publish exactly one loopback port"
done
pass 'Only Frontend and Backend publish loopback ports.'

verify_image_metadata() {
  local image_name=$1 ref metadata image_id user image_version image_revision image_source
  ref="gopulse/$image_name:$IMAGE_TAG"
  if ! metadata=$(docker image inspect --format '{{.Id}}|{{.Config.User}}|{{index .Config.Labels "org.opencontainers.image.version"}}|{{index .Config.Labels "org.opencontainers.image.revision"}}|{{index .Config.Labels "org.opencontainers.image.source"}}' "$ref" 2>/dev/null); then
    fail "$ref is missing"
  fi
  IFS='|' read -r image_id user image_version image_revision image_source <<<"$metadata"
  [[ -n $image_id ]] || fail "$ref image ID is missing"
  [[ $user =~ ^[0-9]+:[0-9]+$ ]] || fail "$ref user is not a numeric uid:gid"
  [[ $image_version == "$VERSION" ]] || fail "$ref version $image_version does not match $VERSION"
  [[ $image_revision == "$REVISION" ]] || fail "$ref revision $image_revision does not match $REVISION"
  [[ $image_source == "$IMAGE_SOURCE" ]] || fail "$ref source label mismatch"
  printf '%s\n' "$image_id"
}

for service in frontend backend business-worker search-indexer router marshaller monitor; do
  id=$(service_id "$service")
  running_image=$(docker inspect --format '{{.Image}}' "$id")
  tagged_image=$(verify_image_metadata "$service")
  [[ $running_image == "$tagged_image" ]] || fail "$service container image does not match gopulse/$service:$IMAGE_TAG"
done
for service in migrate search-init; do
  id=$(service_id "$service")
  running_image=$(docker inspect --format '{{.Image}}' "$id")
  tagged_image=$(verify_image_metadata backend)
  [[ $running_image == "$tagged_image" ]] || fail "$service container image does not match gopulse/backend:$IMAGE_TAG"
done
verify_image_metadata acceptance >/dev/null
verify_image_metadata redis-exporter >/dev/null
pass "Application image IDs and OCI version/revision/source labels match $VERSION at ${REVISION:0:12}."

compose exec -T backend /bin/sh -ec '
  status=$(wget --quiet --header "Authorization: Bearer $MONITOR_API_TOKEN" --output-document=- http://monitor:9090/internal/v1/exporter-plugins/redis-exporter)
  printf "%s" "$status" | grep -q "\"version\":\"'"$VERSION"'\""
  printf "%s" "$status" | grep -q "\"desired_state\":\"running\""
  printf "%s" "$status" | grep -q "\"observed_state\":\"running\""
'
pass 'Monitor restored the image-bundled Redis Exporter desired state.'
compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts
pass 'Containerized production SPA/API smoke passed.'
info 'Verification passed without changing persistent application state.'
