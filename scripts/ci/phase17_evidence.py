#!/usr/bin/env python3
"""Phase 17 same-candidate evidence contract and verifier."""
import datetime
import hashlib
import json
import re
from pathlib import Path

REQUIRED = {
    'release-runtime-compose', 'runtime-contract-probes-signals',
    'lifecycle-clean-install', 'lifecycle-failure-cleanup',
    'migration-direct-predecessor', 'rabbit-reliability',
    'kafka-reliability', 'alert-reliability',
    'complete-product-permissions', 'backup-restore-current',
    'secret-ownership-cleanup',
}
DIGEST = re.compile(r'^[0-9a-f]{64}$')
REVISION = re.compile(r'^[0-9a-f]{40}$')
SENSITIVE = re.compile(r'(?i)(password|secret|token|cookie|authorization|mysql://|amqp://)["\s:=]+[^\s,}\]]{8,}')


def secret_scan_text(raw):
    """Return JSON scalar text while excluding required-env templates."""
    try:
        document = json.loads(raw)
    except json.JSONDecodeError:
        return raw
    values = []
    def collect(value, key=''):
        if isinstance(value, dict):
            for child_key, child in value.items():
                collect(child, str(child_key))
        elif isinstance(value, list):
            for child in value:
                collect(child, key)
        elif isinstance(value, str) and not ('${' in value and '}' in value):
            values.append(key+': '+value)
    collect(document)
    return '\n'.join(values)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def atomic(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    tmp = path.with_suffix(path.suffix+'.tmp')
    tmp.write_text(json.dumps(value, indent=2, ensure_ascii=False)+'\n')
    tmp.chmod(0o600)
    tmp.replace(path)


def verify(document, root):
    root = Path(root).resolve()
    if document.get('schema') != 'gopulse.phase17.v1' or document.get('status') != 'passed' or not document.get('complete'):
        raise ValueError('Phase 17 evidence is not atomically complete')
    candidate = document.get('candidate', {})
    if candidate.get('version') != '1.14.5' or not REVISION.fullmatch(candidate.get('revision','')):
        raise ValueError('invalid final candidate')
    for key in ('manifest_sha256','bundle_sha256','runtime_contract_sha256'):
        value = candidate.get(key,'').removeprefix('sha256:')
        if not DIGEST.fullmatch(value): raise ValueError('invalid candidate digest: '+key)
    inventory = document.get('inventory', {})
    if inventory.get('host_os') != 'Linux' or inventory.get('host_arch') != 'x86_64' or inventory.get('server_platform') != 'linux/amd64':
        raise ValueError('real Linux amd64 inventory required')
    scenarios = document.get('scenarios', {})
    if set(scenarios) != REQUIRED: raise ValueError('missing or unknown Phase 17 scenario')
    manifest = candidate['manifest_sha256'].removeprefix('sha256:')
    attachments = {}
    for name, scene in scenarios.items():
        if scene.get('status') != 'passed' or scene.get('reason_code') != 'accepted':
            raise ValueError('failed scenario: '+name)
        if scene.get('manifest_sha256') != manifest or scene.get('revision') != candidate['revision']:
            raise ValueError('mixed candidate: '+name)
        start = datetime.datetime.fromisoformat(scene['started_at'])
        end = datetime.datetime.fromisoformat(scene['finished_at'])
        if not start.tzinfo or not end.tzinfo or end < start: raise ValueError('invalid scenario window')
        if not scene.get('facts'): raise ValueError('missing scenario facts')
        for ref in scene.get('attachments', []):
            path = (root/ref['path']).resolve()
            if not path.is_relative_to(root) or not path.is_file() or sha(path) != ref['sha256']:
                raise ValueError('unsafe or changed attachment')
            raw = path.read_text(errors='replace')
            # Runtime contracts intentionally contain required-env templates
            # such as ${AUTH_JWT_SECRET:?...}; variable names are not values.
            if SENSITIVE.search(secret_scan_text(raw)): raise ValueError('sensitive value in attachment')
            attachments[ref['path']] = json.loads(raw) if path.suffix == '.json' else raw
    if attachments.get('attachments/release-manifest.json', {}).get('revision') != candidate['revision']:
        raise ValueError('manifest attachment mismatch')
    if sha(root/'attachments/release-manifest.json') != manifest:
        raise ValueError('manifest digest mismatch')
    if document.get('secret_scan') != 'passed' or document.get('cleanup') != 'passed':
        raise ValueError('secret scan or cleanup missing')
    if not document.get('project_hashes') or len(set(document['project_hashes'])) != len(document['project_hashes']):
        raise ValueError('isolated project hashes required')
    return document
