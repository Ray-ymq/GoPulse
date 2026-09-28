#!/usr/bin/env python3
"""Verify either retained Phase-18-01 capacity evidence or new 18-05 closure evidence."""

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "ci"))

from phase18_evidence import validate_capacity
from phase18_scale_evidence import validate_evidence


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--capacity", type=Path, help="retained Phase-18-01 capacity evidence")
    group.add_argument("--closure", type=Path, help="Phase-18-05 two-run closure evidence directory")
    args = parser.parse_args()
    try:
        if args.capacity is not None:
            document = json.loads(args.capacity.read_text(encoding="utf-8"))
            validate_capacity(document)
            print("PASS: Phase-18-01 historical capacity evidence; no Phase-18-05 result inferred")
        else:
            summary = validate_evidence(args.closure)
            print(f"PASS: Phase-18-05 closure evidence, two runs, averages and file ledger ({summary['result']})")
    except Exception as error:
        print(f"Phase 18 evidence rejected ({type(error).__name__}): {error}")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
