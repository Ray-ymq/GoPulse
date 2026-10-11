#!/usr/bin/env python3
"""Render the authoritative Compose file and run its static safety assertions."""

from __future__ import annotations

import argparse
import subprocess
from pathlib import Path


IMAGE_SERVICES = (
    "admin-frontend",
    "frontend",
    "backend",
    "business-worker",
    "search-indexer",
    "router",
    "marshaller",
    "monitor",
)


def render(repo: Path) -> str:
    command = [
        "docker",
        "compose",
        "--env-file",
        ".env.example",
        "--file",
        "deploy/compose.yaml",
        "config",
    ]
    result = subprocess.run(command, cwd=repo, text=True, encoding="utf-8", capture_output=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or f"docker compose config exited with {result.returncode}")
    return result.stdout


def validate(rendered: str, version: str) -> list[str]:
    problems: list[str] = []
    if rendered.count("host_ip: 127.0.0.1") != 1:
        problems.append("expected exactly one host_ip: 127.0.0.1 publication")
    for image in IMAGE_SERVICES:
        expected = f"image: gopulse/{image}:{version}"
        if expected not in rendered:
            problems.append(f"missing rendered image {expected}")
    if "image: apache/kafka:4.3.1" not in rendered:
        problems.append("missing pinned Kafka image")
    if "image: victoriametrics/victoria-metrics:v1.151.0" not in rendered:
        problems.append("missing pinned VictoriaMetrics image")
    if "condition: service_completed_successfully" not in rendered:
        problems.append("missing completed-service dependency condition")
    if rendered.count("internal: true") != 2:
        problems.append("expected exactly two internal networks")
    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    args = parser.parse_args()
    repo = args.repo.resolve()
    try:
        version = (repo / "VERSION").read_text(encoding="utf-8").strip()
        problems = validate(render(repo), version)
    except (OSError, RuntimeError) as exc:
        print(f"ERROR: {exc}")
        return 2
    if problems:
        for problem in problems:
            print(f"ERROR: {problem}")
        return 1
    print("Compose static checks passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
