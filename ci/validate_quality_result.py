#!/usr/bin/env python3
"""Fail the quality workflow when a selected job is missing or not successful."""

from __future__ import annotations

import argparse
import json
import sys
from typing import Any


CHECK_JOBS = {
    "backend": "module-verify",
    "componentmetrics": "module-verify",
    "router": "module-verify",
    "marshaller": "module-verify",
    "monitor": "module-verify",
    "lifecycle": "module-verify",
    "loadtest": "module-verify",
    "exporters_elasticsearch": "module-verify",
    "exporters_kafka": "module-verify",
    "exporters_mysql": "module-verify",
    "exporters_rabbitmq": "module-verify",
    "exporters_redis": "module-verify",
    "exporters_victoriametrics": "module-verify",
    "devtools": "module-verify",
    "acceptance": "module-verify",
    "frontend": "module-verify",
    "admin_frontend": "module-verify",
    "integration_business": "integration",
    "integration_observe": "integration-observe",
    "e2e_business": "e2e-business",
    "e2e_observe": "e2e-observe",
    "compose": "compose-full-stack",
    "tools": "native-acceptance",
}


def validate(checks: dict[str, Any], needs: dict[str, Any], run_product_checks: bool = True) -> list[str]:
    errors: list[str] = []
    if needs.get("governance", {}).get("result") != "success":
        errors.append(f"governance result is {needs.get('governance', {}).get('result', 'missing')!r}")
    if not run_product_checks:
        return errors
    for check, selected in checks.items():
        if not selected or check not in CHECK_JOBS:
            continue
        job = CHECK_JOBS[check]
        result = needs.get(job, {}).get("result")
        if result != "success":
            errors.append(f"selected check {check} requires {job}=success; got {result or 'missing'}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--checks", required=True)
    parser.add_argument("--needs", required=True)
    parser.add_argument("--run-product-checks", default="true")
    args = parser.parse_args()
    try:
        checks = json.loads(args.checks)
        needs = json.loads(args.needs)
    except json.JSONDecodeError as exc:
        print(f"ERROR: invalid quality result JSON: {exc}", file=sys.stderr)
        return 2
    errors = validate(checks, needs, args.run_product_checks.lower() == "true")
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("quality result validation passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
