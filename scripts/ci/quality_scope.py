#!/usr/bin/env python3
"""Select the smallest CI checks required by the files changed in a branch."""

from __future__ import annotations

import argparse
import json
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

MODULES = (
    "backend",
    "componentmetrics",
    "router",
    "marshaller",
    "monitor",
    "exporters/redis",
    "exporters/mysql",
    "exporters/rabbitmq",
    "exporters/elasticsearch",
    "exporters/kafka",
    "exporters/victoriametrics",
    # Standalone tooling modules: they are built and tested, but they do not
    # participate in the product integration or browser matrices.
    "lifecycle",
    "loadtest",
    # The native local environment helper drives the integration and browser
    # matrices, so its own changes select those checks as well.
    "devtools",
)
STANDALONE_MODULES = ("lifecycle", "loadtest")
CHECKS = MODULES + (
    "frontend",
    "admin_frontend",
    "integration_business",
    "integration_observe",
    "e2e_business",
    "e2e_observe",
    "compose",
    "tools",
)

GO_MODULE_ROOTS = {
    "backend": "backend",
    "componentmetrics": "componentmetrics",
    "router": "router",
    "marshaller": "marshaller",
    "monitor": "monitor",
    "exporters/redis": "exporters/redis",
    "exporters/mysql": "exporters/mysql",
    "exporters/rabbitmq": "exporters/rabbitmq",
    "exporters/elasticsearch": "exporters/elasticsearch",
    "exporters/kafka": "exporters/kafka",
    "exporters/victoriametrics": "exporters/victoriametrics",
    "lifecycle": "lifecycle",
    "loadtest": "loadtest",
    "devtools": "devtools",
}

DOC_SUFFIXES = (".md", ".markdown", ".rst", ".adoc", ".txt")
OBSERVE_BACKEND_PARTS = (
    "internal/alert",
    "internal/adminoverview",
    "internal/eventquery",
    "internal/exporterplugin",
    "internal/logquery",
    "internal/metricquery",
    "internal/observability",
    "internal/platform",
)
AUTHORITY_PATHS = (
    "internal/auth",
    "internal/http",
    "internal/user",
    "internal/platform",
    "internal/exporterplugin",
    "internal/adminoverview",
)


@dataclass(frozen=True)
class Scope:
    changed_files: tuple[str, ...]
    checks: dict[str, bool]
    reasons: tuple[str, ...]


def _normalise(path: str) -> str:
    path = path.replace("\\", "/")
    while path.startswith("./"):
        path = path[2:]
    return path


def changed_files(repo: Path, base_ref: str, head_ref: str = "HEAD") -> list[str]:
    """Return files from the common ancestor to ``head_ref``.

    Using merge-base explicitly makes the selection independent of the last
    commit on a branch when the base branch advanced or the branch was merged.
    """

    common = subprocess.run(
        ["git", "merge-base", base_ref, head_ref],
        cwd=repo,
        check=True,
        text=True,
        encoding="utf-8",
        capture_output=True,
    ).stdout.strip()
    result = subprocess.run(
        [
            "git",
            "-c",
            "core.quotepath=false",
            "diff",
            "--name-only",
            "-z",
            common,
            head_ref,
        ],
        cwd=repo,
        check=True,
        text=True,
        encoding="utf-8",
        capture_output=True,
    )
    return [_normalise(path) for path in result.stdout.split("\0") if path]


def _module_for_path(path: str) -> str | None:
    for module, root in GO_MODULE_ROOTS.items():
        if path == root or path.startswith(root + "/"):
            return module
    return None


def _local_replace_consumers(repo: Path, module_root: str) -> set[str]:
    """Find Go modules that consume a local replacement module."""

    consumers: set[str] = set()
    for module, root in GO_MODULE_ROOTS.items():
        go_mod = repo / root / "go.mod"
        if not go_mod.is_file():
            continue
        for line in go_mod.read_text(encoding="utf-8").splitlines():
            if "replace" not in line or "=>" not in line:
                continue
            replacement = line.split("=>", 1)[1].strip().split()[0]
            replacement_path = (go_mod.parent / replacement).resolve()
            if replacement_path == (repo / module_root).resolve():
                consumers.add(module)
    return consumers


def _is_documentation(path: str) -> bool:
    if path.startswith("dev/") or path.startswith("docs/"):
        return True
    if path in {"README.md", "AGENTS.md", "项目导读.md", "使用手册.md"}:
        return True
    return path.lower().endswith(DOC_SUFFIXES)


def _all_product(checks: dict[str, bool]) -> None:
    for name in CHECKS:
        if name != "tools":
            checks[name] = True
    checks["tools"] = True


def select(repo: Path, paths: Iterable[str]) -> Scope:
    checks = {name: False for name in CHECKS}
    normalised = tuple(sorted({_normalise(path) for path in paths if _normalise(path)}))
    reasons: list[str] = []

    for path in normalised:
        if _is_documentation(path):
            reasons.append(f"{path}: documentation/governance only")
            continue

        if path.startswith(".github/workflows/") or path.startswith(".github/actions/"):
            _all_product(checks)
            reasons.append(f"{path}: CI/action changes require the product matrix")
            continue

        if path in {"VERSION", ".env.example"}:
            _all_product(checks)
            reasons.append(f"{path}: runtime metadata/configuration affects all gates")
            continue

        if path.startswith("deploy/") or path.endswith("Dockerfile") or "/Dockerfile" in path:
            checks["compose"] = True
            checks["tools"] = True
            reasons.append(f"{path}: deployment/container gate")
            continue

        if path.startswith("devtools/"):
            checks["devtools"] = True
            checks["integration_business"] = True
            checks["integration_observe"] = True
            checks["e2e_business"] = True
            checks["e2e_observe"] = True
            reasons.append(f"{path}: native environment helper gate")
            continue

        if path.startswith("scripts/"):
            checks["tools"] = True
            if "verify-compose" in path or "verify-release" in path:
                checks["compose"] = True
            if "verify-business" in path:
                checks["integration_business"] = True
            if "verify-observability" in path or "verify-logs" in path or "verify-events" in path:
                checks["integration_observe"] = True
            if "verify-admin" in path or "frontend" in path:
                checks["e2e_business"] = True
                checks["e2e_observe"] = True
            reasons.append(f"{path}: tool self-test/syntax gate")
            continue

        if path.startswith("frontend/"):
            checks["frontend"] = True
            checks["e2e_business"] = True
            if "/admin" in path or path.startswith("frontend/vite.config"):
                checks["e2e_observe"] = True
            reasons.append(f"{path}: user frontend checks")
            continue

        if path.startswith("admin-frontend/"):
            checks["admin_frontend"] = True
            checks["e2e_observe"] = True
            reasons.append(f"{path}: management frontend checks")
            continue

        module = _module_for_path(path)
        if module is not None:
            checks[module] = True
            module_root = GO_MODULE_ROOTS[module]
            if path == module_root + "/go.mod" or path == module_root + "/go.sum":
                for consumer in _local_replace_consumers(repo, module_root):
                    checks[consumer] = True
            if module in STANDALONE_MODULES:
                reasons.append(f"{path}: {module} module checks")
                continue
            if module == "componentmetrics":
                for consumer in _local_replace_consumers(repo, "componentmetrics"):
                    checks[consumer] = True
                checks["integration_observe"] = True
            if module == "backend":
                checks["integration_business"] = True
                if any(part in path for part in OBSERVE_BACKEND_PARTS):
                    checks["integration_observe"] = True
                    checks["e2e_observe"] = True
                if any(part in path for part in AUTHORITY_PATHS):
                    checks["e2e_observe"] = True
                if "internal/http" in path and not any(part in path for part in OBSERVE_BACKEND_PARTS):
                    checks["e2e_business"] = True
            else:
                checks["integration_observe"] = True
            if module in {"router", "marshaller", "monitor"}:
                checks["e2e_observe"] = True
            reasons.append(f"{path}: {module} module checks")
            continue

        if path.startswith(".env") or path.endswith("package.json") or path.endswith("package-lock.json"):
            checks["frontend"] = True
            checks["admin_frontend"] = True
            reasons.append(f"{path}: package/environment checks")
            continue

        # A new product path must not silently evade the required checks.
        _all_product(checks)
        reasons.append(f"{path}: unknown non-documentation path, conservative product matrix")

    return Scope(normalised, checks, tuple(reasons))


def _render(scope: Scope) -> dict[str, object]:
    return {
        "changed_files": list(scope.changed_files),
        "checks": scope.checks,
        "reasons": list(scope.reasons),
    }


def write_github_output(scope: Scope, target: Path) -> None:
    github_checks = {name.replace("/", "_"): value for name, value in scope.checks.items()}
    lines = [f"{name}={'true' if value else 'false'}" for name, value in github_checks.items()]
    lines.append("checks=" + json.dumps(github_checks, sort_keys=True, separators=(",", ":")))
    lines.append("changed_files<<GOPULSE_CHANGED_FILES")
    lines.extend(scope.changed_files)
    lines.append("GOPULSE_CHANGED_FILES")
    target.write_text("\n".join(lines) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--base-ref")
    parser.add_argument("--head-ref", default="HEAD")
    parser.add_argument("--changed-file", action="append", default=[])
    parser.add_argument("--format", choices=("json", "text"), default="text")
    parser.add_argument("--github-output", type=Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repo = args.repo.resolve()
    if args.changed_file:
        paths = args.changed_file
    elif args.base_ref:
        paths = changed_files(repo, args.base_ref, args.head_ref)
    else:
        raise SystemExit("--base-ref is required when --changed-file is not supplied")
    scope = select(repo, paths)
    if args.github_output:
        write_github_output(scope, args.github_output.resolve())
    if args.format == "json":
        print(json.dumps(_render(scope), ensure_ascii=False, sort_keys=True))
    else:
        print("changed files:")
        for path in scope.changed_files:
            print(f"  {path}")
        print("selected checks:")
        selected = [name for name, enabled in scope.checks.items() if enabled]
        print("  " + (", ".join(selected) if selected else "governance only"))
        for reason in scope.reasons:
            print(f"reason: {reason}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
