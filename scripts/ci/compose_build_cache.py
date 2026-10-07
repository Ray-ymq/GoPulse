#!/usr/bin/env python3
"""Build the existing Compose targets with optional GitHub Actions caches."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

TARGETS = (
    "backend", "business-worker", "search-indexer", "admin-frontend", "frontend",
    "acceptance", "router", "marshaller", "monitor", "redis-exporter",
)
PLATFORM = "linux/amd64"


def definition(repo: Path, env: dict[str, str], cached: bool) -> dict:
    result = subprocess.run(
        ["docker", "compose", "--env-file", str(repo / ".env.example"),
         "--file", str(repo / "deploy/compose.yaml"), "build", "--print", *TARGETS],
        cwd=repo, env=env, check=True, capture_output=True, text=True,
    )
    config = json.loads(result.stdout)
    targets = {}
    for name in TARGETS:
        target = dict(config["target"][name])
        target["platforms"] = [PLATFORM]
        target["output"] = ["type=docker"]
        if cached:
            scope = f"gopulse-{name}-linux-amd64-v1"
            target["cache-from"] = [f"type=gha,scope={scope},version=2,timeout=2m"]
            target["cache-to"] = [
                f"type=gha,scope={scope},version=2,mode=max,timeout=2m,ignore-error=true"
            ]
        targets[name] = target
    return {"group": {"default": {"targets": list(TARGETS)}}, "target": targets}


def cache_failure(output: str) -> bool:
    # A compiler/test failure must not become a second full build just because
    # an earlier progress line mentioned an optional cache lookup.
    errors = [line for line in output.splitlines() if "failed to solve:" in line.lower()]
    return bool(errors and re.search(
        r"(?:configure gha cache|import cache|cache (?:manifest|importer|exporter)|"
        r"(?:actions|github).*cache)", errors[-1], re.IGNORECASE,
    ))


def build(repo: Path, *, print_only: bool = False, no_cache: bool = False) -> int:
    env = dict(os.environ)
    version = (repo / "VERSION").read_text().strip()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError("VERSION must use major.minor.patch")
    revision = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=repo, check=True,
        capture_output=True, text=True,
    ).stdout.strip()
    major, minor, patch = version.split(".")
    env.update(
        GOPULSE_VERSION=version, GOPULSE_REVISION=revision, GOPULSE_IMAGE_TAG=version,
        GOPULSE_UPDATE_VERSION=f"{major}.{minor}.{int(patch) + 1}",
    )
    cached = not no_cache and bool(
        env.get("ACTIONS_RUNTIME_TOKEN") and env.get("ACTIONS_RESULTS_URL")
    )
    config = definition(repo, env, cached)
    if print_only:
        print(json.dumps(config, indent=2))
        return 0
    if not cached:
        print("Compose build: remote cache unavailable; building normally.", flush=True)
    with tempfile.TemporaryDirectory(prefix="gopulse-ci-build-") as temporary:
        path = Path(temporary) / "bake.json"
        command = ["docker", "buildx", "bake", "--file", str(path), "--load"]
        path.write_text(json.dumps(config))
        result = subprocess.run(command, cwd=repo, env=env, capture_output=True, text=True)
        sys.stdout.write(result.stdout)
        sys.stderr.write(result.stderr)
        if result.returncode and cached and cache_failure(result.stdout + result.stderr):
            print("Compose build: cache service failed; retrying once without remote cache.", flush=True)
            for target in config["target"].values():
                target.pop("cache-from", None)
                target.pop("cache-to", None)
            path.write_text(json.dumps(config))
            result = subprocess.run(command, cwd=repo, env=env)
        return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--print", dest="print_only", action="store_true")
    parser.add_argument("--no-cache", action="store_true")
    args = parser.parse_args()
    try:
        return build(args.repo.resolve(), print_only=args.print_only, no_cache=args.no_cache)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"Compose build configuration failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
