#!/usr/bin/env python3
"""Verify sanitized Phase 18 capacity evidence."""
import argparse
import json
from pathlib import Path

from phase18_evidence import validate_capacity

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--capacity', type=Path, required=True)
    arguments = parser.parse_args()
    try:
        document = json.loads(arguments.capacity.read_text())
        validate_capacity(document)
    except Exception as error:
        print('Phase 18 capacity evidence rejected (' + type(error).__name__ + ')')
        raise SystemExit(1)
    print('PASS: Phase 18 capacity evidence, candidate, recipe, repeatability, SLO, and cleanup')
