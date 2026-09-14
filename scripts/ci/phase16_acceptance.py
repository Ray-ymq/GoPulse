#!/usr/bin/env python3
"""Containerized final matrix: orchestrate existing lifecycle/recovery contracts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
import sys
from types import SimpleNamespace
from phase16_evidence import SCENARIOS, SENSITIVE, atomic, now, require, sha, verify
from release_artifacts import verify_bundle
from verify_current_recovery import CurrentRecovery
from verify_backup_restore import docker


def unrelated_snapshot(destination=None):
    resources = CurrentRecovery.resources()
    objects = {}
    for kind, ids in resources.items():
        if not ids: objects[kind] = []; continue
        cmd = ['inspect'] if kind == 'containers' else [kind[:-1], 'inspect']
        values = json.loads(docker(*cmd, *ids))
        # Healthcheck clocks and logs change independently; identity/config must not.
        if kind == 'containers':
            values = [{'Id':v['Id'],'Image':v['Image'],'Config':v['Config'], 'Mounts':sorted(v['Mounts'], key=lambda m:json.dumps(m,sort_keys=True)),
                       'StartedAt':v['State']['StartedAt'], 'RestartCount':v['RestartCount']} for v in values]
        if kind == 'networks':
            values = [{k:v.get(k) for k in ('Id','Name','Driver','IPAM','Labels','Options','Scope','Internal','Attachable','EnableIPv6')} for v in values]
        objects[kind] = values
    if destination: atomic(destination, objects) # private, contains foreign Config secrets
    return hashlib.sha256(json.dumps(objects, sort_keys=True).encode()).hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest', type=Path, required=True)
    p.add_argument('--work', type=Path, required=True)
    p.add_argument('--evidence', type=Path, required=True)
    p.add_argument('--artifact-receipt', type=Path, required=True)
    a = p.parse_args()
    work = a.work.resolve(); work.mkdir(parents=True, exist_ok=True, mode=0o700)
    require(work.stat().st_mode & 0o077 == 0, 'private work directory required')
    (work/'tmp').mkdir(mode=0o700, exist_ok=True)
    evidence = a.evidence.resolve(); evidence.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    bundle = a.manifest.resolve().parent
    m = verify_bundle(a.manifest.resolve())
    require(m['version'] == '1.13.6', 'Phase16 requires 1.13.6')
    require(bundle != Path('/work'), 'independent Bundle directory required')
    identity = sha(a.manifest)
    archive = bundle/('gopulse-'+m['version']+'-bundle.tar.gz')
    server = json.loads(docker('version','--format','{{json .Server}}'))
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64' and server['Os']+'/'+server['Arch'] == 'linux/amd64', 'real Linux amd64 runtime required')
    runner = os.environ['GOPULSE_ACCEPTANCE_IMAGE']
    ref = os.environ['GOPULSE_ACCEPTANCE_REF']
    info = json.loads(docker('image','inspect',runner))[0]
    require(info['Id'] == runner and ref in info['RepoDigests'], 'runner not pinned to registry image')
    labels = info['Config']['Labels']
    require(labels['org.opencontainers.image.revision'] == m['revision'] and labels['org.opencontainers.image.version'] == m['version'], 'runner/product source mismatch')
    checkpoint = work/'matrix-progress.json'
    if checkpoint.exists():
        d = json.loads(checkpoint.read_text())
        require(d['manifest_sha256'] == identity and d['bundle_archive_sha256'] == sha(archive) and d['runner']['image'] == runner, 'progress belongs to different candidate')
    else:
        disk = os.statvfs(work)
        engine = json.loads(docker('info','--format','{{json .}}'))
        d = dict(schema='gopulse.phase16.v1', complete=False, status='incomplete', redacted=False,
                 candidate=m, manifest_sha256=identity, bundle_archive_sha256=sha(archive),
                 runner={'image':runner,'ref':ref,'revision':m['revision']}, projects={}, scenarios={},
                 started_at=now(), inventory={'host_os':platform.system(),'host_arch':platform.machine(),
                 'host_release':Path('/host/os-release').read_text(), 'kernel':platform.release(),
                 'server_os':server['Os'],'server_arch':server['Arch'], 'server_version':server['Version'],
                 'compose':docker('compose','version'), 'cpu_count':engine['NCPU'], 'memory_bytes':engine['MemTotal'],
                 'disk_free_bytes':disk.f_bavail*disk.f_frsize}, isolation_before=unrelated_snapshot(work/'isolation-before.json'))
        sentinel = work/'user-owned-sentinel'
        sentinel.write_bytes(os.urandom(64)); sentinel.chmod(0o600)
        d['sentinel_sha256'] = sha(sentinel)
        atomic(checkpoint,d)
    # Interrupted runs may resume their same-candidate private recovery journal;
    # incomplete files never receive the final completion marker.
    secrets = []
    def redact(text):
        for path in (work/'recovery').glob('*/secrets.json'):
            for key, value in json.loads(path.read_text()).items():
                if any(w in key for w in ('PASSWORD','TOKEN','SECRET')) and isinstance(value,str) and len(value)>8: secrets.append(value)
        journal = work/'recovery/acceptance.json'
        if journal.exists():
            password = json.loads(journal.read_text()).get('password')
            if password: secrets.append(password)
        for path in (work/'recovery').glob('*/state.json'):
            secrets.append(json.loads(path.read_text())['installation_token'])
        for value in set(secrets): text = text.replace(value,'[REDACTED]')
        return SENSITIVE.sub('[REDACTED]',text)
    def attach(path, name):
        target = evidence.parent/name
        text = redact(Path(path).read_text())
        target.write_text(text); target.chmod(0o600)
        return {'path':name,'sha256':sha(target)}
    def scenario(name, action):
        if name in d['scenarios']: return
        started = now()
        print('RUN: '+name, flush=True)
        facts, files = action()
        d['scenarios'][name] = dict(status='passed', started_at=started, finished_at=now(),
            manifest_sha256=identity, revision=m['revision'], bundle_archive_sha256=d['bundle_archive_sha256'],
            facts=facts, files=files)
        atomic(checkpoint,d)
        print('PASS: '+name, flush=True)
    def artifact():
        receipt = json.loads(a.artifact_receipt.read_text())
        require(receipt == {'manifest_sha256':'sha256:'+identity,'revision':m['revision'],'platform':'linux/amd64','status':'amd64-runtime-and-compose-passed'}, 'artifact/Compose receipt not passed')
        return receipt, [attach(a.artifact_receipt,'artifact-runtime.json'), attach(a.manifest,'release-manifest.json')]
    def lifecycle(mode):
        target = work/(mode+'.json')
        cmd = [sys.executable, str(Path(__file__).with_name('verify_product_lifecycle.py')), '--platform','linux/amd64', '--manifest',str(a.manifest),'--'+mode,'--evidence',str(target)]
        result = subprocess.run(cmd,capture_output=True,text=True,timeout=2400)
        log = work/(mode+'.log'); log.write_text(redact(result.stdout+result.stderr)); log.chmod(0o600)
        require(result.returncode == 0, 'lifecycle '+mode+' failed; see '+str(log))
        return json.loads(target.read_text()), [attach(target,mode+'.json'), attach(log,mode+'.log')]
    recovery = None
    def get_recovery():
        nonlocal recovery
        if recovery is None:
            recovery = CurrentRecovery(SimpleNamespace(manifest=a.manifest,work=work/'recovery',acceptance_image=runner))
        return recovery
    def current():
        r = get_recovery(); r.current_product()
        return {'completed':list(r.data['completed'])}, []
    def failures():
        r = get_recovery(); r.current_failures()
        return {'completed':list(r.data['completed']), 'failures':r.data['failures']}, []
    def cleanup():
        r = get_recovery(); r.cleanup()
        for name in ('source','target','second','negative'):
            state = r.state(name)
            d['projects'][name] = {key+'_sha256':hashlib.sha256(state[field].encode()).hexdigest() for key,field in [('project','project'),('token','installation_token')]}
        return {'completed':list(r.data['completed'])}, [attach(r.work/'evidence.json','recovery.json')]
    def final_scan():
        require(unrelated_snapshot(work/'isolation-after.json') == d['isolation_before'], 'unrelated Docker resource changed')
        require(sha(work/'user-owned-sentinel') == d['sentinel_sha256'], 'user sentinel changed')
        require(sha(a.manifest) == identity and sha(archive) == d['bundle_archive_sha256'], 'candidate drifted')
        for path in evidence.parent.iterdir():
            if path.is_file():
                text = path.read_text()
                require(redact(text) == text, 'Secret found in '+path.name)
        return {'unrelated_resources_unchanged':True, 'user_file_unchanged':True, 'candidate_unchanged':True, 'secret_scan_passed':True}, []
    def interrupted(signum, _): raise RuntimeError('matrix interrupted by signal '+str(signum))
    signal.signal(signal.SIGTERM, interrupted); signal.signal(signal.SIGINT, interrupted)
    try:
        scenario(SCENARIOS[0],artifact)
        scenario(SCENARIOS[1],lambda:lifecycle('clean-install'))
        scenario(SCENARIOS[2],lambda:lifecycle('failure-matrix'))
        scenario(SCENARIOS[3],current)
        scenario(SCENARIOS[4],failures)
        scenario(SCENARIOS[5],cleanup)
        scenario(SCENARIOS[6],final_scan)
        d.update(complete=True,status='passed',redacted=True,finished_at=now())
        verify(d,evidence.parent)
        atomic(evidence,d); atomic(checkpoint,d)
        print('PASS: complete same-candidate Phase16 matrix',flush=True)
    except Exception as exc:
        d.update(complete=False,status='incomplete',redacted=False,last_error=redact(str(exc)))
        atomic(checkpoint,d)
        print('FAIL: '+redact(str(exc)),file=sys.stderr,flush=True)
        raise SystemExit(1)

if __name__ == '__main__': main()
