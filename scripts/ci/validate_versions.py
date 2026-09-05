#!/usr/bin/env python3
"""Validate that package metadata follows the root product VERSION."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")


def validate(repo: Path) -> list[str]:
    errors: list[str] = []
    try:
        version = (repo / "VERSION").read_text(encoding="utf-8").strip()
    except OSError as exc:
        return [f"could not read VERSION: {exc}"]
    if not SEMVER.fullmatch(version):
        errors.append(f"VERSION must use major.minor.patch; found {version!r}")

    environment_path = repo / ".env.example"
    if environment_path.exists():
        environment_version = None
        for line in environment_path.read_text(encoding="utf-8").splitlines():
            if line.startswith("GOPULSE_VERSION="):
                environment_version = line.partition("=")[2].strip()
                break
        if environment_version != version:
            errors.append(
                f".env.example GOPULSE_VERSION is {environment_version!r}; expected root VERSION {version!r}"
            )

    for relative, label in [
        (Path("frontend/package.json"), "frontend package"),
        (Path("frontend/package-lock.json"), "frontend lockfile"),
    ]:
        path = repo / relative
        try:
            document = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            errors.append(f"could not read {relative}: {exc}")
            continue
        metadata_version = document.get("version")
        if metadata_version != version:
            errors.append(f"{label} version is {metadata_version!r}; expected root VERSION {version!r}")
        if relative.name == "package-lock.json":
            locked_version = document.get("packages", {}).get("", {}).get("version")
            if locked_version != version:
                errors.append(
                    f"frontend lockfile root package version is {locked_version!r}; expected root VERSION {version!r}"
                )
    return errors


def main() -> int:
    errors = validate(Path.cwd())
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Version metadata matches root VERSION.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
