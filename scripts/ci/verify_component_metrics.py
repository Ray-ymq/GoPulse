#!/usr/bin/env python3
"""Phase-19-01 fixed component acceptance; never mutate pre-existing resources.

Build the seven current product images as 2.1.1 before running. Uses the actual
Compose product, two user identities and six official plugins. The only helper
fault is a transparent, owned-network proxy for one Backend metrics endpoint.
"""
import argparse
import datetime
import json
import os
import re
import signal
import time
import uuid

from verify_plugin_metrics import ROOT, Client, command, wait_until
from verify_plugin_topology import TopologyAcceptance
from verify_plugin_isolation import SOURCES, REPRESENTATIVE

VERSION = '2.1.1'
COMPONENTS = ('backend', 'business-worker', 'search-indexer', 'monitor', 'router', 'marshaller')
QUERIES = {
 'backend': ('http_requests_total', 'outbox_last_publish_success_timestamp_seconds'),
 'business-worker': ('messages_total', 'last_success_timestamp_seconds'),
 'search-indexer': ('messages_total', 'last_success_timestamp_seconds'),
 'monitor': ('scrapes_total', 'last_scrape_success_timestamp_seconds'),
 'router': ('messages_total', 'last_kafka_ack_timestamp_seconds'),
 'marshaller': ('records_total', 'last_storage_success_timestamp_seconds'),
}
BUDGETS = dict(zip(COMPONENTS, (5616, 37, 24, 157, 120, 310)))
COMPONENT_FAMILY_COUNT = 54
LATENCY_BUCKETS = ('0.005', '0.01', '0.025', '0.05', '0.1', '0.25', '0.5', '1', '2', '5', '10', '+Inf')
CATALOG_ENDPOINT_ENV = (
    'BACKEND_ENDPOINTS=backend,backend-2',
    'BUSINESS_WORKER_ENDPOINTS=business-worker,business-worker-2',
    'SEARCH_INDEXER_ENDPOINTS=search-indexer,search-indexer-2',
    'ROUTER_ENDPOINTS=router,router-2',
    'MARSHALLER_ENDPOINTS=marshaller,marshaller-2',
)


def self_test():
    assert len(COMPONENTS) == len(set(COMPONENTS)) == 6
    assert set(QUERIES) == set(BUDGETS) == set(COMPONENTS)
    assert all(len(queries) == 2 for queries in QUERIES.values())
    # Use the production catalog, not a separately maintained fixture list.
    specs = json.loads(command(['env', *CATALOG_ENDPOINT_ENV, 'go', 'run', './cmd/catalog'], timeout=60,
                               cwd=ROOT/'componentmetrics').stdout)
    assert {s['ID']: s['MaxSamples'] for s in specs} == BUDGETS
    generated = (ROOT/'admin-frontend/src/services/componentMetrics.ts').read_text()
    line = next(line for line in generated.splitlines() if line.startswith('export const componentContracts:'))
    actual = json.loads(line.split(' = ', 1)[1])
    expected = {}
    for s in specs:
        for f in s['Families']:
            contract = dict(source=s['ID'], kind=f['Kind'], unit=f['Unit'], keys=f['Keys'] or [], tuples=f['Tuples'])
            if f.get('Distribution'):
                distribution = f['Distribution']
                contract['distribution'] = {
                    'name': distribution['Name'], 'role': distribution['Role'], 'buckets': distribution['Buckets']
                }
            expected[f['Name']] = contract
    assert actual == expected
    label_line = next(line for line in (ROOT/'admin-frontend/src/services/management.ts').read_text().splitlines()
                       if line.startswith('const labelKeys ='))
    browser_labels = set(re.findall(r"'([^']+)'", label_line))
    component_labels = {key for family in expected.values() for key in family['keys']}
    assert component_labels - {'le'} <= browser_labels, sorted(component_labels - browser_labels - {'le'})
    assert all(not any(key in ('source', 'target_id', 'producer_kind', 'producer_id', 'user_id') for key in f['Keys'] or [])
               for s in specs for f in s['Families'])
    backend = next(s for s in specs if s['ID'] == 'backend')
    distributions = [f['Distribution'] for f in backend['Families'] if f.get('Distribution')]
    assert {(d['Name'], d['Role']) for d in distributions} == {
        ('gopulse_backend_http_request_duration_seconds', 'bucket'),
        ('gopulse_backend_http_request_duration_seconds', 'count'),
        ('gopulse_backend_http_request_duration_seconds', 'sum'),
    }
    assert {tuple(d['Buckets']) for d in distributions} == {LATENCY_BUCKETS}
    assert {f['Name'] for f in backend['Families']} >= {
        'gopulse_backend_http_requests_in_flight',
        'gopulse_backend_http_concurrency_limit',
        'gopulse_backend_http_rejected_total',
    }
    assert all(set(f['Keys'] or []) <= {'method', 'route', 'status_class', 'le', 'alert_source', 'dependency'}
               for f in backend['Families'])
    print('PASS: six fixed identities, budgets, distribution contract and browser contract match production catalog; no Docker access')


# Keep command output safe, while permitting the local catalog command's cwd.
from verify_plugin_metrics import command as _command

def command(args, *, cwd=None, **kwargs):
    if cwd is not None:
        args = ['sh', '-c', 'cd "$1" && shift && exec "$@"', 'command', str(cwd), *args]
    return _command(args, **kwargs)


class ComponentAcceptance(TopologyAcceptance):
    def __init__(self):
        super().__init__()
        values = dict(line.split('=', 1) for line in self.env_file.read_text().splitlines() if '=' in line)
        values.update(GOPULSE_VERSION=VERSION, GOPULSE_IMAGE_TAG=VERSION, GOPULSE_UPDATE_VERSION=VERSION)
        self.tokens = {c: values[c.upper().replace('-', '_')+'_METRICS_TOKEN'] for c in COMPONENTS}
        assert len(set(self.tokens.values())) == 6 and all(len(v) >= 32 for v in self.tokens.values())
        self.env_file.write_text(''.join(f'{k}={v}\n' for k, v in values.items()))
        self.override['services'] = {s: {'image': 'gopulse/'+s+':'+VERSION} for s in (*COMPONENTS, 'frontend')}
        self.override['services']['monitor']['environment'] = {'MONITOR_BOOTSTRAP_PACKAGE': ''}
        self.fault = self.work/'component-fault'
        self.fault.mkdir()
        binary = self.work/'component-probe'
        command(['env', 'CGO_ENABLED=0', 'go', 'build', '-o', str(binary), str(ROOT/'scripts/ci/testdata/component-probe.go')])
        self.override['services']['component-probe'] = {
            'image': 'golang:1.26.0-alpine3.23', 'entrypoint': ['/probe/component-probe', 'serve'],
            'volumes': [str(binary)+':/probe/component-probe:ro', str(self.fault)+':/fault:ro'],
            'networks': ['observability'], 'read_only': True, 'cap_drop': ['ALL']}
        self.save_override()
        self.evidence_file = self.work/'component-evidence.json'

    def record(self, name, values):
        self.evidence.append({'step': name, 'at': time.time(), 'values': values})
        self.evidence_file.write_text(json.dumps(self.evidence, indent=2))
        print('PASS: '+name, flush=True)

    def config(self, source):
        if source == 'redis':
            return {'config': {'host': 'redis', 'port': 6379, 'database': 0, 'connect_timeout': '1s', 'scrape_timeout': '2s'}, 'secrets': {'password': self.secret}}
        if source == 'victoriametrics':
            return {'config': {'host': source, 'port': 8428, 'username': 'vm_'+self.token, 'connect_timeout': '1s', 'scrape_timeout': '2s'},
                    'secrets': {'password': 'vm-'+self.token+'-012345678901234567890123456789'}}
        return super().config(source)

    def endpoint(self, component, *, auth='valid', method='GET', path='/internal/v1/metrics', body=''):
        self.owned_id('component-probe')
        headers = [] if auth == 'missing' else ['Bearer '+(self.tokens[component] if auth in ('valid', 'duplicate') else 'wrong-token')]
        if auth == 'duplicate': headers *= 2
        response = self.compose('exec', '-T', 'component-probe', '/probe/component-probe', data=json.dumps(dict(Component=component, Method=method, Path=path, Body=body, Authorization=headers)).encode())
        result = json.loads(response.stdout)
        assert not any(token in result['body'] for token in self.tokens.values())
        return result

    def query(self, component, suffix):
        return self.admin.request('observability/metrics?metric=gopulse_'+component.replace('-', '_')+'_'+suffix+'&range=15m')['data']

    def values(self, component, suffix, **labels):
        return [p['value'] for s in self.query(component, suffix)['series'] if all(s['labels'].get(k) == v for k, v in labels.items()) for p in s['points'][-1:]]

    def positive(self, component, suffix, **labels):
        # A scalable component has one logical producer identity but can emit
        # the same stored series from two replicas.  Progress counters and
        # timestamps are proven by any positive point in the fresh acceptance
        # window; exact current-value assertions remain in values().
        data = self.query(component, suffix)
        values = [point['value'] for series in data['series']
                  if all(series['labels'].get(k) == v for k, v in labels.items())
                  for point in series['points']]
        return values if values and max(values) > 0 else None

    def business(self):
        me = self.admin.request('users/me')['data']
        post = self.admin.request('posts', 'POST', {'title': 'component-'+self.token, 'content': 'private-body-'+self.token}, 201)['data']
        post_id = post['id']
        self.user.request(f'posts/{post_id}/comments', 'POST', {'content': 'private-comment-'+self.token}, 201)
        self.user.request(f'posts/{post_id}/like', 'PUT', expected=204)
        self.user.request(f'posts/{post_id}/bookmark', 'PUT', expected=204)
        self.user.request(f'users/{me["id"]}/follow', 'PUT')
        wait_until(lambda: self.admin.request('notifications')['data'], 'durable worker notification')
        wait_until(lambda: any(str(p['id']) == str(post_id) for p in self.admin.request('search/posts?q=component-'+self.token)['data']), 'search create projection')
        self.admin.request(f'posts/{post_id}', 'PATCH', {'title': 'updated-'+self.token, 'content': 'updated-body-'+self.token})
        wait_until(lambda: any(str(p['id']) == str(post_id) and p['title'] == 'updated-'+self.token for p in self.admin.request('search/posts?q=updated-'+self.token)['data']), 'search update projection')
        second = self.user.request('posts', 'POST', {'title': 'delete-'+self.token, 'content': 'delete-body-'+self.token}, 201)['data']
        wait_until(lambda: any(str(p['id']) == str(second['id']) for p in self.user.request('search/posts?q=delete-'+self.token)['data']), 'second user search projection')
        self.user.request(f'posts/{second["id"]}', 'DELETE', expected=204)
        wait_until(lambda: not any(str(p['id']) == str(second['id']) for p in self.user.request('search/posts?q=delete-'+self.token)['data']), 'search delete projection')
        self.post_id = post_id
        self.record('two-user business/worker/search regression', {'posts': 2, 'operations': ['create', 'comment', 'like', 'bookmark', 'follow', 'notification', 'search', 'update', 'delete']})

    def six_components(self):
        values = {}
        for component, queries in QUERIES.items():
            values[component] = {q: wait_until(lambda c=component, q=q: self.positive(c, q), component+' '+q) for q in queries}
        wait_until(lambda: self.positive('monitor', 'scrapes_total', scraped_producer_kind='component', scraped_target_id='monitor-local', result='publish_success'), 'Monitor self identity through storage/query')
        wait_until(lambda: self.positive('router', 'messages_total', type='metrics', message_source='backend', result='produced'), 'Router message source label')
        wait_until(lambda: self.positive('marshaller', 'records_total', type='metrics', message_source='backend', stage='store', result='stored'), 'Marshaller storage stage/source')
        self.record('six real component processing/progress queries', values)

    def backend_latency_and_capacity(self):
        labels = {'method': 'GET', 'route': '/api/v1/users/me', 'status_class': '2xx'}
        def fixed_buckets():
            series = [
                s for s in self.query('backend', 'http_request_duration_seconds_bucket')['series']
                if all(s['labels'].get(key) == value for key, value in labels.items())
            ]
            if {s['labels'].get('le') for s in series} != set(LATENCY_BUCKETS):
                return None
            values = {s['labels']['le']: s['points'][-1]['value'] for s in series if s['points']}
            if set(values) != set(LATENCY_BUCKETS) or values['+Inf'] <= 0:
                return None
            previous = -1
            for bucket in LATENCY_BUCKETS:
                if values[bucket] < previous:
                    return None
                previous = values[bucket]
            return values

        buckets = wait_until(fixed_buckets, 'Backend fixed latency buckets')
        tail = [buckets['+Inf']]
        count = wait_until(
            lambda: self.positive('backend', 'http_request_duration_seconds_count', **labels),
            'Backend latency count',
        )
        total = wait_until(
            lambda: self.positive('backend', 'http_request_duration_seconds_sum', **labels),
            'Backend latency sum',
        )
        assert tail[-1] == count[-1], (tail, count)
        assert self.values('backend', 'http_concurrency_limit') == [128]
        in_flight = self.values('backend', 'http_requests_in_flight')
        assert in_flight and 0 <= in_flight[-1] <= 128
        rejected = self.values('backend', 'http_rejected_total')
        assert rejected and rejected[-1] >= 0
        self.record('Backend fixed latency distribution and capacity queries', {
            'bucket_labels': list(LATENCY_BUCKETS), 'tail_count': tail[-1],
            'count': count[-1], 'sum': total[-1], 'concurrency_limit': 128,
            'in_flight': in_flight[-1], 'rejected_total': rejected[-1],
        })

    def security_and_cardinality(self):
        catalog = self.admin.request('observability/metrics/catalog')['data']
        assert len([d for d in catalog if d['producer_kind'] == 'component']) == COMPONENT_FAMILY_COUNT
        query_counts = {}
        for index, component in enumerate(COMPONENTS):
            info = json.loads(command(['docker', 'inspect', self.owned_id(component)]).stdout)[0]
            assert not (info['NetworkSettings']['Ports'] or {}).get(str(19101+index)+'/tcp')
            assert not any(info['NetworkSettings']['Ports'].get(port)
                           for port in (info['NetworkSettings']['Ports'] or {}))
            for auth, status in [('missing', 401), ('wrong', 401), ('duplicate', 401), ('valid', 200)]:
                assert self.endpoint(component, auth=auth)['status'] == status
            assert self.endpoint(component, path='/not-found', method='POST')['status'] == 404
            assert self.endpoint(component, method='POST', path='/internal/v1/metrics?q=1')['status'] == 405
            assert self.endpoint(component, path='/internal/v1/metrics?q=1')['status'] == 400
            assert self.endpoint(component, body='x')['status'] == 400
            body = self.endpoint(component)['body']
            lines = [line for line in body.splitlines() if line and not line.startswith('#')]
            assert len(lines) <= BUDGETS[component]
            assert all(s not in body for s in [self.admin_name, self.user_name, 'private-body-'+self.token, 'private-comment-'+self.token, '?q=', 'request_id=', 'post_id=', 'user_id='])
            count = 0
            for definition in (d for d in catalog if d['source'] == component):
                suffix = definition['metric'].removeprefix('gopulse_'+component.replace('-', '_')+'_')
                result = self.query(component, suffix)
                count += len(result['series'])
                assert not any(token in json.dumps(result) for token in self.tokens.values())
            assert count <= BUDGETS[component]
            query_counts[component] = {'endpoint_samples': len(lines), 'queried_series': count, 'max_samples': BUDGETS[component]}
        self.user.request('observability/metrics/catalog', expected=403)
        self.user.request('observability/metrics?metric=gopulse_backend_http_requests_total&range=15m', expected=403)
        self.record('endpoint authentication, isolation and bounded series', query_counts)

    def browser(self):
        result = command(['docker', 'run', '--rm', '--label', 'com.docker.compose.project='+self.project,
            '--network', self.project+'_edge', '-e', 'GOPULSE_BASE_URL=http://frontend:8080',
            '-e', 'GOPULSE_P14_ADMIN='+self.admin_name, '-e', 'GOPULSE_P14_PASSWORD='+self.auth_password,
            '-v', str(ROOT/'frontend/e2e/phase14-components.spec.ts')+':/work/frontend/e2e/phase14-components.spec.ts:ro',
            'gopulse/acceptance:1.10.6', 'e2e/phase14-components.spec.ts'], timeout=180, check=False)
        output = (result.stdout+result.stderr).decode(errors='replace').replace(self.auth_password, '[REDACTED]')
        (self.work/'component-browser.log').write_text(output)
        assert result.returncode == 0, 'component browser failed; see redacted component-browser.log'
        self.record('actual Frontend component query', {'backend_only': True, 'scraped_identity_rendered': True})

    def dependency_fault(self):
        self.owned_id('redis')
        def wait_for_backend_dependency(value, description):
            def observe():
                self.user.request(f'posts/{self.post_id}')
                return value in self.values('backend', 'dependency_up', dependency='redis')
            wait_until(observe, description)
        try:
            self.compose('stop', 'redis')
            wait_for_backend_dependency(0, 'Redis interaction degradation metric')  # cache fallback preserves committed business fact
            self.record('real Redis outage', {'backend_dependency_up': 0, 'post_detail_status': 200})
        finally:
            self.compose('start', 'redis')
            self.healthy('redis')
        wait_for_backend_dependency(1, 'Redis metric recovery')
        self.record('Redis recovery', {'backend_dependency_up': 1})

    def endpoint_fault(self):
        # Only Monitor's fixed backend DNS points through this owned proxy.
        info = json.loads(command(['docker', 'inspect', self.owned_id('component-probe')]).stdout)[0]
        address = info['NetworkSettings']['Networks'][self.project+'_observability']['IPAddress']
        self.override['services']['monitor']['extra_hosts'] = ['backend:'+address]
        self.save_override()
        self.compose('up', '-d', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        for source in SOURCES: wait_until(lambda s=source: self.status_for(s)['observed_state'] == 'running', source+' retained running')
        fault_at = time.time()
        (self.fault/'enabled').write_text('503')
        try:
            wait_until(lambda: self.positive('monitor', 'scrapes_total', scraped_producer_kind='component', scraped_target_id='backend-local', result='scrape_failure'), 'single endpoint failure exported')
            self.user.request(f'posts/{self.post_id}')
            assert self.endpoint('backend', path='/ready')['status'] == 200
            for source in SOURCES:
                wait_until(lambda s=source: datetime.datetime.fromisoformat(self.status_for(s)['last_success_at'].replace('Z', '+00:00')).timestamp() > fault_at, source+' still collecting during component failure')
            for component in COMPONENTS[1:]:
                wait_until(lambda c=component: max(self.values('monitor', 'last_scrape_success_timestamp_seconds', scraped_producer_kind='component', scraped_target_id=c+'-local') or [0]) > fault_at, component+' unaffected publish')
            assert {entry['id'] for entry in self.admin.request('exporter-plugins')['data']} == {source+'-exporter' for source in SOURCES}
            self.record('single Backend endpoint 503 isolation', {'other_components': 5, 'plugins': 6, 'backend_ready': 200, 'business_detail': 200})
        finally: (self.fault/'enabled').unlink(missing_ok=True)
        wait_until(lambda: self.positive('monitor', 'scrapes_total', scraped_producer_kind='component', scraped_target_id='backend-local', result='publish_success'), 'component scrape recovery')

    def storage_fault(self):
        self.owned_id('victoriametrics')
        began = int(time.time())
        try:
            self.compose('stop', 'victoriametrics')
            wait_until(lambda: 'gopulse_marshaller_dependency_up{dependency="victoriametrics"} 0' in self.endpoint('marshaller')['body'], 'actual storage failure state')
            self.admin.request('observability/metrics?metric=gopulse_backend_http_requests_total&range=15m', expected=503)
            assert self.endpoint('backend', path='/ready')['status'] == 200
            post = self.admin.request('posts', 'POST', {'title': 'storage-'+self.token, 'content': 'storage-boundary'}, 201)['data']
            self.user.request(f'posts/{post["id"]}/comments', 'POST', {'content': 'during metrics storage outage'}, 201)
            wait_until(lambda: any(str(p['id']) == str(post['id']) for p in self.admin.request('search/posts?q=storage-'+self.token)['data']), 'Indexer continues during VM outage')
            wait_until(lambda: any(str(n.get('post_id')) == str(post['id']) for n in self.admin.request('notifications')['data']), 'Worker continues during VM outage')
            self.record('metrics storage outage does not change business readiness or consumer facts', {'backend_ready': 200, 'metric_query': 503, 'notification': True, 'search_projection': True, 'marshaller_dependency_up': 0})
        finally:
            self.compose('start', 'victoriametrics')
            self.healthy('victoriametrics')
        recovery_post = self.admin.request(
            'posts', 'POST',
            {'title': 'storage-recovery-'+self.token, 'content': 'storage-recovery-boundary'},
            201,
        )['data']
        self.user.request(
            f'posts/{recovery_post["id"]}/comments',
            'POST',
            {'content': 'after metrics storage recovery'},
            201,
        )
        wait_until(
            lambda: any(str(p['id']) == str(recovery_post['id'])
                        for p in self.admin.request('search/posts?q=storage-recovery-'+self.token)['data']),
            'Indexer progress after storage recovery',
        )
        wait_until(
            lambda: any(str(n.get('post_id')) == str(recovery_post['id'])
                        for n in self.admin.request('notifications')['data']),
            'Worker progress after storage recovery',
        )
        for c in ('business-worker', 'search-indexer'):
            wait_until(lambda c=c: max(self.values(c, 'last_success_timestamp_seconds') or [0]) >= began, c+' outage progress persisted after recovery')
        wait_until(lambda: 1 in self.values('marshaller', 'dependency_up', dependency='victoriametrics'), 'VM dependency recovery persisted')
        self.record('metrics storage recovery', {'consumer_progress_persisted': True, 'marshaller_dependency_up': 1})

    def consumer_shutdown(self):
        restarted_at = int(time.time())
        for component in ('business-worker', 'search-indexer'):
            cid = self.owned_id(component)
            started = time.monotonic()
            self.compose('stop', component)
            elapsed = time.monotonic()-started
            info = json.loads(command(['docker', 'inspect', cid]).stdout)[0]
            assert not info['State']['Running'] and info['State']['Pid'] == 0 and info['State']['ExitCode'] == 0 and elapsed < 15
            assert self.endpoint(component)['status'] == 0
            self.compose('up', '-d', '--no-deps', '--force-recreate', component)
            wait_until(lambda c=component: self.endpoint(c)['status'] == 200, component+' replacement metrics')
            self.record(component+' SIGTERM and replacement', {'exit_code': 0, 'shutdown_seconds': round(elapsed, 2), 'old_pid': 0, 'endpoint_reopened': True})
        self.business()
        for c in ('business-worker', 'search-indexer'):
            wait_until(lambda c=c: max(self.values(c, 'last_success_timestamp_seconds') or [0]) >= restarted_at, c+' replacement consumes')

    def run(self):
        for component in (*COMPONENTS, 'frontend'): command(['docker', 'image', 'inspect', f'gopulse/{component}:{VERSION}'])
        self.started = True
        self.compose('up', '-d', '--no-build', timeout=360)
        for s in ('frontend', 'backend', 'monitor', 'marshaller'): self.healthy(s)
        base = 'http://'+self.compose('port', 'frontend', '8080').stdout.decode().strip()
        self.admin, self.user = Client(base), Client(base)
        for client, name in ((self.admin, self.admin_name), (self.user, self.user_name)):
            client.request('auth/register', 'POST', {'username': name, 'password': self.auth_password}, 201)
        self.compose('exec', '-T', 'backend', '/usr/local/bin/admin-role', 'promote', '--username', self.admin_name)
        self.admin.request('exporter-plugins/redis-exporter/install', 'POST', self.config('redis'), 201)
        wait_until(self.offsets, 'formal Marshaller consumer offset')
        for s in ('mysql', 'rabbitmq'): self.reconciler.reconcile(s)
        for s in ('kafka', 'elasticsearch', 'victoriametrics'):
            self.admin.request('exporter-plugins/'+s+'-exporter/install', 'POST', self.config(s), 201)
        for s in SOURCES:
            wait_until(lambda s=s: self.metric_for(s, REPRESENTATIVE[s]), s+' representative plugin query')
        self.record('six-plugin representative regression', {'sources': list(SOURCES)})
        self.business()
        self.backend_latency_and_capacity()
        self.six_components()
        self.security_and_cardinality()
        self.browser()
        self.dependency_fault()
        self.endpoint_fault()
        self.consumer_shutdown()
        self.storage_fault()
        assert self.admin.request('observability/logs?service=backend')['data'], 'Logs regression'
        assert self.admin.request('observability/events')['data'], 'Events regression'
        for c in COMPONENTS:
            logs = self.compose('logs', '--no-color', '--tail', '300', c).stdout.decode()
            assert not any(token in logs for token in self.tokens.values())
        self.record('Logs/Events and credentials regression', {'logs': True, 'events': True, 'tokens_in_logs': False})
        print('PASS: six component metrics, real business, storage/query, endpoint/dependency faults and consumer replacement', flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test: self_test(); return
    acceptance = ComponentAcceptance()
    def interrupted(*_): raise RuntimeError('component acceptance interrupted')
    signal.signal(signal.SIGINT, interrupted); signal.signal(signal.SIGTERM, interrupted)
    try: acceptance.run()
    finally: acceptance.cleanup()

if __name__ == '__main__': main()
