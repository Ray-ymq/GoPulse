"""Phase-14-04: one owned six-plugin product, with scoped faults and cleanup.

Build current product/monitor-acceptance images first. No synthetic VM writes,
product trust bypass, global daemon changes or external-resource mutations.
"""
import concurrent.futures
import io
import json
import tarfile
import time
import uuid

from verify_plugin_metrics import Client, ROOT, command, wait_until
from verify_plugin_topology import TopologyAcceptance

VERSION = '1.11.4'
SOURCES = ('redis', 'mysql', 'rabbitmq', 'kafka', 'elasticsearch', 'victoriametrics')
VM_FAMILIES = 'up rows_inserted_total query_requests_total active_timeseries storage_rows storage_size_bytes free_disk_space_bytes active_merges storage_rows_deleted_total'.split()
REPRESENTATIVE = dict(zip(SOURCES, ('used_memory_bytes', 'uptime_seconds', 'connections', 'brokers', 'documents', 'storage_rows')))


def self_test():
    assert len(SOURCES) == len(set(SOURCES)) == 6
    assert len(VM_FAMILIES) == 9 and 'retention_deletions_total' not in VM_FAMILIES


class IsolationAcceptance(TopologyAcceptance):
    def __init__(self):
        super().__init__()
        self.override['services'] = {s: {'image': 'gopulse/'+s+':'+VERSION}
                                     for s in ('frontend', 'backend', 'router', 'marshaller', 'monitor')}
        self.override['services']['monitor']['environment'] = {
            'MONITOR_BOOTSTRAP_PACKAGE': '', 'MONITOR_ROUTER_URL': 'http://fault-router:9091'}
        # This helper can only reject one fixed source; all accepted messages go
        # through the production Router's real auth, Kafka and Marshaller path.
        self.fault = self.work/'fault'
        self.fault.mkdir()
        (self.fault/'source').write_text('')
        command(['env', 'CGO_ENABLED=0', 'go', 'build', '-o', str(self.work/'fault-router'),
                 str(ROOT/'scripts/ci/testdata/plugin-fault-router.go')])
        self.override['services']['fault-router'] = {
            'image': 'golang:1.26.0-alpine3.23', 'entrypoint': ['/probe/fault-router'],
            'volumes': [str(self.work/'fault-router')+':/probe/fault-router:ro', str(self.fault)+':/fault:ro'],
            'networks': ['observability'], 'read_only': True, 'cap_drop': ['ALL']}
        self.save_override()

    def config(self, source):
        if source == 'victoriametrics':
            return {'config': {'host': source, 'port': 8428, 'username': 'vm_'+self.token,
                               'connect_timeout': '1s', 'scrape_timeout': '2s'},
                    'secrets': {'password': 'vm-'+self.token+'-012345678901234567890123456789'}}
        if source == 'redis':
            return {'config': {'host': 'redis', 'port': 6379, 'database': 0,
                               'connect_timeout': '1s', 'scrape_timeout': '2s'},
                    'secrets': {'password': self.secret}}
        return super().config(source)

    def scrape(self, source):
        port = 9121+SOURCES.index(source)
        self.owned_id('monitor')
        raw = self.compose('exec', '-T', 'monitor', 'sh', '-c',
            '{ cat; sleep 3; } | nc -w 4 127.0.0.1 "$1"', 'probe', str(port),
            data=b'GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n').stdout
        head, body = raw.split(b'\r\n\r\n', 1)
        return int(head.split()[1]), body

    def processes(self):
        records = {s: json.loads(self.file(s+'-exporter/runtime/process.json')) for s in SOURCES}
        assert len({r['pid'] for r in records.values()}) == 6
        # The record alone is not a uniqueness proof. Inspect only this owned
        # container's process list and compare all actual executable identities.
        cid = self.owned_id('monitor')
        processes = json.loads(command(['docker', 'inspect', cid]).stdout)[0]
        assert processes['State']['Running']
        listing = self.compose('exec', '-T', 'monitor', 'sh', '-c',
                              'for p in /proc/[0-9]*/exe; do readlink "$p" || true; done').stdout.decode()
        for s in SOURCES:
            assert sum(line.endswith('/bin/gopulse-'+s+'-exporter') for line in listing.splitlines()) == 1
        return records

    def all_fresh(self, excluded=()):
        before = {s: (self.status_for(s)['last_success_at'], self.metric_for(s)[-1]['timestamp'])
                  for s in SOURCES if s not in excluded}
        for s, (success, at) in before.items():
            # query_range evaluates a moving time grid: a newer query timestamp
            # alone is not evidence that the collector actually sampled again.
            wait_until(lambda s=s, success=success: (self.status_for(s)['last_success_at'] or '') > (success or ''),
                       s+' new successful collection')
            wait_until(lambda s=s, at=at: self.new_metric(s, 'up', at, 1), s+' fresh Backend point')
            assert self.status_for(s)['observed_state'] == 'running'
        self.record('unaffected collectors sampled and published again', {'sources': list(before)})
        return list(before)

    def vm_mapping(self):
        # Query the pinned VM's own endpoint from the owned container. The shell
        # reads its existing Docker Secret; credentials never enter evidence.
        raw = self.compose('exec', '-T', 'victoriametrics', 'sh', '-c',
            'wget -q -O - --header="Authorization: Basic $(printf \'%s:%s\' "$VICTORIAMETRICS_USERNAME" "$(cat /run/secrets/victoriametrics_password)" | base64 | tr -d \'\\n\')" http://127.0.0.1:8428/metrics').stdout.decode()
        code, body = self.scrape('victoriametrics')
        assert code == 200 and body.count(b'# TYPE ') == 9, ('VM scrape', code, len(body), body.count(b'# TYPE '))
        samples = dict(line.rsplit(' ', 1) for line in body.decode().splitlines() if line and not line.startswith('#'))
        assert set(samples) == {'gopulse_victoriametrics_'+f for f in VM_FAMILIES}
        assert all(float(v) >= 0 for v in samples.values()) and '{' not in body.decode()
        # Record only selected safe values, never the raw upstream response.
        assert 'vm_cache_entries{type="storage/hour_metric_ids"}' in raw
        assert 'vm_rows_deleted_total{type="storage/small"}' in raw
        for f in VM_FAMILIES:
            wait_until(lambda f=f: self.metric_for('victoriametrics', f), 'VM Backend '+f)
        catalog = self.admin.request('observability/metrics/catalog')['data']
        assert not any('retention_deletions_total' in str(item) for item in catalog)
        # A numeric-only upstream excerpt explains the mapping without exposing
        # paths or the unbounded rest of /metrics. Stable deletion counters must
        # match; other counters/gauges may move between the two HTTP requests.
        upstream = {}
        for line in raw.splitlines():
            if line.startswith(('vm_rows_deleted_total{type="storage/',
                                'vm_cache_entries{type="storage/hour_metric_ids"}',
                                'vm_http_requests_total{path="/api/v1/query"}',
                                'vm_http_requests_total{path="/api/v1/query_range"}')):
                key, value = line.rsplit(' ', 1)
                upstream[key] = float(value)
        deleted = sum(value for key, value in upstream.items() if key.startswith('vm_rows_deleted_total{'))
        assert float(samples['gopulse_victoriametrics_storage_rows_deleted_total']) == deleted
        self.record('locked VM nine-family snapshot through real storage',
                    {'exported': {k: float(v) for k, v in samples.items()}, 'selected_upstream': upstream})

    def upload_raw(self, source, raw, expected):
        boundary='p1404'+uuid.uuid4().hex
        data=(f'--{boundary}\r\nContent-Disposition: form-data; name="package"; filename="plugin.tar.gz"\r\nContent-Type: application/gzip\r\n\r\n'.encode()+raw+f'\r\n--{boundary}--\r\n'.encode())
        return self.admin.request('exporter-plugins/'+source+'-exporter/update', 'POST', data, expected,
                                 {'Content-Type': 'multipart/form-data; boundary='+boundary})

    def run(self):
        self.started = True
        self.compose('up', '-d', '--no-build', timeout=360)
        for s in ('frontend', 'backend', 'monitor', 'marshaller'):
            self.healthy(s)
        base = 'http://'+self.compose('port', 'frontend', '8080').stdout.decode().strip()
        self.admin, self.user = Client(base), Client(base)
        for client, name in ((self.admin, self.admin_name), (self.user, self.user_name)):
            client.request('auth/register', 'POST', {'username': name, 'password': self.auth_password}, 201)
        self.compose('exec', '-T', 'backend', '/usr/local/bin/admin-role', 'promote', '--username', self.admin_name)
        self.admin.request('exporter-plugins/redis-exporter/install', 'POST', self.config('redis'), 201)
        wait_until(self.offsets, 'formal Marshaller offset')
        for s in ('mysql', 'rabbitmq'):
            self.reconciler.reconcile(s)
        for s in ('kafka', 'elasticsearch', 'victoriametrics'):
            self.admin.request('exporter-plugins/'+s+'-exporter/connection-test', 'POST', self.config(s))
            self.admin.request('exporter-plugins/'+s+'-exporter/install', 'POST', self.config(s), 201)
        for s in SOURCES:
            assert self.status_for(s)['observed_state'] == 'running'
            wait_until(lambda s=s: self.metric_for(s), s+' up')
            wait_until(lambda s=s: self.metric_for(s, REPRESENTATIVE[s]), s+' runtime value')
            for method, suffix in (('GET', ''), ('POST', '/connection-test'), ('POST', '/install'), ('PUT', '/configuration'), ('POST', '/start'), ('POST', '/stop'), ('POST', '/update')):
                self.user.request('exporter-plugins/'+s+'-exporter'+suffix, method, expected=403)
        self.user.request('observability/metrics?metric=gopulse_victoriametrics_up&range=15m', expected=403)
        self.processes()
        self.vm_mapping()
        self.mark('six production packages, unique processes and local endpoints, six up/runtime Backend queries; ordinary-user management denied')
        post = self.admin.request('posts', 'POST', {'title': 'phase1404 '+self.token, 'content': 'retained '+self.token}, 201)['data']
        wait_until(lambda: self.admin.request('search/posts?q='+self.token)['data'], 'business search')
        # Invalid VM credentials are checked without mutation or leaking the candidate.
        bad = self.config('victoriametrics'); bad['secrets']['password'] = 'wrong-'+self.token
        response = self.admin.request('exporter-plugins/victoriametrics-exporter/connection-test', 'POST', bad, 422)
        assert 'wrong-'+self.token not in json.dumps(response)
        pointer = self.file('victoriametrics-exporter/active.json')
        self.admin.request('exporter-plugins/victoriametrics-exporter/configuration', 'PUT', bad, 422)
        assert self.file('victoriametrics-exporter/active.json') == pointer
        self.admin.request('exporter-plugins/victoriametrics-exporter/connection-test', 'POST', self.config('victoriametrics'))
        # Only the dedicated RabbitMQ collector credential is changed, not a
        # broker or business account. All other five continue storing new points.
        before = self.processes()
        try:
            self.compose('exec', '-T', 'rabbitmq', 'rabbitmqctl', 'change_password', 'gopulse_metrics', 'wrong-'+self.token)
            wait_until(lambda: self.status_for('rabbitmq').get('last_error', {}).get('code') == 'network_failed', 'safe target failure')
            code, body = self.scrape('rabbitmq')
            assert code == 503 and body == b'# TYPE gopulse_rabbitmq_up gauge\ngopulse_rabbitmq_up 0\n'
            self.all_fresh(('rabbitmq',))
        finally:
            self.compose('exec', '-T', 'rabbitmq', 'rabbitmqctl', 'change_password', 'gopulse_metrics', self.account('rabbitmq')['password'])
        wait_until(lambda: not self.status_for('rabbitmq').get('last_error'), 'target credential recovery')
        assert self.processes() == before
        self.mark('single-target authentication failure and same-process recovery; other five new metrics; wrong VM candidate is safe and atomic')
        # Single unexpected process exit; collect fresh points from the other five.
        record = before['victoriametrics']
        self.compose('exec', '-T', 'monitor', 'kill', '-KILL', str(record['pid']))
        wait_until(lambda: self.status_for('victoriametrics')['observed_state'] == 'failed', 'unexpected exit')
        self.all_fresh(('victoriametrics',))
        self.admin.request('exporter-plugins/victoriametrics-exporter/start', 'POST')
        self.processes()
        self.mark('unexpected VM Exporter exit affects one ID only; restart restores one process')
        # Fault injection uses the acceptance-only trusted release catalog.
        self.override['services']['monitor']['image'] = 'gopulse/monitor-acceptance:'+VERSION
        self.save_override()
        self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        self.processes()
        pointer = self.file('victoriametrics-exporter/active.json')
        self.upload_for('victoriametrics', 'victoriametrics-failure.tar.gz', 422)
        assert self.file('victoriametrics-exporter/active.json') == pointer
        self.all_fresh(('victoriametrics',))
        # Self-consistent metadata/hash but unregistered version must never execute.
        raw = self.compose('exec', '-T', 'monitor', 'cat', '/opt/gopulse/packages/victoriametrics-update.tar.gz').stdout
        source = tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz')
        dest = io.BytesIO()
        with tarfile.open(fileobj=dest, mode='w:gz') as out:
            for member in source.getmembers():
                payload = source.extractfile(member).read()
                if member.name == 'plugin.json':
                    manifest = json.loads(payload); manifest['version'] = '1.11.89'
                    payload = json.dumps(manifest).encode(); member.size = len(payload)
                out.addfile(member, io.BytesIO(payload))
        process = self.file('victoriametrics-exporter/runtime/process.json')
        self.upload_raw('victoriametrics', dest.getvalue(), 400)
        assert self.file('victoriametrics-exporter/runtime/process.json') == process
        self.upload_for('victoriametrics', 'victoriametrics-update.tar.gz', 200)
        assert self.status_for('victoriametrics')['version'] == '1.11.5'
        self.mark('trusted higher failure rolls back; unregistered consistent package rejected before execution; trusted success update')
        # Overlap a VM trial update with another ID lifecycle and reads.
        # Deterministic same-ID serialization is tested at the operation token.
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            # A failed trial is naturally bounded but may be too short for a
            # deterministic overlapping request. Same-ID serialization itself is
            # proven by TestSixPluginOperationIsolation at the token boundary.
            update = pool.submit(self.upload_for, 'victoriametrics', 'victoriametrics-failure.tar.gz', 422)
            self.admin.request('exporter-plugins/elasticsearch-exporter/stop', 'POST')
            self.admin.request('exporter-plugins/elasticsearch-exporter/start', 'POST')
            assert self.status_for('redis')['observed_state'] == 'running'
            update.result()
        self.processes()
        # Selectively reject one collector at the transport boundary; all other
        # sources still traverse the same real Router and storage pipeline.
        before = self.processes()
        (self.fault/'source').write_text('victoriametrics')
        wait_until(lambda: self.status_for('victoriametrics').get('last_error', {}).get('code') == 'publish_failed', 'directed publish failure')
        self.all_fresh(('victoriametrics',))
        (self.fault/'source').write_text('')
        wait_until(lambda: not self.status_for('victoriametrics').get('last_error'), 'directed publish recovery')
        assert self.processes() == before
        self.mark('per-ID operations and directed publish failure preserve other collectors and five fresh Backend sources')
        # Shared Router failure cannot promise fresh persisted metrics. It must
        # not stop any exporter/collector, and publication must recover in place.
        self.compose('stop', 'router')
        try:
            for s in SOURCES:
                wait_until(lambda s=s: self.status_for(s).get('last_error', {}).get('code') == 'publish_failed', s+' shared publish failure')
            assert self.processes() == before
            self.admin.request('posts/'+str(post['id']))
        finally:
            self.compose('start', 'router'); self.healthy('router')
        self.all_fresh()
        assert self.processes() == before
        self.mark('shared Router failure degrades publication only; all six survive and recover without process replacement')
        # VM is both target and storage. No assertion invents outage-period up=0.
        history_cutoff = self.metric_for('victoriametrics', 'up')[-1]['timestamp']
        service_ids = {s: self.owned_id(s) for s in ('monitor', 'marshaller')}
        self.compose('stop', 'victoriametrics')
        try:
            wait_until(lambda: self.status_for('victoriametrics').get('last_error', {}).get('code') == 'network_failed', 'VM safe target status while storage down')
            self.admin.request('observability/metrics?metric=gopulse_victoriametrics_up&range=15m', expected=503)
            code, body = self.scrape('victoriametrics')
            assert code == 503 and body == b'# TYPE gopulse_victoriametrics_up gauge\ngopulse_victoriametrics_up 0\n'
            assert self.processes() == before
            self.admin.request('posts/'+str(post['id']))
        finally:
            self.compose('start', 'victoriametrics'); self.healthy('victoriametrics')
        self.all_fresh()
        self.vm_mapping()
        assert self.processes() == before
        assert service_ids == {s: self.owned_id(s) for s in service_ids}
        assert any(p['timestamp'] <= history_cutoff and p['value'] == 1 for p in self.metric_for('victoriametrics', 'up'))
        self.mark('VM outage: safe Monitor status and unavailable queries, no fabricated persisted up=0; same-process recovery and retained history')
        # Restart with a damaged single desired record. Others must reconcile.
        pointer = self.file('mysql-exporter/active.json')
        self.compose('exec', '-T', 'monitor', 'sh', '-c', 'printf broken > /var/lib/gopulse-monitor/plugins/mysql-exporter/active.json')
        self.compose('restart', 'monitor'); self.healthy('monitor')
        for s in SOURCES:
            if s != 'mysql':
                wait_until(lambda s=s: self.status_for(s)['observed_state'] == 'running', s+' independent restart')
        assert not any(s['id'] == 'mysql-exporter' and s['observed_state'] == 'running' for s in self.internal()['data'])
        self.compose('exec', '-T', 'monitor', 'sh', '-c', 'cat > /var/lib/gopulse-monitor/plugins/mysql-exporter/active.json', data=pointer)
        self.compose('restart', 'monitor'); self.healthy('monitor')
        self.processes(); self.all_fresh()
        self.mark('stable-catalog restart: damaged ID does not block other five; repaired desired state restores exactly six processes')
        wait_until(lambda: self.admin.request('observability/logs')['data'], 'Logs regression')
        events = wait_until(lambda: self.admin.request('observability/events')['data'], 'Events regression')
        vm_events = wait_until(lambda: self.admin.request('observability/events?plugin_id=victoriametrics-exporter')['data'], 'VM scoped lifecycle events')
        assert all(e['metadata']['plugin_id'] == 'victoriametrics-exporter' for e in vm_events)
        self.record('VM scoped lifecycle events', {'names': sorted({e['event_name'] for e in vm_events})})
        assert self.offsets()
        self.admin.request('posts/'+str(post['id']))
        wait_until(lambda: self.admin.request('search/posts?q='+self.token)['data'], 'search regression')
        self.browser_vm()
        snapshots = [json.dumps(self.admin.request('exporter-plugins/catalog')).encode(),
                     json.dumps(self.admin.request('exporter-plugins')).encode(), json.dumps(events).encode(),
                     self.compose('logs', '--no-color', 'monitor', 'backend', 'router', 'marshaller').stdout]
        for s in SOURCES:
            active = json.loads(self.file(s+'-exporter/active.json'))['revision']
            for name in ('config.json', 'revision.json'):
                snapshots.append(self.file(s+'-exporter/revisions/'+active+'/'+name))
            mode = self.compose('exec', '-T', 'monitor', 'stat', '-c', '%a', '/var/lib/gopulse-monitor/plugins/'+s+'-exporter/revisions/'+active+'/secret.json').stdout.strip()
            assert mode == b'600'
        for secret in (self.secret, self.auth_password, self.account('mysql')['password'], self.account('rabbitmq')['password'], self.config('victoriametrics')['secrets']['password'], 'wrong-'+self.token):
            assert all(secret.encode() not in value for value in snapshots)
        public = snapshots[0]+snapshots[1]+snapshots[2]
        for forbidden in (b'"pid"', b'/var/lib/gopulse', b'"executable_path"'):
            assert forbidden not in public
        self.mark('Logs/Events, formal offsets, business/search, browser VM lifecycle and credential/public-identity scans')

    def browser_vm(self):
        result=command(['docker', 'run', '--rm', '--network', self.project+'_edge',
            '-e', 'GOPULSE_BASE_URL=http://frontend:8080', '-e', 'GOPULSE_P14_ADMIN='+self.admin_name,
            '-e', 'GOPULSE_P14_PASSWORD='+self.auth_password,
            '-e', 'GOPULSE_VM_USER='+self.config('victoriametrics')['config']['username'],
            '-e', 'GOPULSE_VM_PASSWORD='+self.config('victoriametrics')['secrets']['password'],
            '-v', str(ROOT/'frontend/e2e/phase14-isolation.spec.ts')+':/work/frontend/e2e/phase14-isolation.spec.ts:ro',
            'gopulse/acceptance:1.10.6', 'e2e/phase14-isolation.spec.ts'], timeout=180, check=False)
        output=(result.stdout+result.stderr).decode(errors='replace')
        for secret in (self.auth_password, self.config('victoriametrics')['secrets']['password']):
            output=output.replace(secret, '[REDACTED]')
        (self.work/'browser.log').write_text(output)
        assert result.returncode == 0, 'VM browser failed; see redacted browser.log'
