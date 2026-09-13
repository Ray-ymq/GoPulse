#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd -P)
REMOTE=origin
PUSH=0

usage() {
  cat <<'USAGE'
Usage: scripts/start-development-batch.sh Phase-XX-YY [--remote REMOTE] [--push]

Fetch the selected remote main, resolve the authoritative batch allocation, create
its develop/x.x.x branch, synchronize VERSION/.env/npm metadata, validate it, and
commit the bootstrap metadata. Untracked files are preserved and never staged.

--push also publishes the new branch after validating its name.
USAGE
}
fail() { printf '[gopulse-branch] ERROR: %s\n' "$*" >&2; exit 1; }
info() { printf '[gopulse-branch] %s\n' "$*"; }

[[ $# -ge 1 ]] || { usage >&2; exit 2; }
BATCH=$1
shift
while [[ $# -gt 0 ]]; do
  case $1 in
    --remote) [[ $# -ge 2 ]] || fail '--remote requires a remote name'; REMOTE=$2; shift 2 ;;
    --push) PUSH=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ $BATCH =~ ^Phase-[0-9]{2}-[0-9]{2}$ ]] || fail 'batch must match Phase-XX-YY'

cd "$REPO_ROOT"
[[ -z "$(git diff --name-only)" ]] || fail 'tracked working-tree changes exist; commit or stash them before starting a batch'
[[ -z "$(git diff --cached --name-only)" ]] || fail 'staged changes exist; commit or unstage them before starting a batch'
git remote get-url "$REMOTE" >/dev/null 2>&1 || fail "remote does not exist: $REMOTE"

info "Fetching $REMOTE/main"
git fetch --no-tags "$REMOTE" main
[[ -n "$(git rev-parse --verify "$REMOTE/main")" ]] || fail "cannot resolve $REMOTE/main"

allocation=$(
  PYTHONPATH="$REPO_ROOT/scripts/ci" python3 - "$REPO_ROOT" "$BATCH" <<'PY'
import json
import sys
from pathlib import Path
from validate_branch import load_allocations

repo = Path(sys.argv[1]).resolve()
batch = sys.argv[2]
matches = [item for item in load_allocations(repo) if item.batch == batch]
if len(matches) != 1:
    raise SystemExit(f"{batch} must map to exactly one authoritative allocation; found {len(matches)}")
item = matches[0]
print(json.dumps({"version": item.version, "branch": item.branch, "plan": str(item.plan)}))
PY
)
VERSION=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])' <<<"$allocation")
BRANCH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["branch"])' <<<"$allocation")
PLAN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["plan"])' <<<"$allocation")
[[ $BRANCH =~ ^develop/[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "allocation has unsafe branch: $BRANCH"
info "$BATCH => $VERSION => $BRANCH (from $PLAN)"

if git show-ref --verify --quiet "refs/heads/$BRANCH"; then
  fail "local branch already exists: $BRANCH; continue it instead of recreating it"
fi
if [[ -n "$(git ls-remote --heads "$REMOTE" "refs/heads/$BRANCH")" ]]; then
  fail "remote branch already exists: $REMOTE/$BRANCH; continue it instead of recreating it"
fi

git switch -c "$BRANCH" "$REMOTE/main"
python3 scripts/ci/sync_version_metadata.py --repo "$REPO_ROOT" --version "$VERSION"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$BRANCH" --base-ref "$REMOTE/main"

git add VERSION .env.example frontend/package.json frontend/package-lock.json admin-frontend/package.json admin-frontend/package-lock.json
if git diff --cached --quiet; then
  info 'Version metadata was already synchronized; no bootstrap commit was needed.'
else
  git commit -m "chore: initialize $BATCH version metadata"
fi

if (( PUSH )); then
  git push --set-upstream "$REMOTE" "$BRANCH"
else
  info "Branch ready. Push it with: git push --set-upstream $REMOTE $BRANCH"
fi
