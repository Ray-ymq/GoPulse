#!/usr/bin/env python3
"""Phase-14-05 fixed component acceptance; never mutate pre-existing resources.

Build the seven current product images as 1.11.5 before running. Uses the actual
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
import urllib.request
import uuid

from verify_plugin_metrics import ROOT, Client, command, wait_until
from verify_plugin_topology import TopologyAcceptance
from verify_plugin_isolation import SOURCES, REPRESENTATIVE

VERSION = '1.11.5'
COMPONENTS = ('backend', 'business-worker', 'search-indexer', 'monitor', 'router', 'marshaller')
QUERIES = {
 'backend': ('http_requests_total', 'outbox_last_publish_success_timestamp_seconds'),
 'business-worker': ('messages_total', 'last_success_timestamp_seconds'),
 'search-indexer': ('messages_total', 'last_success_timestamp_seconds'),
 'monitor': ('scrapes_total', 'last_scrape_success_timestamp_seconds'),
 'router': ('messages_total', 'last_kafka_ack_timestamp_seconds'),
 'marshaller': ('records_total', 'last_storage_success_timestamp_seconds'),
}
BUDGETS = dict(zip(COMPONENTS, (677, 37, 24, 112, 112, 260)))


def self_test():
    assert len(COMPONENTS) == len(set(COMPONENTS)) == 6
    assert set(QUERIES) == set(BUDGETS) == set(COMPONENTS)
    assert all(len(queries) == 2 for queries in QUERIES.values())
    # Use the production catalog, not a separately maintained fixture list.
    specs = json.loads(command(['go', 'run', './cmd/catalog'], timeout=60,
                               cwd=ROOT/'componentmetrics').stdout)
    assert {s['ID']: s['MaxSamples'] for s in specs} == BUDGETS
    generated = (ROOT/'frontend/src/services/componentMetrics.ts').read_text()
    line = next(line for line in generated.splitlines() if line.startswith('export const componentContracts:'))
    actual = json.loads(line.split(' = ', 1)[1])
    expected = {f['Name']: dict(source=s['ID'], kind=f['Kind'], unit=f['Unit'], keys=f['Keys'] or [], tuples=f['Tuples'])
                for s in specs for f in s['Families']}
    assert actual == expected
    assert all(not any(key in ('source', 'target_id', 'producer_kind', 'producer_id', 'user_id') for key in f['Keys'] or [])
               for s in specs for f in s['Families'])
    print('PASS: six fixed identities, budgets and browser contract match production catalog; no Docker access')


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
        values = self.values(component, suffix, **labels)
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

    def security_and_cardinality(self):
        catalog = self.admin.request('observability/metrics/catalog')['data']
        assert len([d for d in catalog if d['producer_kind'] == 'component']) == 38
        query_counts = {}
        for index, component in enumerate(COMPONENTS):
            info = json.loads(command(['docker', 'inspect', self.owned_id(component)]).stdout)[0]
            assert not (info['NetworkSettings']['Ports'] or {}).get(str(19101+index)+'/tcp')
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
        public = 'http://'+self.compose('port', 'backend', '8080').stdout.decode().strip()+'/internal/v1/metrics'
        try: urllib.request.urlopen(public, timeout=5); raise AssertionError('public metrics route exposed')
        except urllib.error.HTTPError as error: assert error.code == 404
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
        try:
            self.compose('stop', 'redis')
            self.user.request(f'posts/{self.post_id}')  # cache fallback preserves committed business fact
            wait_until(lambda: self.values('backend', 'dependency_up', dependency='redis') == [0], 'Redis interaction degradation metric')
            self.record('real Redis outage', {'backend_dependency_up': 0, 'post_detail_status': 200})
        finally:
            self.compose('start', 'redis')
            self.healthy('redis')
        self.user.request(f'posts/{self.post_id}')
        wait_until(lambda: self.values('backend', 'dependency_up', dependency='redis') == [1], 'Redis metric recovery')
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
            ready = urllib.request.urlopen('http://'+self.compose('port', 'backend', '8080').stdout.decode().strip()+'/ready', timeout=10)
            assert ready.code == 200
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
            ready = urllib.request.urlopen('http://'+self.compose('port', 'backend', '8080').stdout.decode().strip()+'/ready', timeout=10)
            assert ready.code == 200
            post = self.admin.request('posts', 'POST', {'title': 'storage-'+self.token, 'content': 'storage-boundary'}, 201)['data']
            self.user.request(f'posts/{post["id"]}/comments', 'POST', {'content': 'during metrics storage outage'}, 201)
            wait_until(lambda: any(str(p['id']) == str(post['id']) for p in self.admin.request('search/posts?q=storage-'+self.token)['data']), 'Indexer continues during VM outage')
            wait_until(lambda: any(str(n.get('post_id')) == str(post['id']) for n in self.admin.request('notifications')['data']), 'Worker continues during VM outage')
            self.record('metrics storage outage does not change business readiness or consumer facts', {'backend_ready': 200, 'metric_query': 503, 'notification': True, 'search_projection': True, 'marshaller_dependency_up': 0})
        finally:
            self.compose('start', 'victoriametrics')
            self.healthy('victoriametrics')
        for c in ('business-worker', 'search-indexer'):
            wait_until(lambda c=c: max(self.values(c, 'last_success_timestamp_seconds') or [0]) >= began, c+' outage progress persisted after recovery')
        wait_until(lambda: self.values('marshaller', 'dependency_up', dependency='victoriametrics') == [1], 'VM dependency recovery persisted')
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
