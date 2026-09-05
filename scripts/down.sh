#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
PROJECT_NAME=${COMPOSE_PROJECT_NAME:-gopulse}
ENV_FILE=${GOPULSE_ENV_FILE:-$REPO_ROOT/.env}
REMOVE_VOLUMES=0
CONFIRM_PROJECT=

info() { printf '[gopulse] %s\n' "$*"; }
fail() { printf '[gopulse] ERROR: %s\n' "$*" >&2; return 1; }
usage() {
  cat <<'USAGE'
Usage: scripts/down.sh [--project-name NAME] [--env-file PATH]
                       [--volumes --confirm-project NAME]

Stops only the label-verified Compose project. Named volumes are preserved by
default. Volume deletion requires both explicit flags and an exact project-name
confirmation.
USAGE
}

while (($#)); do
  case $1 in
    --project-name) PROJECT_NAME=${2:?missing project name}; shift 2 ;;
    --env-file) ENV_FILE=${2:?missing environment file}; shift 2 ;;
    --volumes) REMOVE_VOLUMES=1; shift ;;
    --confirm-project) CONFIRM_PROJECT=${2:?missing confirmation}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; usage >&2; exit 2 ;;
  esac
done

[[ $PROJECT_NAME =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "unsafe project name"
if ((REMOVE_VOLUMES)); then
  [[ $CONFIRM_PROJECT == "$PROJECT_NAME" ]] || fail "--volumes requires --confirm-project $PROJECT_NAME"
fi
[[ -f $ENV_FILE ]] || ENV_FILE="$REPO_ROOT/.env.example"
[[ -f $REPO_ROOT/VERSION ]] || fail "VERSION is missing"
export GOPULSE_VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
export GOPULSE_REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || printf unknown)
command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "Docker Engine is unavailable"

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

found=0
while IFS= read -r id; do
  [[ -n $id ]] || continue
  found=1
  project=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")
  working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
  config_files=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$id")
  [[ $project == "$PROJECT_NAME" && $working_dir == "$COMPOSE_WORKDIR" && $config_files == *"$COMPOSE_FILE"* ]] || {
    fail "container $id failed project ownership validation"
    exit 1
  }
done < <(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME")

for kind in network volume; do
  while IFS= read -r id; do
    [[ -n $id ]] || continue
    found=1
    label=$(docker "$kind" inspect --format '{{index .Labels "com.docker.compose.project"}}' "$id")
    [[ $label == "$PROJECT_NAME" ]] || { fail "$kind $id failed project ownership validation"; exit 1; }
  done < <(docker "$kind" ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME")
done

if ((found == 0)); then
  info "Project $PROJECT_NAME has no Compose resources; nothing to stop."
  exit 0
fi

args=(down --remove-orphans)
((REMOVE_VOLUMES)) && args+=(--volumes)
compose "${args[@]}"
if ((REMOVE_VOLUMES)); then
  info "Stopped $PROJECT_NAME and removed its verified named volumes."
else
  info "Stopped $PROJECT_NAME; named volumes were preserved."
fi
