"""Fail-closed Phase 16 acceptance contract, independent of the executor."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
from release_manifest import validate

SCENARIOS = ('artifact-runtime-compose', 'clean-install-lifecycle', 'linux-failure-matrix',
             'current-product', 'recovery-failures', 'owned-cleanup', 'secret-isolation')
DIGEST = re.compile(r'^[0-9a-f]{64}$')
SENSITIVE = re.compile(r'(?i)(?:-----BEGIN .*PRIVATE KEY|Bearer\s+[A-Za-z0-9._-]{16,}|(?:Acceptance|Recovery)-[a-f0-9]{12}-password|mysql-[a-f0-9]{12}|jwt-[a-f0-9]{12})')


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def atomic(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, tmp = tempfile.mkstemp(prefix='.'+path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(data, stream, indent=2, ensure_ascii=False)
            stream.write('\n'); stream.flush(); os.fsync(stream.fileno())
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp): os.unlink(tmp)


def require(value, message):
    if not value: raise ValueError(message)


def verify(document, root):
    require(document.get('schema') == 'gopulse.phase16.v1', 'unknown schema')
    require(document.get('complete') is True and document.get('status') == 'passed', 'incomplete/failed evidence')
    require(document.get('redacted') is True, 'missing secret scan')
    require(not SENSITIVE.search(json.dumps(document)), 'Secret match')
    candidate = validate(document['candidate'])
    require(candidate['version'] == '1.13.6', 'wrong candidate version')
    manifest_digest = document['manifest_sha256']
    require(DIGEST.fullmatch(manifest_digest), 'invalid manifest digest')
    require(DIGEST.fullmatch(document['bundle_archive_sha256']), 'invalid Bundle checksum')
    host = document['inventory']
    require(host['host_os'] == 'Linux' and host['host_arch'] in ('x86_64', 'amd64'), 'not Linux amd64 host')
    require(host['server_os'] == 'linux' and host['server_arch'] == 'amd64', 'not Linux amd64 Docker server')
    require(host['kernel'] and host['compose'] and host['cpu_count'] > 0 and host['memory_bytes'] > 0 and host['disk_free_bytes'] > 0, 'incomplete inventory')
    require(document['runner']['revision'] == candidate['revision'], 'runner revision differs')
    require(re.fullmatch(r'sha256:[0-9a-f]{64}', document['runner']['image']), 'mutable runner')
    require(re.fullmatch(r'.+@sha256:[0-9a-f]{64}', document['runner']['ref']), 'runner not registry pinned')
    identities = document['projects']
    require(set(identities) == {'source','target','second','negative'}, 'missing recovery projects')
    for field in ('project_sha256','token_sha256'):
        values = [i[field] for i in identities.values()]
        require(all(DIGEST.fullmatch(v) for v in values) and len(set(values)) == 4, 'projects not isolated')
    scenes = document['scenarios']
    require(set(scenes) == set(SCENARIOS), 'missing/unknown scenario')
    loaded = {}
    root = Path(root).resolve()
    for name, scene in scenes.items():
        require(scene['status'] == 'passed', 'failed scenario: '+name)
        require(scene['manifest_sha256'] == manifest_digest, 'mixed candidate: '+name)
        require(scene['revision'] == candidate['revision'] and scene['bundle_archive_sha256'] == document['bundle_archive_sha256'], 'mixed source/Bundle')
        start, end = [datetime.datetime.fromisoformat(scene[k]) for k in ('started_at','finished_at')]
        require(start.tzinfo and end.tzinfo and end >= start, 'invalid scenario window')
        require(scene['facts'], 'missing scenario facts')
        for ref in scene['files']:
            path = (root/ref['path']).resolve()
            require(path.is_relative_to(root) and path.is_file(), 'missing/unsafe evidence file')
            require(sha(path) == ref['sha256'], 'evidence checksum mismatch')
            text = path.read_text()
            require(not SENSITIVE.search(text), 'Secret in evidence attachment')
            if path.suffix == '.json': loaded[ref['path']] = json.loads(text)
    require(loaded['release-manifest.json'] == candidate, 'manifest attachment differs from candidate')
    require(sha(root/'release-manifest.json') == manifest_digest, 'manifest identity mismatch')
    runtime = loaded['artifact-runtime.json']
    require(runtime == {'manifest_sha256':'sha256:'+manifest_digest, 'revision':candidate['revision'], 'platform':'linux/amd64', 'status':'amd64-runtime-and-compose-passed'}, 'runtime gate did not pass same candidate')
    for mode in ('clean-install','failure-matrix'):
        receipt = loaded[mode+'.json']
        require(receipt['status'] == 'passed' and receipt['manifest_sha256'] == manifest_digest and receipt['mode'] == mode, 'lifecycle not passed')
        require(receipt['isolation_preserved'] is True and receipt['cleanup_passed'] is True, 'lifecycle cleanup missing')
        require(receipt['commands'], 'missing actual lifecycle calls')
    recovery = loaded['recovery.json']
    require(recovery['status'] == 'passed' and recovery['manifest_sha256'] == manifest_digest, 'recovery not passed')
    require(recovery['candidate']['revision'] == candidate['revision'], 'mixed recovery revision')
    require(recovery['candidate']['images'] == candidate['images'] and recovery['candidate']['plugins'] == candidate['plugins'], 'mixed recovery artifacts')
    for step in ('current-product-two-restores-passed','current-product-failure-matrix-passed', 'owned-cleanup-and-isolation-passed',
                 'wrong-passphrase-and-tamper-rejected-before-resources', 'source-three-source-facts',
                 'target-nonempty-restore-rejected','second-nonempty-restore-rejected'):
        require(step in recovery['completed'], 'missing recovery outcome: '+step)
    for alias in ('source','target','second'):
        browser = recovery['browsers'][alias]
        require(set(browser['checks']) == {'desktop','narrow','three-source-create'} and browser['timezone'] == 'Asia/Shanghai', 'incomplete browser matrix')
        require(browser['runner_image'] == document['runner']['image'] and browser['product_revision'] == candidate['revision'] and browser['runner_revision'] == candidate['revision'], 'mixed browser candidate')
    require(set(recovery['three_sources']['source']) >= {'metrics','logs','events'}, 'missing real three-source incidents')
    require(set(recovery['failures']) == {'import','interrupt'}, 'missing recovery failures')
    require(set(recovery['restores']) == {'target','second'} and set(recovery['backups']) == {'source','target'}, 'missing A/B/C loop')
    for result in recovery['restores'].values(): require(result['facts_verified'] is True, 'restore facts not verified')
    require(scenes['secret-isolation']['facts']['unrelated_resources_unchanged'] is True, 'unrelated resources changed')
    return document
