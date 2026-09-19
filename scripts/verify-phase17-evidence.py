#!/usr/bin/env python3
"""Verify final Phase 17 Linux amd64 evidence and attachment binding."""
import argparse
import json
import sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parent/'ci'))
from phase17_evidence import verify
p=argparse.ArgumentParser(description=__doc__);p.add_argument('--linux',type=Path,required=True);a=p.parse_args()
verify(json.loads(a.linux.read_text()),a.linux.parent)
print('PASS: Phase 17 Linux amd64 evidence, candidate binding, matrix, redaction and cleanup')
