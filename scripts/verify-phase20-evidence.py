#!/usr/bin/env python3
"""Strict Phase 20 diagnostic evidence entry point."""
import argparse
import json
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parent/'ci'))
from phase20_evidence import verify_directory, verify_publication
from phase20_evidence import verify_retention_directory
from phase20_chain import verify_directory as verify_chain_directory
from phase20_optimization import verify_optimization_directory
from phase20_budget import verify_budget_directory
from phase20_closure import verify_closure_directory
from phase20_closure import verify_publication_source
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--diagnostic',type=Path)
parser.add_argument('--publication',type=Path)
parser.add_argument('--chain',type=Path)
parser.add_argument('--optimization',type=Path)
parser.add_argument('--retention',type=Path)
parser.add_argument('--budget',type=Path)
parser.add_argument('--preflight',type=Path)
parser.add_argument('--closure',type=Path)
parser.add_argument('--source',type=Path)
args=parser.parse_args()
try:
    selected=[value for value in (args.diagnostic,args.chain,args.optimization,args.retention,args.budget,args.preflight,args.closure) if value]
    if args.source and not args.publication:
        parser.error('--source requires --publication')
    if args.publication and not args.diagnostic and not args.source:
        parser.error('--publication requires --source unless paired with --diagnostic')
    if args.publication and not args.diagnostic:
        selected.append(args.publication)
    if len(selected)!=1:
        parser.error('provide exactly one evidence directory mode')
    if args.chain:
        result=verify_chain_directory(args.chain)
    elif args.optimization:
        contract=args.optimization.parent/'contract.json'
        if not contract.is_file():
            contract=args.optimization.parent/'optimization-contract.json'
        if not contract.is_file():
            parser.error('optimization directory must contain contract.json or optimization-contract.json')
        result=verify_optimization_directory(args.optimization,contract)
    elif args.retention:
        result=verify_retention_directory(args.retention)
    elif args.budget:
        document=json.loads((args.budget/'budget.json').read_text())
        result=verify_budget_directory(args.budget,formal=bool(document.get('formal')))
    elif args.preflight:
        result=verify_closure_directory(args.preflight,formal=False)
    elif args.closure:
        result=verify_closure_directory(args.closure,formal=True)
    elif args.publication:
        if not args.source:
            parser.error('--publication requires --source')
        result=verify_publication_source(args.publication,args.source)
    else:
        result=verify_directory(args.diagnostic)
        if args.publication:result['publication']=verify_publication(args.diagnostic,args.publication)
    print(json.dumps(result,indent=2))
except (ValueError,KeyError,OSError,TypeError) as error:
    print(json.dumps({'execution_status':'incomplete','error':str(error)}));sys.exit(1)
