#!/usr/bin/env python3
"""Strict Phase 20 diagnostic evidence entry point."""
import argparse
import json
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parent/'ci'))
from phase20_evidence import verify_directory, verify_publication
from phase20_chain import verify_directory as verify_chain_directory
from phase20_optimization import verify_optimization_directory
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--diagnostic',type=Path)
parser.add_argument('--publication',type=Path)
parser.add_argument('--chain',type=Path)
parser.add_argument('--optimization',type=Path)
args=parser.parse_args()
try:
    selected=[value for value in (args.diagnostic,args.chain,args.optimization) if value]
    if len(selected)!=1:
        parser.error('provide exactly one of --diagnostic, --chain, or --optimization')
    if args.chain:
        result=verify_chain_directory(args.chain)
    elif args.optimization:
        contract=args.optimization.parent/'contract.json'
        if not contract.is_file():
            contract=args.optimization.parent/'optimization-contract.json'
        if not contract.is_file():
            parser.error('optimization directory must contain contract.json or optimization-contract.json')
        result=verify_optimization_directory(args.optimization,contract)
    else:
        result=verify_directory(args.diagnostic)
        if args.publication:result['publication']=verify_publication(args.diagnostic,args.publication)
    print(json.dumps(result,indent=2))
except (ValueError,KeyError,OSError,TypeError) as error:
    print(json.dumps({'execution_status':'incomplete','error':str(error)}));sys.exit(1)
