#!/usr/bin/env python3
"""Validate the closed runtime inventory against source, Compose and env inputs."""
import argparse
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON key')
        result[key] = value
    return result

def load(path):
    return json.loads(Path(path).read_text(), object_pairs_hook=unique)

def schema_check(value, spec):
    types = {'object': dict, 'array': list, 'string': str, 'integer': int, 'boolean': bool, 'null': type(None)}
    kinds = spec.get('type', [])
    if isinstance(kinds, str):
        kinds = [kinds]
    if kinds and type(value) not in [types[k] for k in kinds]:
        raise ValueError('schema type mismatch')
    if 'const' in spec and value != spec['const']:
        raise ValueError('schema constant mismatch')
    if 'enum' in spec and value not in spec['enum']:
        raise ValueError('schema enum mismatch')
    if isinstance(value, dict):
        if not set(spec.get('required', [])) <= value.keys():
            raise ValueError('schema required field missing')
        for key, entry in value.items():
            if key not in spec.get('properties', {}):
                if spec.get('additionalProperties') is False:
                    raise ValueError('schema unknown field')
            else:
                schema_check(entry, spec['properties'][key])
    if isinstance(value, list):
        for entry in value:
            schema_check(entry, spec.get('items', {}))
    if isinstance(value, str) and 'pattern' in spec and not re.fullmatch(spec['pattern'], value):
        raise ValueError('schema pattern mismatch')
    if type(value) is int and value < spec.get('minimum', value):
        raise ValueError('schema range mismatch')

def compose_document(path):
    return json.loads(subprocess.check_output(['docker', 'compose', '-f', str(path), 'config', '--no-interpolate', '--format', 'json'], text=True))

def sensitive(key):
    return any(s in key for s in ('PASSWORD', 'TOKEN', 'SECRET', 'DSN')) or key == 'RABBITMQ_URL'

def validate(contract, compose, env, root=ROOT, check_version=True):
    schema_check(contract, load(root/'deploy/runtime-contracts.schema.json'))
    source = (root/'componentmetrics/runtime.go').read_text()
    registered = {name: (int(port), int(budget)) for name, port, budget in re.findall(r'"([a-z-]+)":\s*\{(\d+),\s*(\d+)\}', source)}
    components = contract['components']
    if len(components) != 12 or len({c['id'] for c in components}) != 12 or {c['id'] for c in components} != set(registered):
        raise ValueError('component inventory mismatch')
    if check_version and contract['product_version'] != (root/'VERSION').read_text().strip():
        raise ValueError('product version mismatch')
    if f'const RuntimeContractVersion = "{contract["contract_version"]}"' not in (root/'componentmetrics/probe.go').read_text():
        raise ValueError('implementation contract version mismatch')
    env_keys = {line.split('=', 1)[0] for line in env.splitlines() if '=' in line and not line.startswith('#')}
    entries = contract['env_example']
    if len(entries) != len(env_keys) or {v['key'] for v in entries} != env_keys:
        raise ValueError('env example key drift')
    for field in entries:
        if sensitive(field['key']) and not field['sensitive']:
            raise ValueError('secret declared public')
    ports = set()
    for c in components:
        if not (root/c['entrypoint']).is_file() or not (root/c['module']/'go.mod').is_file():
            raise ValueError('missing process entrypoint')
        code = (root/c['entrypoint']).read_text()
        if c['id'].endswith('-exporter'):
            if 'componentmetrics.ServeRuntime' not in code:
                raise ValueError('exporter runtime adapter missing')
        elif 'StartConfiguredWithProbes' not in code:
            raise ValueError('private probe adapter missing')
        loader = '\n'.join(p.read_text() for p in (root/c['configuration']).glob('*.go') if not p.name.endswith('_test.go'))
        if f'ValidateRuntimeEnvironment("{c["id"]}")' not in loader:
            raise ValueError('typed configuration adapter missing')
        if (c['listeners'][0]['port'], c['shutdown_seconds']) != registered[c['id']]:
            raise ValueError('source port or shutdown drift')
        for listener in c['listeners']:
            if listener['port'] in ports or listener['scope'] != 'private':
                raise ValueError('duplicate or public listener port')
            ports.add(listener['port'])
        if c['probes'] != dict(startup='/startup', live='/live', ready='/ready', health='/health'):
            raise ValueError('probe path conflict')
        if c['max_concurrent_checks'] != 1 or c['cache_ms'] != 250 or c['check_timeout_ms'] != {'router':5000, 'marshaller':2000}.get(c['id'],1000):
            raise ValueError('unbounded probe policy')
        if c['stop_grace_seconds'] <= c['shutdown_seconds']:
            raise ValueError('insufficient stop grace')
        fields = {f['key']: f for f in c['environment']}
        if len(fields) != len(c['environment']):
            raise ValueError('duplicate environment key')
        for f in fields.values():
            if sensitive(f['key']) and (not f['sensitive'] or f['default'] is not None):
                raise ValueError('unsafe secret metadata')
            if len(f['aliases']) > 1:
                raise ValueError('multiple compatibility aliases')
            for alias in f['aliases']:
                if not alias.get('expires') or tuple(map(int, alias['expires'].split('.'))) <= tuple(map(int, contract['product_version'].split('.'))):
                    raise ValueError('alias has no future expiry')
        if not c['compose_service'] and c['stop_grace_seconds'] != registered['monitor'][1]:
            raise ValueError('supervisor deadline drift')
        if c['compose_service']:
            service = compose['services'][c['compose_service']]
            owned = {k: str(v) for k, v in service.get('environment', {}).items()}
            recorded = {k: f['compose_value'] for k, f in fields.items() if f['compose_value'] is not None}
            if recorded != owned:
                raise ValueError('unregistered or changed Compose-owned environment key')
            health = service.get('healthcheck', {}).get('test', [])
            if not any(f':{c["listeners"][0]["port"]}/ready' in part for part in health):
                raise ValueError('Docker readiness healthcheck drift')
            if service.get('stop_grace_period') != str(c['stop_grace_seconds'])+'s':
                raise ValueError('Compose stop grace drift')
    plugin = contract['plugin_manifest']
    validator = (root/plugin['validator']).read_text()
    if 'manifest.HealthPath != "/health"' not in validator or 'manifest.MetricsPath != "/metrics"' not in validator:
        raise ValueError('plugin manifest path drift')
    catalog = (root/plugin['catalog']).read_text()
    if 'Port: 9121 + i' not in catalog:
        raise ValueError('plugin listener catalog drift')
    for c in components:
        if c['plugin_id'] and '"'+c['id'].removesuffix('-exporter')+'"' not in catalog:
            raise ValueError('plugin omitted')
    return contract

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--contract', type=Path, default=ROOT/'deploy/runtime-contracts.json')
    parser.add_argument('--compose', type=Path, default=ROOT/'deploy/compose.yaml')
    parser.add_argument('--env', type=Path, default=ROOT/'.env.example')
    args = parser.parse_args()
    validate(load(args.contract), compose_document(args.compose), args.env.read_text())
    print('Runtime contract: 12 processes, configuration, private probes, budgets and plugin catalog verified.')

if __name__ == '__main__':
    main()
