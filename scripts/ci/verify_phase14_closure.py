#!/usr/bin/env python3
"""Final cross-batch contract; invoked after the full Compose/platform gate.

Uses current images and the registered Phase 13 Redis fixture, never retags or
mutates an existing image/volume. Previous batch package tests are not rerun.
"""
import hashlib
import json
import re
import signal
import time

from verify_plugin_metrics import ROOT, PATTERN, Client, command, wait_until
from verify_component_metrics import ComponentAcceptance, COMPONENTS
from verify_plugin_isolation import IsolationAcceptance, SOURCES, REPRESENTATIVE

VERSION = '1.11.6'


def cleanup_orphan_volumes(project, preexisting):
    """Compose drops formerly mounted volumes from its active removal graph."""
    assert PATTERN.fullmatch(project), 'invalid closure project'
    names = command(['docker', 'volume', 'ls', '-q', '--filter',
                     'label=com.docker.compose.project='+project]).stdout.decode().split()
    # Validate the complete removal set before deleting anything.
    for name in names:
        assert name not in preexisting and name.startswith(project+'_'), 'refuse pre-existing or unowned volume'
        info = json.loads(command(['docker', 'volume', 'inspect', name]).stdout)[0]
        assert info.get('Labels', {}).get('com.docker.compose.project') == project, 'volume ownership mismatch'
    for name in names:
        command(['docker', 'volume', 'rm', name])
    assert not command(['docker', 'volume', 'ls', '-q', '--filter',
                        'label=com.docker.compose.project='+project]).stdout.strip(), 'owned volume remains'


class ClosureAcceptance(ComponentAcceptance):
    processes = IsolationAcceptance.processes
    all_fresh = IsolationAcceptance.all_fresh
    scrape = IsolationAcceptance.scrape
    upload_raw = IsolationAcceptance.upload_raw

    def __init__(self):
        super().__init__()
        self.tag = 'p1406-' + self.token
        self.images = []
        values = dict(line.split('=', 1) for line in self.env_file.read_text().splitlines() if '=' in line)
        values.update(GOPULSE_VERSION=VERSION, GOPULSE_IMAGE_TAG=self.tag,
                      GOPULSE_UPDATE_VERSION='1.11.7')
        self.env_file.write_text(''.join(f'{k}={v}\n' for k, v in values.items()))
        for service in (*COMPONENTS, 'frontend', 'acceptance'):
            self.override['services'].setdefault(service, {})['image'] = 'gopulse/'+service+':'+self.tag
        self.override['services']['monitor-acceptance'] = {
            'image': 'gopulse/monitor-acceptance:'+self.tag,
            'profiles': ['acceptance'],
            'build': {'context': str(ROOT), 'dockerfile': 'deploy/docker/observability.Dockerfile',
                      'target': 'monitor-acceptance', 'args': {'ACCEPTANCE_UPDATE_VERSION': '1.11.7', 'VERSION': VERSION, 'REVISION': values['GOPULSE_REVISION']}}}
        self.save_override()
        self.evidence_file = self.work/'closure-evidence.json'
        (self.work/'snapshot.json').write_text(json.dumps({k: sorted(v) for k, v in self.snapshot.items()}, indent=2))
        print('Phase 14 owned project: '+self.project, flush=True)

    def build(self):
        for service in (*COMPONENTS, 'frontend', 'acceptance', 'monitor-acceptance'):
            image = 'gopulse/'+service+':'+self.tag
            assert command(['docker', 'image', 'inspect', image], check=False).returncode != 0
            self.images.append(image)
        # Frontend Dockerfile runs tests, typecheck and build against this source.
        result = self.compose('--profile', 'acceptance', 'build', *COMPONENTS, 'frontend',
                              'acceptance', 'monitor-acceptance', timeout=1800, check=False)
        (self.work/'build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode == 0, 'current image build failed; see owned build.log'
        artifacts = {}
        for image in self.images:
            info = json.loads(command(['docker', 'image', 'inspect', image]).stdout)[0]
            assert info['Config']['Labels']['org.opencontainers.image.version'] == VERSION
            artifacts[image] = info['Id']
        self.record('current product and acceptance images built; Frontend tests/typecheck/build', artifacts)

    def monitor_volume(self, volume, legacy=False, acceptance=False):
        image = ('gopulse/monitor:1.10.6' if legacy else
                 'gopulse/'+('monitor-acceptance' if acceptance else 'monitor')+':'+self.tag)
        self.override['services']['monitor'].update(image=image, volumes=[volume+':/var/lib/gopulse-monitor/plugins'],
            environment={'MONITOR_BOOTSTRAP_PACKAGE': '/opt/gopulse/packages/gopulse-redis-exporter.tar.gz' if legacy else ''})
        self.save_override()
        if self.started:
            self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
            self.healthy('monitor')

    def package(self, source, filename=None):
        return self.compose('exec', '-T', 'monitor', 'cat', '/opt/gopulse/packages/'+
                            (filename or 'gopulse-'+source+'-exporter.tar.gz')).stdout

    def migration(self):
        legacy_digest = hashlib.sha256(self.package('redis')).hexdigest()
        assert legacy_digest == 'b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727'
        self.record('registered Phase 13 fixture digest verified', {'archive_sha256': legacy_digest})
        before = wait_until(lambda: self.status() if self.status().get('last_success_at') else None, 'legacy Redis scrape')
        history = wait_until(self.metric, 'legacy Redis history')[-1]['timestamp']
        post = self.user.request('posts', 'POST', {'title': 'migration-'+self.token, 'content': 'preserved business data'}, 201)['data']
        self.monitor_volume('monitor_plugin_data')
        migrated = self.status()
        for key in ('version', 'installed_at', 'updated_at', 'desired_state'):
            assert before[key] == migrated[key]
        assert migrated['version'] == '1.10.6' and migrated['observed_state'] == 'running'
        self.upload_raw('redis', self.package('redis'), 200)
        assert self.status()['version'] == VERSION
        wait_until(lambda: any(p['timestamp'] > history for p in self.metric()), 'v2 historical continuity')
        assert any(p['timestamp'] <= history for p in self.metric())
        self.compose('restart', 'monitor'); self.healthy('monitor')
        assert len(self.internal()['data']) == 1
        assert self.user.request('posts/'+str(post['id']))['data']['id'] == post['id']
        self.monitor_volume('p14_stopped', legacy=True)
        self.internal('/redis-exporter/stop', 'POST')
        stopped = self.status()
        self.monitor_volume('p14_stopped')
        for key in ('version', 'installed_at', 'updated_at', 'desired_state'):
            assert stopped[key] == self.status()[key]
        self.upload_raw('redis', self.package('redis'), 200)
        self.admin.request('exporter-plugins/redis-exporter/configuration', 'PUT', self.config('redis'))
        assert self.status()['observed_state'] == 'stopped'
        assert self.compose('exec', '-T', 'monitor', 'test', '!', '-e',
            '/var/lib/gopulse-monitor/plugins/redis-exporter/runtime/process.json', check=False).returncode == 0
        self.record('real Phase 13 running/stopped volumes migrate; v2 explicit update; history and business preserved',
                    {'legacy_version': before['version'], 'current_version': VERSION, 'retry_records': 1})
        self.monitor_volume('p14_empty', acceptance=True)
        assert self.internal()['data'] == []

    def browser_spec(self, spec, extra=()):
        result = command(['docker', 'run', '--rm', '--label', 'com.docker.compose.project='+self.project,
            '--network', self.project+'_edge', '-e', 'GOPULSE_BASE_URL=http://frontend:8080',
            '-e', 'GOPULSE_P14_ADMIN='+self.admin_name, '-e', 'GOPULSE_P14_PASSWORD='+self.auth_password,
            '-e', 'GOPULSE_P14_SECRET='+self.secret, *extra,
            'gopulse/acceptance:'+self.tag, 'e2e/'+spec], timeout=240, check=False)
        output = (result.stdout+result.stderr).decode(errors='replace')
        for secret in (self.auth_password, self.secret):
            output = output.replace(secret, '[REDACTED]')
        (self.work/(spec+'.log')).write_text(output)
        assert result.returncode == 0, 'browser failed; see owned redacted '+spec+'.log'
        self.record('browser '+spec, {'passed': True})

    def cold_start(self):
        archive = self.work/'closure-redis.tar.gz'
        archive.write_bytes(self.package('redis', 'redis-update.tar.gz')); archive.chmod(0o644)
        self.browser_spec('phase14-closure.spec.ts', ['-v', str(archive)+':/work/packages/closure-redis.tar.gz:ro'])
        wait_until(self.offsets, 'formal consumer offsets before Kafka collector')
        for source in ('mysql', 'rabbitmq'):
            self.reconciler.reconcile(source)
            before = self.account(source)
            self.reconciler.reconcile(source)
            assert before == self.account(source)
        for source in ('kafka', 'elasticsearch', 'victoriametrics'):
            self.admin.request('exporter-plugins/'+source+'-exporter/install', 'POST', self.config(source), 201)
        self.processes()
        assert self.status_for('redis')['version'] == '1.11.7'
        values = {}
        for source in SOURCES:
            wait_until(lambda s=source: self.metric_for(s) and self.metric_for(s)[-1]['value'] == 1, source+' up=1')
            values[source] = wait_until(lambda s=source: self.metric_for(s, REPRESENTATIVE[s]), source+' real value')[-1]['value']
        self.record('official package digests', {s: hashlib.sha256(self.package(s)).hexdigest() for s in SOURCES})
        self.record('empty volume six independent official plugins; real queries; idempotent dedicated accounts', values)

    def volume_recovery(self):
        self.admin.request('exporter-plugins/elasticsearch-exporter/stop', 'POST')
        before = {s: self.file(s+'-exporter/active.json') for s in SOURCES}
        secrets = {}
        for s in SOURCES:
            revision = json.loads(before[s])['revision']
            secrets[s] = hashlib.sha256(self.file(s+'-exporter/revisions/'+revision+'/secret.json')).hexdigest()
        self.monitor_volume('p14_empty', acceptance=True)
        for s in SOURCES:
            assert self.file(s+'-exporter/active.json') == before[s]
            revision = json.loads(before[s])['revision']
            assert hashlib.sha256(self.file(s+'-exporter/revisions/'+revision+'/secret.json')).hexdigest() == secrets[s]
            wait_until(lambda s=s: self.status_for(s)['observed_state'] == ('stopped' if s == 'elasticsearch' else 'running'), s+' desired restore')
        assert self.compose('exec', '-T', 'monitor', 'test', '!', '-e',
            '/var/lib/gopulse-monitor/plugins/elasticsearch-exporter/runtime/process.json', check=False).returncode == 0
        self.admin.request('exporter-plugins/elasticsearch-exporter/start', 'POST')
        self.processes(); self.all_fresh()
        self.record('same volume replacement preserves six revisions/Secrets/releases and stopped state', {'targets': 6})

    def faults(self):
        before = self.processes()
        self.compose('stop', 'redis')
        try:
            wait_until(lambda: self.metric_for('redis')[-1]['value'] == 0, 'Redis persisted up=0')
            self.all_fresh(('redis',))
            self.user.request('posts/'+str(self.post_id))
        finally:
            self.compose('start', 'redis'); self.healthy('redis')
        self.all_fresh(); assert self.processes() == before
        self.record('non-storage target outage: other five and business continue; no Exporter restart', {'target': 'redis'})
        self.compose('exec', '-T', 'monitor', 'kill', '-KILL', str(before['victoriametrics']['pid']))
        wait_until(lambda: self.status_for('victoriametrics')['observed_state'] == 'failed', 'Exporter unexpected exit')
        for s in SOURCES:
            if s != 'victoriametrics':
                assert json.loads(self.file(s+'-exporter/runtime/process.json')) == before[s]
        self.all_fresh(('victoriametrics',))
        self.admin.request('exporter-plugins/victoriametrics-exporter/start', 'POST')
        self.processes()
        active = self.file('victoriametrics-exporter/active.json')
        self.upload_raw('victoriametrics', self.package('victoriametrics', 'victoriametrics-failure.tar.gz'), 422)
        assert self.file('victoriametrics-exporter/active.json') == active
        self.all_fresh()
        self.record('single process failure/recovery and trusted higher-version trial rollback', {'failure_version': '1.11.90'})
        before = self.processes()
        self.compose('stop', 'router')
        try:
            self.user.request('posts/'+str(self.post_id))
            time.sleep(12)
            assert self.processes() == before
            assert self.endpoint('monitor')['status'] == 200
        finally:
            self.compose('start', 'router'); self.healthy('router')
        self.all_fresh()
        self.record('shared Router outage preserves collectors and business; new snapshots after recovery', {'publisher': 'existing synchronous bounded HTTP transport; no new queue'})
        self.endpoint_fault()
        self.storage_fault()

    def final_security(self):
        catalog = self.admin.request('observability/metrics/catalog')['data']
        for s in SOURCES:
            entries = [d for d in catalog if d['source'] == s]
            assert entries and all(d['producer_kind'] == 'exporter_plugin' and d['producer_id'] == s+'-exporter' for d in entries)
        for path in ('observability/metrics?metric=unknown&range=15m',
                     'observability/metrics?metric=gopulse_redis_up&range=999m',
                     'observability/metrics?metric=gopulse_redis_up&range=15m&label.unknown=value'):
            self.admin.request(path, expected=400)
        for path in ('exporter-plugins', 'exporter-plugins/catalog', 'observability/metrics/catalog'):
            self.user.request(path, expected=403)
        wait_until(lambda: self.admin.request('observability/logs?service=backend')['data'], 'final Logs')
        events = wait_until(lambda: self.admin.request('observability/events')['data'], 'final Events')
        public = [json.dumps(self.admin.request('exporter-plugins')).encode(), json.dumps(catalog).encode(),
                  json.dumps(events).encode(), self.compose('logs', '--no-color', *COMPONENTS).stdout]
        for s in SOURCES:
            public.append(self.file(s+'-exporter/active.json'))
            revision = json.loads(self.file(s+'-exporter/active.json'))['revision']
            public.append(self.file(s+'-exporter/revisions/'+revision+'/revision.json'))
        for secret in (self.secret, self.auth_password, *self.tokens.values(),
                       self.account('mysql')['password'], self.account('rabbitmq')['password'],
                       self.config('victoriametrics')['secrets']['password']):
            assert all(secret.encode() not in raw for raw in public), 'credential scan failed: public/runtime category'
        self.record('catalog provenance, unauthorized/invalid queries, Logs/Events and public credential scan', {'passed': True})

    def run(self):
        self.build()
        self.monitor_volume('monitor_plugin_data', legacy=True)
        self.started = True
        self.compose('up', '-d', '--no-build', timeout=420)
        for s in ('frontend', 'backend', 'monitor', 'marshaller'): self.healthy(s)
        port = self.compose('port', 'frontend', '8080').stdout.decode().strip()
        assert re.fullmatch(r'127\.0\.0\.1:[0-9]+', port)
        self.admin, self.user = Client('http://'+port), Client('http://'+port)
        for client, name in ((self.admin, self.admin_name), (self.user, self.user_name)):
            client.request('auth/register', 'POST', {'username': name, 'password': self.auth_password}, 201)
        self.compose('exec', '-T', 'backend', '/usr/local/bin/admin-role', 'promote', '--username', self.admin_name)
        self.migration()
        self.cold_start()
        self.business()
        self.six_components()
        self.security_and_cardinality()
        self.browser_spec('phase14-components.spec.ts')
        self.volume_recovery()
        self.faults()
        self.consumer_shutdown()
        self.final_security()
        self.record('Phase 14 cross-batch runtime gates passed', {'version': VERSION})

    def cleanup(self):
        super().cleanup()
        cleanup_orphan_volumes(self.project, set(self.snapshot['volume']))
        for image in self.images:
            if command(['docker', 'image', 'inspect', image], check=False).returncode == 0:
                command(['docker', 'image', 'rm', image])
        self.record('owned Compose resources cleaned; pre-existing resources preserved', {'project': self.project})


def main():
    acceptance = ClosureAcceptance()
    def interrupted(*_):
        raise RuntimeError('Phase 14 closure interrupted')
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    try:
        acceptance.run()
    finally:
        acceptance.cleanup()


if __name__ == '__main__':
    main()
