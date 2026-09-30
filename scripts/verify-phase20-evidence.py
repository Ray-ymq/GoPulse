#!/usr/bin/env python3
"""Strict Phase 20 diagnostic evidence entry point."""
import argparse
import json
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parent/'ci'))
from phase20_evidence import verify_directory, verify_publication
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--diagnostic',required=True,type=Path)
parser.add_argument('--publication',type=Path)
args=parser.parse_args()
try:
    result=verify_directory(args.diagnostic)
    if args.publication:result['publication']=verify_publication(args.diagnostic,args.publication)
    print(json.dumps(result,indent=2))
except (ValueError,KeyError,OSError,TypeError) as error:
    print(json.dumps({'execution_status':'incomplete','error':str(error)}));sys.exit(1)
