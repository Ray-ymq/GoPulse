#!/usr/bin/env python3
"""Fixed release gates; foreign-platform output is explicitly metadata-only."""
import argparse
import json
import os
from pathlib import Path
import subprocess
from release_artifacts import inspect_image, platform_ref, plugin_records, run, verify_bundle
from release_manifest import ROOT, PLATFORMS, sha


def main():
    p=argparse.ArgumentParser();p.add_argument('--manifest',required=True,type=Path)
    p.add_argument('--platform',required=True,choices=PLATFORMS)
    mode=p.add_mutually_exclusive_group(required=True);mode.add_argument('--runtime',action='store_true');mode.add_argument('--metadata-only',action='store_true')
    a=p.parse_args();a.manifest=a.manifest.resolve();m=verify_bundle(a.manifest)
    for name,image in {**m['images'],'lifecycle':m['lifecycle']}.items():
        inspect_image(image,a.platform,m['version'],m['revision'])
        print('PASS metadata:',name,a.platform,flush=True)
    for name,image in m['third_party'].items():
        inspect_image(image,a.platform,m['version'],m['revision'],product=False)
        print('PASS third-party metadata:',name,a.platform,flush=True)
    plugins=plugin_records(m['images']['monitor'],a.platform,a.manifest.parent/'verified-plugins'/a.platform.split('/')[1])
    expected=[p for p in m['plugins'] if p['arch']==a.platform.split('/')[1]]
    if sorted(plugins,key=lambda x:(x['id'],x['version']))!=sorted(expected,key=lambda x:(x['id'],x['version'])):raise ValueError('Monitor plugin catalog differs from manifest')
    if a.runtime:
        server=json.loads(run('docker','version','--format','{{json .Server}}'))
        if a.platform!='linux/amd64' or server['Os']+'/'+server['Arch']!=a.platform:raise ValueError('runtime gate requires real matching Linux amd64 server')
        ref=platform_ref(m['lifecycle'],a.platform);run('docker','pull',ref,capture=False)
        result=json.loads(run('docker','run','--rm','--read-only','--network','none','--cap-drop','ALL',ref,'version','--json'))
        if result['version']!=m['version'] or result['revision']!=m['revision'] or result['platform']!=a.platform:raise ValueError('lifecycle runtime mismatch')
        # Full Compose gate runs ONCE, consuming the candidate platform digests.
        subprocess.run([str(ROOT/'scripts/verify-compose.sh')],env={**os.environ,'GOPULSE_RELEASE_MANIFEST':str(a.manifest)},check=True)
        status='amd64-runtime-and-compose-passed'
    else:
        status='metadata-only; real arm64 runtime DEFERRED to Phase-16-06'
    receipt={'manifest_sha256':sha(a.manifest.read_bytes()),'revision':m['revision'],'platform':a.platform,'status':status}
    (a.manifest.parent/('verification-'+a.platform.split('/')[1]+'.json')).write_text(json.dumps(receipt,indent=2)+'\n')
    print(status)

if __name__=='__main__':main()
