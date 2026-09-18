#!/usr/bin/env python3
"""Direct 1.13.6 -> 1.14.4 state acceptance; never treats source checks as candidate receipts."""
import argparse
import hashlib
import json
import os
import sys
from pathlib import Path

from release_artifacts import verify_bundle


def identity(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs(args):
    source = verify_bundle(args.from_manifest.resolve())
    target = verify_bundle(args.manifest.resolve())
    if source['version'] != '1.13.6' or target['version'] != '1.14.4':
        raise ValueError('state acceptance requires the direct 1.13.6 to 1.14.4 path')
    work = args.work.resolve()
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    if work.stat().st_mode & 0o077:
        raise ValueError('state acceptance work directory must be private')
    binding = {'from_manifest_sha256': identity(args.from_manifest),
               'manifest_sha256': identity(args.manifest)}
    path = work/'binding.json'
    if path.exists():
        if json.loads(path.read_text()) != binding:
            raise ValueError('state acceptance workspace belongs to other manifests')
    else:
        with path.open('x') as stream:
            os.chmod(path, 0o600)
            json.dump(binding, stream)
    return source, target, work, binding


def source_checkout(target, explicit=None):
    import subprocess
    root = Path(__file__).resolve().parents[2]
    paths = [explicit] if explicit else [root]
    if not explicit:
        listing = subprocess.check_output(['git','-C',str(root),'worktree','list','--porcelain'],text=True)
        paths += [Path(line[9:]) for line in listing.splitlines() if line.startswith('worktree ')]
    for path in paths:
        if path is None:continue
        path=path.resolve()
        revision=subprocess.check_output(['git','-C',str(path),'rev-parse','HEAD'],text=True).strip()
        dirty=subprocess.check_output(['git','-C',str(path),'status','--porcelain','--untracked-files=no'],text=True)
        if revision==target['revision'] and not dirty and (path/'VERSION').read_text().strip()==target['version']:
            return path
    raise ValueError('a clean candidate revision checkout is required for Compose acceptance')


def command_gate(name, command, work, binding, files, env, cwd):
    import subprocess
    import tempfile
    from verify_backup_restore import save
    from verify_current_recovery import CurrentRecovery
    signature=hashlib.sha256(b''.join(str(p).encode()+p.read_bytes() for p in files)).hexdigest()
    stamp={**binding,'implementation_sha256':signature}
    result_path=work/(name+'-receipt.json')
    if result_path.exists():
        old=json.loads(result_path.read_text())
        log=work/(name+'.log')
        if (old.get('binding')==stamp and old.get('status')=='passed'
                and log.exists() and identity(log)==old.get('log_sha256')):
            print('REUSE: '+name+' (same candidate and implementation)',flush=True)
            return old
    baseline=CurrentRecovery.resources()
    log=work/(name+'.log')
    # The business verifier snapshots even ignored files below the checkout.
    # Spool outside it until the child has completed its cleanup/snapshot check.
    with tempfile.TemporaryFile(mode='w+b', dir='/tmp') as stream:
        result=subprocess.run(command,env=env,cwd=cwd,stdout=stream,stderr=subprocess.STDOUT)
        stream.seek(0)
        with log.open('wb') as destination:
            os.chmod(log,0o600)
            destination.write(stream.read())
    preserved=CurrentRecovery.resources()==baseline
    masked='[gopulse-compose] ERROR:' in log.read_text(errors='replace')
    receipt={'binding':stamp,'status':'passed' if result.returncode==0 and preserved and not masked else 'failed',
             'exit_code':result.returncode,'resource_inventory_preserved':preserved,'log_sha256':identity(log)}
    save(result_path,receipt)
    if receipt['status']!='passed':raise RuntimeError(name+' candidate gate failed; private log retained')
    print('PASS: '+name+' candidate gate and resource inventory',flush=True)
    return receipt


def main():
    import fcntl
    import subprocess
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--from-manifest', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--candidate-source', type=Path)
    parser.add_argument('--migration-only', action='store_true',help='run the scoped data path; does not emit whole-batch success')
    args = parser.parse_args()
    source, target, work, binding = inputs(args)
    lock=(work/'.lock').open('a')
    os.chmod(work/'.lock',0o600)
    fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    from verify_current_recovery import CurrentRecovery
    from verify_phase17_migration import MigrationAcceptance
    from verify_backup_restore import save
    root=Path(__file__).resolve().parents[2]
    source_root=None if args.migration_only else source_checkout(target,args.candidate_source)
    baseline = CurrentRecovery.resources()
    receipt = {'schema': 'gopulse.phase17-state.v1', **binding,
               'from_version': source['version'], 'target_version': target['version'],
               'candidate_revision': target['revision'], 'complete': False, 'checks': {}}
    save(work/'receipt.json', receipt)
    migration_path=work/'migration-receipt.json'
    migration_stamp={**binding,'implementation_sha256':identity(root/'scripts/ci/verify_phase17_migration.py')}
    try:
        old=json.loads(migration_path.read_text()) if migration_path.exists() else {}
        if old.get('binding')==migration_stamp and old.get('status')=='passed':
            receipt['checks']['migration']=old
            print('REUSE: formal migration and recovery (same candidate and implementation)',flush=True)
        else:
            acceptance=None
            try:
                acceptance=MigrationAcceptance(args.from_manifest.resolve(),target,work/'migration')
                facts=acceptance.run()
            finally:
                if acceptance is not None:acceptance.cleanup()
            if CurrentRecovery.resources()!=baseline:raise RuntimeError('migration changed resource inventory')
            result={'binding':migration_stamp,'status':'passed','results':facts,
                    'resource_inventory_preserved':True,'secret_scan':'passed'}
            save(migration_path,result)
            receipt['checks']['migration']=result
        save(work/'receipt.json',receipt)
        if args.migration_only:
            receipt['pending']=['candidate_business','candidate_marshaller','candidate_alerts','candidate_compose']
            return 0
        env={**os.environ,'GOPULSE_RELEASE_MANIFEST':str(args.manifest.resolve())}
        shared=[root/'scripts/ci/candidate_runtime.py',root/'scripts/ci/release_artifacts.py']
        gates=[('business',[str(root/'scripts/verify-business.sh')],[root/'scripts/verify-business.sh'],root),
               ('marshaller',[str(root/'scripts/verify-marshaller.sh')],[root/'scripts/verify-marshaller.sh'],root),
               ('alerts',[str(root/'scripts/verify-alerts.sh')],[root/'scripts/ci/verify_alerts.py',root/'scripts/ci/verify_alert_sources.py',root/'scripts/ci/verify_plugin_metrics.py'],root),
               ('compose',[str(source_root/'scripts/verify-compose.sh')],[source_root/'scripts/verify-compose.sh',source_root/'scripts/verify-compose-observability.sh'],source_root)]
        for name,command,files,cwd in gates:
            receipt['checks'][name]=command_gate(name,command,work,binding,files+shared,env,cwd)
            save(work/'receipt.json',receipt)
        if CurrentRecovery.resources()!=baseline:raise RuntimeError('state acceptance changed resource inventory')
        receipt['complete']=True
        print('PASS: immutable candidate migration, messages, alerts and Compose state acceptance',flush=True)
        return 0
    finally:
        save(work/'receipt.json',receipt)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        import traceback
        # Stack locations only: never include exception text or local credentials.
        for frame in traceback.extract_tb(error.__traceback__):
            print(Path(frame.filename).name+':'+str(frame.lineno)+':'+frame.name, file=sys.stderr)
        print("State acceptance failed ("+type(error).__name__+"); private work journal retained", file=sys.stderr)
        sys.exit(1)
