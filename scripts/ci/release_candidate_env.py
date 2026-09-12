#!/usr/bin/env python3
"""Print validated immutable references for the existing isolated Compose runner."""
import argparse
from release_manifest import load, validate
from release_artifacts import platform_ref

if __name__ == '__main__':
    p=argparse.ArgumentParser()
    p.add_argument('--manifest',required=True)
    p.add_argument('--version',required=True)
    p.add_argument('--revision',required=True)
    a=p.parse_args()
    m=validate(load(a.manifest),a.version,a.revision)
    for name,image in {**m['images'],**m['third_party']}.items():
        print('GOPULSE_'+name.upper().replace('-','_')+'_IMAGE='+platform_ref(image,'linux/amd64'))
