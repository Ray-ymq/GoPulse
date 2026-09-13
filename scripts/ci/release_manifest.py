"""Strict release validation, using only the closed subset of JSON Schema we emit."""
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLATFORMS = ('linux/amd64', 'linux/arm64')
PRODUCTS = ('backend', 'business-worker', 'search-indexer', 'frontend', 'admin-frontend', 'router', 'marshaller', 'monitor', 'redis-exporter')
SOURCES = ('redis', 'mysql', 'rabbitmq', 'kafka', 'elasticsearch', 'victoriametrics')


def sha(data):
    return 'sha256:' + hashlib.sha256(data).hexdigest()


def unique(pairs):
    result = {}
    for k, v in pairs:
        if k in result:
            raise ValueError(f'duplicate JSON key: {k}')
        result[k] = v
    return result


def load(path):
    return json.loads(Path(path).read_text(), object_pairs_hook=unique)


def check_schema(value, schema):
    if 'anyOf' in schema:
        for candidate in schema['anyOf']:
            try:
                check_schema(value, candidate)
                return
            except ValueError:
                pass
        raise ValueError('no matching schema alternative')
    if 'const' in schema and (type(value) is not type(schema['const']) or value != schema['const']):
        raise ValueError('incorrect constant')
    if 'enum' in schema and value not in schema['enum']:
        raise ValueError('unknown enum value')
    kind = schema.get('type')
    if kind and type(value) is not {'string': str, 'object': dict, 'array': list}[kind]:
        raise ValueError('incorrect value type')
    if kind == 'object':
        if not set(schema['required']).issubset(value) or not set(value).issubset(schema['properties']):
            raise ValueError('missing or unknown required field')
        for k, v in value.items():
            check_schema(v, schema['properties'][k])
    if kind == 'array':
        if not schema.get('minItems', 0) <= len(value) <= schema.get('maxItems', 100000):
            raise ValueError('incorrect collection size')
        for v in value:
            check_schema(v, schema['items'])
    if 'pattern' in schema and re.fullmatch(schema['pattern'], value) is None:
        raise ValueError('invalid identity or digest')


def validate(m, version=None, revision=None):
    check_schema(m, load(ROOT / 'deploy/release/release-manifest.schema.json'))
    if version is not None and m['version'] != version:
        raise ValueError('version mismatch')
    if revision is not None and m['revision'] != revision:
        raise ValueError('revision mismatch')
    for image in [*m['images'].values(), *m['third_party'].values(), m['lifecycle']]:
        if ':latest' in image['ref'].split('@')[0] or len(set(image['platforms'].values())) != len(image['platforms']):
            raise ValueError('mutable tag or duplicate platform digest')
    supported = set(m['lifecycle']['platforms'])
    if any(set(i['platforms']) != supported for i in m['images'].values()):
        raise ValueError('product platform sets differ')
    seen, current, legacy = set(), set(), 0
    for p in m['plugins']:
        key = (p['id'], p['version'], p['arch'])
        if key in seen:
            raise ValueError('duplicate plugin identity')
        seen.add(key)
        if p['purpose'] == 'current':
            if p['version'] != m['version'] or not p['schema_sha256']:
                raise ValueError('current plugin version/schema mismatch')
            current.add((p['id'], p['arch']))
        else:
            if key != ('redis-exporter', '1.9.4', 'amd64') or p['schema_sha256']:
                raise ValueError('invalid legacy upgrade input')
            legacy += 1
    if current != {(s+'-exporter', a) for s in SOURCES for a in (p.split('/')[1] for p in supported)} or legacy != 1:
        raise ValueError('incomplete plugin catalogs')
    return m


def payload_digest(files):
    # Canonical POSIX-relative paths + raw file digests. Manifest/checksums excluded.
    return sha(''.join(f'{sha(data)[7:]}  {name}\n' for name, data in sorted(files.items())).encode())
