#!/usr/bin/env python3
"""Verify the completed, same-candidate Linux product matrix and attachments."""
import argparse
import json
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parent/'ci'))
from phase16_evidence import verify

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--linux', type=Path, required=True)
    args = parser.parse_args()
    verify(json.loads(args.linux.read_text()), args.linux.parent)
    print('PASS: Phase 16 Linux amd64 evidence, candidate binding, matrix and redaction')
