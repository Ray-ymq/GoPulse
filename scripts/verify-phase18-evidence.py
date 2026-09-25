#!/usr/bin/env python3
"""Verify sanitized Phase 18 capacity or scaling evidence."""
import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / 'ci'))

from phase18_evidence import validate_capacity, validate_scaling, validate_qualification

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument('--capacity', type=Path)
    source.add_argument('--scaling', type=Path)
    source.add_argument('--qualification', type=Path)
    arguments = parser.parse_args()
    try:
        path = arguments.capacity or arguments.scaling or arguments.qualification
        document = json.loads(path.read_text())
        if arguments.capacity:
            validate_capacity(document)
            detail = 'candidate, recipe, repeatability, SLO, and cleanup'
        elif arguments.qualification:
            validate_qualification(
                document,
                work=path.resolve().parent.parent,
            )
            detail = 'candidate-bound infrastructure qualification, diagnostics, and cleanup'
        else:
            validate_scaling(document)
            detail = 'topology, paired scaling, replacement, ownership, and cleanup'
    except Exception as error:
        print('Phase 18 evidence rejected (' + type(error).__name__ + ')')
        raise SystemExit(1)
    print('PASS: Phase 18 evidence, ' + detail)
