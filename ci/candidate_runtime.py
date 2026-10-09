#!/usr/bin/env python3
"""Resolve and extract immutable candidate executables for existing real verifiers."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import uuid

from release_artifacts import platform_ref, verify_bundle


def run(*args):
    result = subprocess.run(['docker',*args], capture_output=True, text=True, timeout=240)
    if result.returncode:
        raise RuntimeError('candidate Docker operation failed: '+args[0])
    return result.stdout.strip()


def extract(manifest, output, names):
    value = verify_bundle(manifest)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    if output.stat().st_mode & 0o077:
        raise ValueError('candidate extraction directory must be private')
    owner = uuid.uuid4().hex
    paths = {'backend': ['server','migrate','search-reindex'],
             'business-worker': ['business-worker'], 'search-indexer': ['search-indexer'],
             'marshaller': ['marshaller']}
    result = {'version':value['version'],'revision':value['revision'],
              'images':{k:platform_ref(v,'linux/amd64') for k,v in value['images'].items()}}
    for name in names:
        ref = result['images'][name]
        run('pull', ref)
        cid = run('create','--label','io.gopulse.candidate-extraction='+owner,ref)
        try:
            for binary in paths[name]:
                destination = output/binary
                if destination.exists():
                    raise ValueError('refusing to overwrite extracted candidate binary')
                run('cp',cid+':/usr/local/bin/'+binary,str(destination))
                destination.chmod(0o700)
        finally:
            obj = json.loads(run('inspect',cid))[0]
            if obj['Config']['Labels'].get('io.gopulse.candidate-extraction') != owner:
                raise RuntimeError('candidate extraction ownership mismatch')
            run('rm','-v',cid)
    return result


if __name__ == '__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest',required=True,type=Path)
    p.add_argument('--output',required=True,type=Path)
    p.add_argument('--binary',action='append',choices=['backend','business-worker','search-indexer','marshaller'],default=[])
    a=p.parse_args()
    print(json.dumps(extract(a.manifest,a.output,a.binary)))
