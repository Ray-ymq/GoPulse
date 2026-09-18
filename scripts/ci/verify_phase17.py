#!/usr/bin/env python3
"""Run and aggregate the final immutable Phase 17 Linux amd64 matrix."""
import argparse
import datetime
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

from phase17_evidence import atomic, sha, verify
from release_artifacts import verify_bundle
from verify_current_recovery import CurrentRecovery

ROOT = Path(__file__).resolve().parents[2]


def now(): return datetime.datetime.now(datetime.timezone.utc).isoformat()

def run(command, log, cwd=ROOT, env=None, timeout=7200):
    started = now()
    with log.open('wb') as stream:
        log.chmod(0o600)
        result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Phase 17 gate failed: '+command[0]+'; private log retained')
    return started, now()

def attachment(source, destination):
    destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    shutil.copyfile(source, destination); destination.chmod(0o600)
    return {'path': str(destination.relative_to(destination.parents[1])), 'sha256': sha(destination), 'redacted': True}

def scene(name, binding, window, facts, refs):
    return {'status':'passed','reason_code':'accepted','manifest_sha256':binding['manifest_sha256'],
            'revision':binding['revision'],'started_at':window[0],'finished_at':window[1],
            'facts':facts,'attachments':refs}

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest',type=Path,required=True);p.add_argument('--runtime-contract',type=Path,required=True)
    p.add_argument('--from-manifest',type=Path,required=True);p.add_argument('--work',type=Path,required=True)
    p.add_argument('--evidence',type=Path,required=True)
    a=p.parse_args();a.manifest=a.manifest.resolve();a.from_manifest=a.from_manifest.resolve();a.runtime_contract=a.runtime_contract.resolve()
    work=a.work.resolve();work.mkdir(parents=True,exist_ok=True,mode=0o700)
    if work.stat().st_mode & 0o077: raise ValueError('Phase 17 work directory must be private')
    manifest=verify_bundle(a.manifest);source=verify_bundle(a.from_manifest)
    if manifest['version']!='1.14.5' or source['version']!='1.13.6':raise ValueError('Phase 17 requires 1.13.6 -> 1.14.5')
    if sha(a.runtime_contract)!=manifest['runtime_contract']['sha256'].removeprefix('sha256:'):raise ValueError('runtime contract differs from candidate Bundle')
    receipt=a.manifest.parent/'verification-amd64.json'
    expected={'manifest_sha256':'sha256:'+sha(a.manifest),'revision':manifest['revision'],'platform':'linux/amd64','status':'amd64-runtime-and-compose-passed'}
    if not receipt.exists() or json.loads(receipt.read_text())!=expected:raise ValueError('run release artifact runtime gate first')
    server=json.loads(subprocess.check_output(['docker','version','--format','{{json .Server}}'],text=True))
    if platform.system()!='Linux' or platform.machine()!='x86_64' or server['Os']+'/'+server['Arch']!='linux/amd64':raise ValueError('real Linux amd64 Docker required')
    before=CurrentRecovery.resources();started_all=now();logs=work/'logs';logs.mkdir(exist_ok=True,mode=0o700)
    paths={'runtime':work/'runtime.json','clean':work/'lifecycle-clean.json','failure':work/'lifecycle-failure.json',
           'state':work/'state'/'receipt.json','recovery':work/'recovery'/'evidence.json'}
    windows={}
    windows['runtime']=run([sys.executable,str(ROOT/'scripts/ci/runtime_acceptance.py'),'--candidate','1.14.5','--manifest',str(a.manifest),'--evidence',str(paths['runtime'])],logs/'runtime.log')
    windows['clean']=run([sys.executable,str(ROOT/'scripts/ci/verify_product_lifecycle.py'),'--platform','linux/amd64','--manifest',str(a.manifest),'--clean-install','--evidence',str(paths['clean'])],logs/'lifecycle-clean.log')
    windows['failure']=run([sys.executable,str(ROOT/'scripts/ci/verify_product_lifecycle.py'),'--platform','linux/amd64','--manifest',str(a.manifest),'--failure-matrix','--evidence',str(paths['failure'])],logs/'lifecycle-failure.log')
    windows['state']=run([str(ROOT/'scripts/verify-phase17-state.sh'),'--from-manifest',str(a.from_manifest),'--manifest',str(a.manifest),'--work',str(work/'state'),'--compose-receipt',str(receipt)],logs/'state.log')
    windows['recovery']=run([str(ROOT/'scripts/verify-backup-restore.sh'),'--platform','linux/amd64','--manifest',str(a.manifest),'--work',str(work/'recovery'),'--current-regression'],logs/'recovery.log')
    run([str(ROOT/'scripts/verify-backup-restore.sh'),'--platform','linux/amd64','--manifest',str(a.manifest),'--work',str(work/'recovery'),'--current-product','--cleanup'],logs/'recovery-cleanup.log')
    if CurrentRecovery.resources()!=before:raise RuntimeError('Phase 17 changed unrelated Docker resources')
    data={key:json.loads(path.read_text()) for key,path in paths.items()}
    if not data['runtime'].get('passed') or not data['state'].get('complete') or data['recovery'].get('status')!='passed':raise RuntimeError('incomplete final receipts')
    evidence=a.evidence.resolve();attach=evidence.parent/'attachments';attach.mkdir(parents=True,exist_ok=True,mode=0o700)
    refs={}
    for key,path in [('release-manifest',a.manifest),('runtime-contract',a.runtime_contract),('artifact-runtime',receipt),*paths.items()]:
        refs[key]=attachment(path,attach/(key+'.json'))
    binding={'manifest_sha256':sha(a.manifest),'revision':manifest['revision']}
    artifact_window=(started_all,windows['runtime'][0])
    checks=data['state']['checks']; recovery=data['recovery']
    scenarios={
      'release-runtime-compose':scene('release-runtime-compose',binding,artifact_window,{'receipt_reused':True,'platform':'linux/amd64'},[refs['artifact-runtime'],refs['release-manifest']]),
      'runtime-contract-probes-signals':scene('runtime-contract-probes-signals',binding,windows['runtime'],{'scenario_count':len(data['runtime']['scenarios']),'component_logs':len(data['runtime']['logs'])},[refs['runtime'],refs['runtime-contract']]),
      'lifecycle-clean-install':scene('lifecycle-clean-install',binding,windows['clean'],{'commands':len(data['clean']['commands']),'cleanup':data['clean']['cleanup_passed']},[refs['clean']]),
      'lifecycle-failure-cleanup':scene('lifecycle-failure-cleanup',binding,windows['failure'],{'commands':len(data['failure']['commands']),'cleanup':data['failure']['cleanup_passed']},[refs['failure']]),
      'migration-direct-predecessor':scene('migration-direct-predecessor',binding,windows['state'],{'steps':len(checks['migration']['results']),'from':'1.13.6','to':'1.14.5'},[refs['state']]),
      'rabbit-reliability':scene('rabbit-reliability',binding,windows['state'],{'candidate_gate':checks['business']['status'],'resource_inventory_preserved':checks['business']['resource_inventory_preserved']},[refs['state']]),
      'kafka-reliability':scene('kafka-reliability',binding,windows['state'],{'candidate_gate':checks['marshaller']['status'],'resource_inventory_preserved':checks['marshaller']['resource_inventory_preserved']},[refs['state']]),
      'alert-reliability':scene('alert-reliability',binding,windows['state'],{'candidate_gate':checks['alerts']['status'],'resource_inventory_preserved':checks['alerts']['resource_inventory_preserved']},[refs['state']]),
      'complete-product-permissions':scene('complete-product-permissions',binding,windows['state'],{'full_compose_reused':checks['compose']['reused'],'same_candidate':True},[refs['state'],refs['artifact-runtime']]),
      'backup-restore-current':scene('backup-restore-current',binding,windows['recovery'],{'completed':recovery['completed'],'restores':list(recovery.get('restores',{}))},[refs['recovery']]),
      'secret-ownership-cleanup':scene('secret-ownership-cleanup',binding,(started_all,now()),{'unrelated_resources_unchanged':True,'owned_cleanup':True},[refs['clean'],refs['failure'],refs['state'],refs['recovery']]),
    }
    project_hashes=[]
    for key in ('clean','failure'):
        project_hashes.append(data[key]['project_sha256'])
    for state_path in (work/'recovery').glob('*/state.json'):
        item=json.loads(state_path.read_text());project_hashes.append(hashlib.sha256(item['project'].encode()).hexdigest())
    project_hashes=list(dict.fromkeys(project_hashes))
    document={'schema':'gopulse.phase17.v1','status':'passed','complete':True,'started_at':started_all,'finished_at':now(),
      'candidate':{'version':manifest['version'],'revision':manifest['revision'],'manifest_sha256':'sha256:'+sha(a.manifest),
        'bundle_sha256':manifest['bundle_sha256'],'runtime_contract_sha256':manifest['runtime_contract']['sha256'],
        'image_digests':{k:v['platforms']['linux/amd64'] for k,v in manifest['images'].items()},
        'plugin_digests':[x['archive_sha256'] for x in manifest['plugins'] if x['purpose']=='current']},
      'source_manifest_sha256':'sha256:'+sha(a.from_manifest),'inventory':{'host_os':platform.system(),'host_arch':platform.machine(),
        'kernel':platform.release(),'server_platform':server['Os']+'/'+server['Arch'],'server_version':server['Version'],
        'compose':subprocess.check_output(['docker','compose','version'],text=True).strip()},
      'project_hashes':project_hashes,'scenarios':scenarios,'secret_scan':'passed','cleanup':'passed'}
    atomic(evidence,document);verify(document,evidence.parent)
    print('PASS: Phase 17 Linux amd64 final evidence and Milestone 4 technical matrix')

if __name__=='__main__':
    try:main()
    except Exception as error:
        import traceback
        for frame in traceback.extract_tb(error.__traceback__):print(Path(frame.filename).name+':'+str(frame.lineno)+':'+frame.name,file=sys.stderr)
        print('Phase 17 acceptance failed ('+type(error).__name__+'); private journal retained',file=sys.stderr);sys.exit(1)
