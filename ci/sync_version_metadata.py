#!/usr/bin/env python3
"""Synchronize all checked-in product version metadata to one SemVer value."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path

SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
PACKAGE_FILES = (
    Path("frontend/package.json"),
    Path("frontend/package-lock.json"),
    Path("admin-frontend/package.json"),
    Path("admin-frontend/package-lock.json"),
)


class VersionMetadataError(ValueError):
    """Raised when the repository does not have the expected metadata shape."""


def _write_if_changed(path: Path, content: str) -> bool:
    current = path.read_text(encoding="utf-8")
    if current == content:
        return False
    path.write_text(content, encoding="utf-8")
    return True


def _render_env(path: Path, version: str) -> str:
    if not path.is_file():
        raise VersionMetadataError(f"missing {path}")
    lines = path.read_text(encoding="utf-8").splitlines(keepends=True)
    seen: set[str] = set()
    output: list[str] = []
    for line in lines:
        key, separator, _ = line.partition("=")
        if separator and key in {"GOPULSE_VERSION", "GOPULSE_IMAGE_TAG"}:
            line = f"{key}={version}\n"
            seen.add(key)
        output.append(line)
    missing = {"GOPULSE_VERSION", "GOPULSE_IMAGE_TAG"} - seen
    if missing:
        raise VersionMetadataError(f"missing .env.example keys: {', '.join(sorted(missing))}")
    return "".join(output)


def _render_package(path: Path, version: str) -> str:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise VersionMetadataError(f"cannot read {path}: {exc}") from exc
    if not isinstance(document, dict) or "version" not in document:
        raise VersionMetadataError(f"{path} has no top-level version")
    document["version"] = version
    if path.name == "package-lock.json":
        packages = document.get("packages")
        root = packages.get("") if isinstance(packages, dict) else None
        if not isinstance(root, dict) or "version" not in root:
            raise VersionMetadataError(f"{path} has no packages[''] version")
        root["version"] = version
    return json.dumps(document, indent=2, ensure_ascii=False) + "\n"


def sync(repo: Path, version: str) -> list[Path]:
    """Update the root and all product package metadata, returning changed paths."""
    if not SEMVER.fullmatch(version):
        raise VersionMetadataError("version must use major.minor.patch")
    version_path = repo / "VERSION"
    if not version_path.is_file():
        raise VersionMetadataError("missing VERSION")
    updates: list[tuple[Path, Path, str]] = [(Path("VERSION"), version_path, version + "\n")]
    env_path = repo / ".env.example"
    updates.append((Path(".env.example"), env_path, _render_env(env_path, version)))
    for relative in PACKAGE_FILES:
        path = repo / relative
        if not path.is_file():
            raise VersionMetadataError(f"missing {relative}")
        updates.append((relative, path, _render_package(path, version)))

    changed: list[Path] = []
    for relative, path, content in updates:
        if _write_if_changed(path, content):
            changed.append(relative)
    return changed


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    try:
        changed = sync(args.repo.resolve(), args.version)
    except (OSError, VersionMetadataError) as exc:
        parser.error(str(exc))
    if changed:
        print("\n".join(str(path) for path in changed))
    else:
        print("version metadata already synchronized")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
