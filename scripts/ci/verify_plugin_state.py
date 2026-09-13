#!/usr/bin/env python3
"""Real Redis + Monitor offline transfer check, NOT a product backup gate.

No router is configured: Monitor's built-in discard publisher is used. This
proves plugin collection and re-materialization, not downstream history recovery.
Secret-bearing pipe contents and container diagnostics are never printed.
"""
import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
LABEL = 'io.gopulse.portable-test'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--monitor-image', required=True, help='local immutable sha256 image ID')
    parser.add_argument('--evidence', type=Path, required=True)
    args = parser.parse_args()
    if not args.monitor_image.startswith('sha256:'):
        parser.error('an immutable local image ID is required')
    token = secrets.token_hex(32)
    prefix = 'gopulse-portable-' + token[:12]
    resources = []

    def docker(*argv, data=None, expect=0):
        result = subprocess.run(['docker', *argv], input=data, capture_output=True, timeout=120)
        if result.returncode != expect:
            raise RuntimeError('scoped Docker operation failed (raw diagnostics suppressed)')
        return result.stdout

    def create(kind, name, *opts):
        if kind == 'container':
            docker('run', '-d', '--name', name, '--label', LABEL+'='+token, *opts)
        else:
            docker(kind, 'create', '--label', LABEL+'='+token, name)
        resources.append((kind, name))

    def wait_for(fn):
        deadline = time.monotonic()+60
        while time.monotonic() < deadline:
            try:
                value = fn()
                if value:
                    return value
            except (OSError, ValueError, urllib.error.URLError, RuntimeError):
                pass
            time.sleep(0.5)
        raise RuntimeError('scoped readiness/collection timeout')

    server = json.loads(docker('version', '--format', '{{json .Server}}'))
    if server['Os']+'/'+server['Arch'] != 'linux/amd64':
        raise RuntimeError('real Linux amd64 Docker server required')
    image = json.loads(docker('image', 'inspect', args.monitor_image))[0]
    if image['Os']+'/'+image['Architecture'] != 'linux/amd64':
        raise RuntimeError('Linux amd64 Monitor image required')
    lock = json.loads((ROOT/'deploy/release/third-party.lock.json').read_text())['redis']
    redis = lock['ref'].split('@')[0]+'@'+lock['platforms']['linux/amd64']
    docker('pull', '--platform', 'linux/amd64', redis)
    source, target = prefix+'-source', prefix+'-target'
    source_volume, target_volume = prefix+'-source-data', prefix+'-target-data'
    plugin_root = '/var/lib/gopulse-monitor/plugins'
    api_token, log_token, password = (secrets.token_hex(32) for _ in range(3))
    evidence = None
    try:
        with tempfile.TemporaryDirectory(prefix=prefix) as tmp:
            envfile = Path(tmp)/'monitor.env'
            with open(envfile, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as output:
                output.write('\n'.join([
                    'GOPULSE_RUNTIME_MODE=container', 'MONITOR_HTTP_HOST=0.0.0.0', 'GOPULSE_VERSION=1.13.4',
                    'MONITOR_API_TOKEN='+api_token, 'LOG_MONITOR_INGEST_TOKEN='+log_token,
                    'MONITOR_BOOTSTRAP_PACKAGE=/opt/gopulse/packages/gopulse-redis-exporter.tar.gz',
                    'MONITOR_PLUGIN_ROOT='+plugin_root, 'REDIS_HOST=redis', 'REDIS_PORT=6379',
                    'REDIS_DB=0', 'REDIS_PASSWORD='+password,
                    'MONITOR_SCRAPE_INTERVAL=2s', 'MONITOR_SCRAPE_TIMEOUT=1s',
                ] + [name+'_METRICS_TOKEN='+secrets.token_hex(32) for name in
                     ('MONITOR', 'BACKEND', 'BUSINESS_WORKER', 'SEARCH_INDEXER', 'ROUTER', 'MARSHALLER')])+'\n')
            redisconf = Path(tmp)/'redis.conf'
            redisconf.write_text('requirepass '+password+'\n'); redisconf.chmod(0o600)
            create('network', prefix)
            create('volume', source_volume)
            create('volume', target_volume)
            # A private mounted config, not process arguments, supplies Redis credentials.
            create('container', prefix+'-redis', '--network', prefix, '--network-alias', 'redis',
                   '--user', str(os.getuid())+':'+str(os.getgid()),
                   '-v', str(redisconf)+':/private/redis.conf:ro', redis, 'redis-server', '/private/redis.conf')

            def start(name, volume):
                create('container', name, '--network', prefix, '--read-only', '--tmpfs', '/tmp:rw,exec',
                       '--env-file', str(envfile), '-v', volume+':'+plugin_root,
                       '-p', '127.0.0.1::9090', args.monitor_image)
                obj = json.loads(docker('container', 'inspect', name))[0]
                port = obj['NetworkSettings']['Ports']['9090/tcp'][0]['HostPort']
                base = 'http://127.0.0.1:'+port

                def request(path, method='GET'):
                    req = urllib.request.Request(base+path, data=b'' if method=='POST' else None,
                          headers={'Authorization': 'Bearer '+api_token}, method=method)
                    with urllib.request.urlopen(req, timeout=3) as response:
                        return json.load(response)
                wait_for(lambda: request('/health'))
                return request

            api = start(source, source_volume)
            status_path = '/internal/v1/exporter-plugins/redis-exporter'
            before = wait_for(lambda: (s if (s := api(status_path)['data'])['last_success_at'] else None))
            # A second process must not export the live runtime's volume.
            base = ['run', '--rm', '-i', '--network', 'none', '--read-only', '--tmpfs', '/tmp',
                    '-v', source_volume+':'+plugin_root, args.monitor_image, 'plugin-state', 'export', '--root', plugin_root]
            docker(*base, expect=1)
            docker('stop', '--time', '20', source)
            wire = docker(*base)
            transfer = json.loads(wire)
            public = json.dumps(transfer['public'])
            for forbidden in (password, api_token, log_token, plugin_root, 'executable_path', '"pid"'):
                if forbidden in public:
                    raise RuntimeError('private/runtime state found in public export')
            if len(transfer['public']['plugins']) != 1:
                raise RuntimeError('unexpected plugin count')
            # Import into a new volume; executable bytes come only from image catalog.
            restore = ['run', '--rm', '-i', '--network', 'none', '--read-only', '--tmpfs', '/tmp',
                       '-v', target_volume+':'+plugin_root, args.monitor_image, 'plugin-state', 'import', '--root', plugin_root]
            docker(*restore, data=wire)
            docker(*restore, data=wire, expect=1)  # cannot overwrite existing target
            restored_api = start(target, target_volume)
            after = wait_for(lambda: (s if (s := restored_api(status_path)['data'])['last_success_at'] else None))
            for field in ('id', 'version', 'desired_state', 'installed_at', 'updated_at'):
                if before[field] != after[field]:
                    raise RuntimeError('restored logical identity mismatch')
            if after['observed_state'] != 'running' or after['last_success_at'] <= before['last_success_at']:
                raise RuntimeError('restored plugin did not resume actual collection')
            evidence = {'schema': 1, 'scope': 'Monitor/Redis offline state only; discard publisher; not product backup acceptance',
                        'platform': 'linux/amd64', 'monitor_image': image['Id'], 'redis_image': redis,
                        'catalog_digest': transfer['public']['catalog_digest'], 'plugins': 1,
                        'live_export_rejected': True, 'nonempty_import_rejected': True,
                        'logical_identity_preserved': True, 'new_collection': True}
            del wire, transfer
    finally:
        cleanup_ok = True
        for kind, name in reversed(resources):
            try:
                obj = json.loads(docker(kind, 'inspect', name))[0]
                labels = obj['Config'].get('Labels', {}) if kind == 'container' else obj.get('Labels', {})
                if labels.get(LABEL) != token:
                    cleanup_ok = False
                    continue
                docker(kind, 'rm', *(['-f', '-v'] if kind == 'container' else []), name)
            except RuntimeError:
                cleanup_ok = False
        if not cleanup_ok:
            raise RuntimeError('scoped cleanup incomplete; inspect only resources labelled '+LABEL)
    if evidence is not None:
        evidence['owned_resources_removed'] = True
        args.evidence.parent.mkdir(parents=True, exist_ok=True)
        args.evidence.write_text(json.dumps(evidence, indent=2)+'\n')
        print('Monitor/Redis portable state check passed (not a product backup gate)')


if __name__ == '__main__':
    try:
        main()
    except RuntimeError as exc:
        raise SystemExit(str(exc)) from None
    except Exception as exc:
        raise SystemExit("Monitor portable state check failed ("+type(exc).__name__+"); secret-bearing diagnostics suppressed") from None
