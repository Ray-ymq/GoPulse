#!/usr/bin/env python3
"""Validate that package metadata follows the root product VERSION."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
OPENAPI = Path("backend/api/openapi.yaml")


def openapi_info_version(text: str) -> str | None:
    """Read info.version from the checked-in API description without a YAML parser."""

    inside = False
    for line in text.splitlines():
        if line.startswith("info:"):
            inside = True
            continue
        if inside:
            if line and not line[0].isspace():
                return None
            match = re.match(r"^  version:[ \t]*(\S+)[ \t]*$", line)
            if match:
                return match.group(1)
    return None


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
        environment_values: dict[str, string] = {}
        for line in environment_path.read_text(encoding="utf-8").splitlines():
            key, separator, value = line.partition("=")
            if separator and key in {"GOPULSE_VERSION", "GOPULSE_IMAGE_TAG"}:
                environment_values[key] = value.strip()
        environment_version = environment_values.get("GOPULSE_VERSION")
        if environment_version != version:
            errors.append(
                f".env.example GOPULSE_VERSION is {environment_version!r}; expected root VERSION {version!r}"
            )
        image_tag = environment_values.get("GOPULSE_IMAGE_TAG")
        if image_tag != version:
            errors.append(
                f".env.example GOPULSE_IMAGE_TAG is {image_tag!r}; expected root VERSION {version!r}"
            )

    for relative, label in [
        (Path("frontend/package.json"), "frontend package"),
        (Path("frontend/package-lock.json"), "frontend lockfile"),
        (Path("admin-frontend/package.json"), "admin frontend package"),
        (Path("admin-frontend/package-lock.json"), "admin frontend lockfile"),
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

    try:
        openapi_text = (repo / OPENAPI).read_text(encoding="utf-8")
    except OSError as exc:
        errors.append(f"could not read {OPENAPI}: {exc}")
    else:
        document_version = openapi_info_version(openapi_text)
        if document_version is None:
            errors.append(f"{OPENAPI} has no info.version")
        elif document_version != version:
            errors.append(
                f"{OPENAPI} info.version is {document_version!r}; expected root VERSION {version!r}"
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
