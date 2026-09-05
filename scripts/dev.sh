#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
COMPOSE_FILE="$REPO_ROOT/deploy/compose.yaml"
COMPOSE_WORKDIR=$(cd -- "$(dirname -- "$COMPOSE_FILE")" && pwd -P)
PROJECT_NAME=${COMPOSE_PROJECT_NAME:-gopulse}
ENV_FILE=${GOPULSE_ENV_FILE:-$REPO_ROOT/.env}
BUILD=1

info() { printf '[gopulse] %s\n' "$*"; }
fail() { printf '[gopulse] ERROR: %s\n' "$*" >&2; return 1; }
usage() {
  cat <<'USAGE'
Usage: scripts/dev.sh [--project-name NAME] [--env-file PATH] [--no-build]

Build and start the container-native GoPulse business stack. The host needs only
Git, Docker Engine, Docker Compose, Bash, and ordinary POSIX utilities; Go,
Node.js, npm, curl, and Python are not used by the lifecycle.
USAGE
}

while (($#)); do
  case $1 in
    --project-name) PROJECT_NAME=${2:?missing project name}; shift 2 ;;
    --env-file) ENV_FILE=${2:?missing environment file}; shift 2 ;;
    --no-build) BUILD=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1"; usage >&2; exit 2 ;;
  esac
done

[[ $PROJECT_NAME =~ ^[a-z0-9][a-z0-9_-]{0,62}$ ]] || fail "project name must match ^[a-z0-9][a-z0-9_-]{0,62}$"
[[ -f $REPO_ROOT/VERSION ]] || fail "VERSION is missing"
VERSION=$(tr -d '[:space:]' <"$REPO_ROOT/VERSION")
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "VERSION must use major.minor.patch"

if [[ ! -f $ENV_FILE ]]; then
  [[ $ENV_FILE == "$REPO_ROOT/.env" ]] || fail "environment file does not exist: $ENV_FILE"
  cp "$REPO_ROOT/.env.example" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  info "Created $ENV_FILE from .env.example; review the development-only credentials."
fi

command -v git >/dev/null 2>&1 || fail "git is required to label images with their revision"
command -v docker >/dev/null 2>&1 || fail "docker is required"
docker info >/dev/null 2>&1 || fail "Docker Engine is unavailable"
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is unavailable"

TOPLEVEL=$(git -C "$REPO_ROOT" rev-parse --show-toplevel)
[[ $(cd -- "$TOPLEVEL" && pwd -P) == "$REPO_ROOT" ]] || fail "script must run from the GoPulse repository"
REVISION=$(git -C "$REPO_ROOT" rev-parse HEAD)
BRANCH=$(git -C "$REPO_ROOT" branch --show-current)
if [[ $BRANCH == develop/* && $BRANCH != "develop/$VERSION" ]]; then
  fail "branch $BRANCH does not match VERSION $VERSION"
fi

export GOPULSE_VERSION=$VERSION GOPULSE_REVISION=$REVISION
compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" --file "$COMPOSE_FILE" "$@"
}

existing=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT_NAME")
if [[ -n $existing ]]; then
  while IFS= read -r id; do
    [[ -n $id ]] || continue
    working_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")
    [[ $working_dir == "$COMPOSE_WORKDIR" ]] || { fail "project $PROJECT_NAME contains a container owned by another workspace"; exit 1; }
  done <<<"$existing"
fi

if ((BUILD)); then
  info "Building GoPulse $VERSION images at revision ${REVISION:0:12}."
  compose build backend business-worker search-indexer frontend acceptance
fi

info "Starting project $PROJECT_NAME with persistent project-scoped volumes."
if ! compose up --detach --wait --wait-timeout 300 frontend backend business-worker search-indexer; then
  compose ps >&2 || true
  compose logs --tail 120 >&2 || true
  fail "Compose startup failed; containers and volumes were retained for diagnosis"
fi

"$SCRIPT_DIR/verify.sh" --project-name "$PROJECT_NAME" --env-file "$ENV_FILE"
FRONTEND_PORT=$(compose port frontend 8080 | sed 's/.*://')
BACKEND_PORT=$(compose port backend 8080 | sed 's/.*://')
info "Frontend: http://127.0.0.1:$FRONTEND_PORT"
info "Backend:  http://127.0.0.1:$BACKEND_PORT"
info "Stop containers and networks with scripts/down.sh; named volumes are preserved by default."
