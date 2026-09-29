#!/usr/bin/env python3
"""Read-only verification of a Phase 19 capacity evidence directory."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "ci"))

from phase19_evidence import validate_evidence


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("path", nargs="?", type=Path)
    parser.add_argument("--directory", "--evidence-dir", dest="directory", type=Path)
    args = parser.parse_args(argv)
    selected = args.directory or args.path
    if selected is None:
        parser.error("an evidence directory is required")
    directory = selected.resolve()
    try:
        evidence_path = directory / "capacity-evidence.json"
        document = json.loads(evidence_path.read_text(encoding="utf-8"))
        validate_evidence(document, directory)
    except Exception as error:
        print(f"Phase 19 evidence rejected ({type(error).__name__}): {error}", file=sys.stderr)
        return 1
    print(f"PASS: Phase 19 evidence is bound, raw-preserving, and strictly verified ({document['execution_status']}/{document['capability_status']})")
    return 0 if document["execution_status"] == "complete" else 2


if __name__ == "__main__":
    raise SystemExit(main())
