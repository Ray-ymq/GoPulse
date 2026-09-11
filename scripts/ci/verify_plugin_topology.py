"""Phase-14-03 real-target gate; all mutations are confined to an owned project."""
import json
import re
import time
import uuid
from pathlib import Path

from verify_plugin_metrics import Client, command, wait_until, ROOT, PATTERN
from verify_plugin_clusters import ClusterAcceptance

VERSION = '1.11.3'
TOPIC = 'gopulse-observability-v1'
GROUP = 'gopulse-marshaller-metrics-v1'
FAMILIES = {
    'kafka': 'up brokers controller_available partitions under_replicated_partitions offline_partitions consumer_group_lag'.split(),
    'elasticsearch': 'up cluster_health_status nodes data_nodes active_primary_shards active_shards relocating_shards initializing_shards unassigned_shards pending_tasks documents store_size_bytes'.split(),
}


def follower_override(project):
    assert PATTERN.fullmatch(project)
    return {'image': 'apache/kafka:4.3.1', 'hostname': 'kafka-follower',
            'environment': {'KAFKA_NODE_ID': '2', 'KAFKA_PROCESS_ROLES': 'broker',
                'KAFKA_LISTENERS': 'INTERNAL://:19092',
                'KAFKA_ADVERTISED_LISTENERS': 'INTERNAL://kafka-follower:19092',
                'KAFKA_LISTENER_SECURITY_PROTOCOL_MAP': 'CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT',
                'KAFKA_CONTROLLER_QUORUM_VOTERS': '1@kafka:9093',
                'KAFKA_CONTROLLER_LISTENER_NAMES': 'CONTROLLER',
                'KAFKA_INTER_BROKER_LISTENER_NAME': 'INTERNAL',
                'KAFKA_AUTO_CREATE_TOPICS_ENABLE': 'false'},
            'networks': ['observability'], 'volumes': ['p1403_follower:/var/lib/kafka/data']}


def self_test():
    service = follower_override('gopulse-p1401-012345abcdef')
    assert service['environment']['KAFKA_PROCESS_ROLES'] == 'broker'
    assert service['volumes'] == ['p1403_follower:/var/lib/kafka/data']
    assert service['networks'] == ['observability'] and 'ports' not in service
    for bad in ['gopulse', '', '../gopulse-p1401-012345abcdef']:
        try:
            follower_override(bad)
        except AssertionError:
            continue
        raise AssertionError('unowned project accepted')


class TopologyAcceptance(ClusterAcceptance):
    def __init__(self):
        super().__init__()
        self.override['services'] = {s: {'image': 'gopulse/'+s+':'+VERSION}
                                    for s in ['frontend', 'backend', 'router', 'marshaller', 'monitor']}
        self.override['services']['monitor']['environment'] = {'MONITOR_BOOTSTRAP_PACKAGE': ''}
        self.override['volumes']['p1403_follower'] = {}
        self.save_override()
        self.evidence = []

    def save_override(self):
        self.override_file.write_text(json.dumps(self.override))

    def record(self, name, values):
        self.evidence.append({'step': name, 'at': time.time(), 'values': values})
        (self.work/'topology-evidence.json').write_text(json.dumps(self.evidence, indent=2))

    def config(self, source):
        if source not in FAMILIES:
            return super().config(source)
        cfg = {'host': source, 'port': 19092 if source == 'kafka' else 9200,
               'connect_timeout': '1s', 'scrape_timeout': '3s'}
        if source == 'kafka':
            cfg.update(topic=TOPIC, consumer_group=GROUP)
        return {'config': cfg, 'secrets': {}}

    def scrape(self, source):
        port = {'redis': 9121, 'mysql': 9122, 'rabbitmq': 9123, 'kafka': 9124, 'elasticsearch': 9125}[source]
        self.owned_id('monitor')
        raw = self.compose('exec', '-T', 'monitor', 'sh', '-c',
            '{ cat; sleep 4; } | nc -w 5 127.0.0.1 "$1"', 'probe', str(port),
            data=b'GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n').stdout
        header, body = raw.split(b'\r\n\r\n', 1)
        return int(header.split()[1]), body

    def sample(self, source):
        code, body = self.scrape(source)
        assert code == 200, (source, code, body[:160])
        assert body.count(b'# TYPE ') == len(FAMILIES[source])
        values = dict(line.rsplit(' ', 1) for line in body.decode().splitlines() if line and not line.startswith('#'))
        assert len(values) == (7 if source == 'kafka' else 14)
        return {key: float(value) for key, value in values.items()}

    def kafka(self, tool, *args):
        self.owned_id('kafka')
        return self.compose('exec', '-T', 'kafka', '/opt/kafka/bin/kafka-'+tool+'.sh',
                            '--bootstrap-server', 'kafka:19092', *args).stdout.decode()

    def describe(self):
        return self.kafka('topics', '--describe', '--topic', TOPIC)

    def offsets(self):
        text = self.kafka('consumer-groups', '--describe', '--group', GROUP)
        rows = []
        for line in text.splitlines():
            fields = line.split()
            if len(fields) >= 6 and fields[:2] == [GROUP, TOPIC]:
                rows.append([int(v) for v in fields[2:6]])  # partition, committed, end, lag
        assert rows and all(row[1] >= 0 for row in rows)
        return sorted(rows)

    def es(self, path, method='GET', body=None):
        self.owned_id('elasticsearch')
        args = ['exec', '-T', 'elasticsearch', 'curl', '--fail', '--silent', '--show-error',
                '-X', method, 'http://127.0.0.1:9200'+path]
        if body is not None:
            args += ['-H', 'Content-Type: application/json', '--data-binary', '@-']
        raw = self.compose(*args, data=json.dumps(body).encode() if body is not None else None).stdout
        return json.loads(raw)

    def assign(self, replicas):
        self.owned_id('kafka')
        body = {'version': 1, 'partitions': [{'topic': TOPIC, 'partition': 0, 'replicas': replicas}]}
        self.compose('exec', '-T', 'kafka', 'sh', '-c', 'cat > /tmp/p1403-assignment.json', data=json.dumps(body).encode())
        output = self.kafka('reassign-partitions', '--reassignment-json-file', '/tmp/p1403-assignment.json', '--execute')
        assert 'Successfully started' in output
        self.record('acceptance administrator replica assignment', body)

    def synchronized(self):
        return bool(re.search(r'Isr: (1,2|2,1)\s', self.describe()))

    def new_metric(self, source, suffix, after, value=None):
        points = self.metric_for(source, suffix)
        if not points:
            return None
        return [p for p in points if p['timestamp'] > after and (value is None or p['value'] == value)] or None

    def follower(self):
        assert 'PartitionCount: 1' in self.describe(), 'gate requires the locked one-partition topic'
        before = self.file('kafka-exporter/runtime/process.json')
        self.override['services']['kafka-follower'] = follower_override(self.project)
        self.save_override()
        self.compose('up', '-d', '--no-build', 'kafka-follower')
        wait_until(lambda: 'id: 2' in self.kafka('broker-api-versions'), 'follower registration')
        self.owned_id('kafka-follower')
        self.assign([1, 2])
        wait_until(self.synchronized, 'replica synchronization')
        assert 'Leader: 1' in self.describe()
        state = self.kafka('consumer-groups', '--describe', '--group', GROUP, '--state')
        assert re.search(r'kafka:19092\s+\(1\)', state), 'formal coordinator must stay on original origin'
        offsets_topic = self.kafka('topics', '--describe', '--topic', '__consumer_offsets')
        assert all('Leader: 1' in line for line in offsets_topic.splitlines() if 'Partition:' in line)
        normal = self.sample('kafka')
        assert normal['gopulse_kafka_brokers'] == 2 and normal['gopulse_kafka_under_replicated_partitions'] == 0
        self.record('follower synchronized', normal)
        last = self.metric_for('kafka', 'under_replicated_partitions')[-1]['timestamp']
        self.compose('stop', 'kafka-follower')
        wait_until(lambda: bool(re.search(r'Isr: 1\s', self.describe())), 'under-replicated topology')
        values = self.sample('kafka')
        assert values['gopulse_kafka_under_replicated_partitions'] == 1 and values['gopulse_kafka_up'] == 1
        self.record('follower stopped: complete real snapshot', {'metrics': values, 'offsets': self.offsets()})
        points = wait_until(lambda: self.new_metric('kafka', 'under_replicated_partitions', last, 1), 'Backend partial topology', 120)
        self.record('Backend under-replicated', points)
        self.compose('start', 'kafka-follower')
        wait_until(self.synchronized, 'follower recovered')
        wait_until(lambda: self.new_metric('kafka', 'under_replicated_partitions', points[-1]['timestamp'], 0), 'Backend topology recovery')
        assert self.file('kafka-exporter/runtime/process.json') == before
        self.assign([1])
        wait_until(lambda: 'ReplicationFactor: 1' in self.describe() and 'Replicas: 1\t' in self.describe(), 'single replica restoration')
        self.compose('stop', 'kafka-follower')
        self.compose('rm', '-f', 'kafka-follower')
        del self.override['services']['kafka-follower']
        self.save_override()
        volume = self.project+'_p1403_follower'
        info = json.loads(command(['docker', 'volume', 'inspect', volume]).stdout)[0]
        assert info['Labels']['com.docker.compose.project'] == self.project
        command(['docker', 'volume', 'rm', volume])
        wait_until(lambda: self.sample('kafka')['gopulse_kafka_brokers'] == 1, 'single broker restoration')
        self.mark('real follower outage: full snapshot/Backend partial value, same-process recovery, single-broker restoration')

    def elasticsearch(self):
        before = self.file('elasticsearch-exporter/runtime/process.json')
        owned_index = 'gopulse-p1403-'+self.token
        # This temporary index is an acceptance fixture, never a business/log/event object.
        self.es('/'+owned_index, 'PUT', {'settings': {'number_of_shards': 1, 'number_of_replicas': 1}})
        self.es('/'+owned_index+'/_doc/1?refresh=true', 'PUT', {'acceptance': True})
        try:
            health = self.es('/_cluster/health')
            assert health['status'] == 'yellow'
            stats = self.es('/_stats/docs,store?level=cluster')
            values = self.sample('elasticsearch')
            after = self.es('/_stats/docs,store?level=cluster')
            docs = [v['_all']['primaries']['docs']['count'] for v in [stats, after]]
            assert min(docs) <= values['gopulse_elasticsearch_documents'] <= max(docs)
            store = [v['_all']['primaries']['store']['size_in_bytes'] for v in [stats, after]]
            assert min(store) <= values['gopulse_elasticsearch_store_size_bytes'] <= max(store)
            assert values['gopulse_elasticsearch_cluster_health_status{status="yellow"}'] == 1
            assert values['gopulse_elasticsearch_active_primary_shards'] == health['active_primary_shards']
            self.record('ES real yellow primary aggregates', {'metrics': values, 'primary_docs_window': docs, 'primary_store_bytes_window': store})
            wait_until(lambda: self.metric_for('elasticsearch', 'cluster_health_status'), 'Backend yellow snapshot')
        finally:
            self.es('/'+owned_index, 'DELETE')
        assert self.file('elasticsearch-exporter/runtime/process.json') == before
        self.mark('real Elasticsearch yellow snapshot uses primary docs/bytes; temporary acceptance index removed')

    def upload_for(self, source, file, expected):
        raw = self.compose('exec', '-T', 'monitor', 'cat', '/opt/gopulse/packages/'+file).stdout
        boundary = 'p1403'+uuid.uuid4().hex
        data = (f'--{boundary}\r\nContent-Disposition: form-data; name="package"; filename="plugin.tar.gz"\r\nContent-Type: application/gzip\r\n\r\n'.encode()+raw+f'\r\n--{boundary}--\r\n'.encode())
        self.admin.request('exporter-plugins/'+source+'-exporter/update', 'POST', data, expected,
                           {'Content-Type': 'multipart/form-data; boundary='+boundary})

    def run(self):
        self.started = True
        self.authenticated_elasticsearch()
        # Start no Marshaller: the first Kafka check must fail without committed offsets.
        self.compose('up', '-d', '--no-build', 'frontend', 'monitor', timeout=360)
        for service in ['frontend', 'backend', 'monitor']:
            self.healthy(service)
        base = 'http://'+self.compose('port', 'frontend', '8080').stdout.decode().strip()
        self.admin, self.user = Client(base), Client(base)
        for client, name in [(self.admin, self.admin_name), (self.user, self.user_name)]:
            client.request('auth/register', 'POST', {'username': name, 'password': self.auth_password}, 201)
        self.compose('exec', '-T', 'backend', '/usr/local/bin/admin-role', 'promote', '--username', self.admin_name)
        topics = self.kafka('topics', '--list')
        self.admin.request('exporter-plugins/kafka-exporter/connection-test', 'POST', self.config('kafka'), 422)
        assert self.kafka('topics', '--list') == topics
        self.record('no committed offset', {'connection_test_http_status': 422, 'topic_list_unchanged': True})
        redis = {'config': {'host': 'redis', 'port': 6379, 'database': 0, 'connect_timeout': '1s', 'scrape_timeout': '2s'}, 'secrets': {'password': self.secret}}
        self.admin.request('exporter-plugins/redis-exporter/install', 'POST', redis, 201)
        self.compose('up', '-d', '--no-build', timeout=360)
        self.healthy('marshaller')
        wait_until(self.offsets, 'formal Marshaller committed offsets')
        for source in ['mysql', 'rabbitmq']:
            self.reconciler.reconcile(source)
        for source in FAMILIES:
            self.admin.request('exporter-plugins/'+source+'-exporter/connection-test', 'POST', self.config(source))
            self.admin.request('exporter-plugins/'+source+'-exporter/install', 'POST', self.config(source), 201)
            for method, suffix in [('GET', ''), ('POST', '/connection-test'), ('POST', '/install'), ('POST', '/start'), ('POST', '/stop'), ('PUT', '/configuration'), ('POST', '/update')]:
                self.user.request('exporter-plugins/'+source+'-exporter'+suffix, method, expected=403)
            values = self.sample(source)
            for suffix in FAMILIES[source]:
                wait_until(lambda s=source, f=suffix: self.metric_for(s, f), source+' Backend '+suffix)
            self.record(source+' initial complete snapshot', values)
        self.mark('empty-volume install of five plugins; missing-offset failure recovers through formal consumption; all 19 new families queryable; user denied')
        post = self.admin.request('posts', 'POST', {'title': 'phase1403 '+self.token, 'content': 'retained '+self.token}, 201)['data']
        wait_until(lambda: self.admin.request('search/posts?q='+self.token)['data'], 'business search')
        aliases = self.es('/_alias')
        templates = self.es('/_index_template')
        self.follower()
        self.elasticsearch()
        # Pin formal consumption briefly to distinguish collector reads from offset writes.
        self.compose('stop', 'marshaller')
        try:
            rows = self.offsets()
            values = self.sample('kafka')
            after = self.offsets()
            assert [r[:2] for r in rows] == [r[:2] for r in after]
            assert sum(max(r[2]-r[1], 0) for r in rows) <= values['gopulse_kafka_consumer_group_lag'] <= sum(max(r[2]-r[1], 0) for r in after)
            self.record('read-only committed offsets and lag window', {'before': rows, 'after': after, 'lag': values['gopulse_kafka_consumer_group_lag']})
        finally:
            self.compose('start', 'marshaller')
            self.healthy('marshaller')
        for source in FAMILIES:
            pid = self.file(source+'-exporter/runtime/process.json')
            self.compose('stop', source)
            try:
                code, body = self.scrape(source)
                assert code == 503 and body == ('# TYPE gopulse_'+source+'_up gauge\ngopulse_'+source+'_up 0\n').encode()
                assert self.status_for(source)['observed_state'] == 'running'
                assert self.scrape('redis')[0] == 200
                self.admin.request('posts/'+str(post['id']))
                if source == 'elasticsearch':
                    self.admin.request('search/posts?q='+self.token, expected=503)
            finally:
                self.compose('start', source)
                self.healthy(source)
            wait_until(lambda: self.scrape(source)[0] == 200, source+' same-process recovery')
            assert self.file(source+'-exporter/runtime/process.json') == pid
        assert self.es('/_alias') == aliases and self.es('/_index_template') == templates
        self.mark('shared target outage yields sole up=0, bounded business degradation, same-process recovery; no alias/template or formal offset writes')
        # Failed configuration and process isolation without global infrastructure outage.
        bad = self.config('kafka'); bad['config']['topic'] = 'not-allowed'
        self.admin.request('exporter-plugins/kafka-exporter/configuration', 'PUT', bad, 400)
        process = json.loads(self.file('kafka-exporter/runtime/process.json'))
        self.compose('exec', '-T', 'monitor', 'kill', '-KILL', str(process['pid']))
        wait_until(lambda: self.status_for('kafka')['observed_state'] == 'failed', 'single process failure')
        for source in ['redis', 'mysql', 'rabbitmq', 'elasticsearch']:
            assert self.status_for(source)['observed_state'] == 'running'
        self.admin.request('posts/'+str(post['id']))
        self.admin.request('exporter-plugins/kafka-exporter/start', 'POST')
        # Production binary/packaging was used above. Only trusted update tests use the acceptance image.
        self.override['services']['monitor']['image'] = 'gopulse/monitor-acceptance:'+VERSION
        self.save_override()
        self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        for source in FAMILIES:
            pointer = self.file(source+'-exporter/active.json')
            self.upload_for(source, source+'-failure.tar.gz', 422)
            assert self.file(source+'-exporter/active.json') == pointer
            assert all(self.status_for(s)['observed_state'] == 'running' for s in ['redis', 'mysql', 'rabbitmq', 'kafka', 'elasticsearch'])
            self.upload_for(source, source+'-update.tar.gz', 200)
            assert self.status_for(source)['version'] == '1.11.4'
        self.admin.request('exporter-plugins/kafka-exporter/stop', 'POST')
        self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        assert self.status_for('kafka')['observed_state'] == 'stopped'
        assert all(self.status_for(s)['observed_state'] == 'running' for s in ['redis', 'mysql', 'rabbitmq', 'elasticsearch'])
        self.admin.request('exporter-plugins/kafka-exporter/start', 'POST')
        self.mark('single config/process/update failure isolated; trusted higher-version updates; same-volume desired-state recovery')
        for source in ['redis', 'mysql', 'rabbitmq']:
            wait_until(lambda s=source: self.metric_for(s), source+' regression query')
        wait_until(lambda: self.admin.request('observability/logs')['data'], 'Logs regression')
        wait_until(lambda: self.admin.request('observability/events')['data'], 'Events regression')
        wait_until(lambda: self.admin.request('search/posts?q='+self.token)['data'], 'search recovery')
        assert self.admin.request('posts/'+str(post['id']))['data']['id'] == post['id']
        # Exercise the changed no-Secret install buttons on a genuinely blank
        # plugin volume, then restore the five-plugin desired-state volume.
        self.override['services']['monitor']['volumes'] = ['p14_empty:/var/lib/gopulse-monitor/plugins']
        self.save_override()
        self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        self.browser_topology()
        self.override['services']['monitor'].pop('volumes')
        self.save_override()
        self.compose('up', '-d', '--no-build', '--no-deps', '--force-recreate', 'monitor')
        self.healthy('monitor')
        assert all(self.status_for(s)['observed_state'] == 'running' for s in ['redis', 'mysql', 'rabbitmq', 'kafka', 'elasticsearch'])
        snapshots = [json.dumps(self.admin.request('exporter-plugins/catalog')), json.dumps(self.admin.request('exporter-plugins')), json.dumps(self.admin.request('observability/events'))]
        for secret in [self.secret, self.auth_password, self.account('mysql')['password'], self.account('rabbitmq')['password']]:
            assert all(secret not in text for text in snapshots)

        self.mark('three prior plugins, Logs/Events, Phase 13 post/search regression and five-plugin browser flow')

    def authenticated_elasticsearch(self):
        # A separate private network preserves the product's no-auth target and
        # verifies real 9.5.2 authentication without changing business settings.
        admin_secret = 'auth-admin-'+self.token
        password = 'auth-metrics-'+self.token
        self.override.setdefault('networks', {})['p1403_auth'] = {'internal': True}
        self.override['volumes']['p1403_auth_data'] = {}
        self.override['services']['elasticsearch-auth'] = {
            'image': 'docker.elastic.co/elasticsearch/elasticsearch:9.5.2',
            'environment': {'discovery.type': 'single-node', 'xpack.security.enabled': 'true',
                'xpack.security.enrollment.enabled': 'false', 'ES_JAVA_OPTS': '-Xms512m -Xmx512m',
                'ELASTIC_PASSWORD': admin_secret},
            'networks': {'p1403_auth': {'aliases': ['elasticsearch']}},
            'volumes': ['p1403_auth_data:/usr/share/elasticsearch/data']}
        self.override['services']['elasticsearch-auth-exporter'] = {
            'image': 'gopulse/monitor:'+VERSION,
            'entrypoint': ['/bin/sh', '-c', 'mkdir -p /tmp/probe && tar -xzf /opt/gopulse/packages/gopulse-elasticsearch-exporter.tar.gz -C /tmp/probe && exec /tmp/probe/bin/gopulse-elasticsearch-exporter'],
            'environment': {'GOPULSE_RUNTIME_MODE': 'container', 'ELASTICSEARCH_HOST': 'elasticsearch',
                'ELASTICSEARCH_PORT': '9200', 'ELASTICSEARCH_USERNAME': 'gopulse_metrics',
                'ELASTICSEARCH_PASSWORD': password, 'ELASTICSEARCH_EXPORTER_CONNECT_TIMEOUT': '1s',
                'ELASTICSEARCH_EXPORTER_SCRAPE_TIMEOUT': '3s'},
            'networks': ['p1403_auth'], 'read_only': True,
            'tmpfs': ['/tmp:rw,exec,nosuid,nodev,size=32m,mode=1777']}
        self.save_override()
        self.override_file.chmod(0o600)
        self.compose('up', '-d', '--no-build', 'elasticsearch-auth')
        def request(path, method='GET', body=None, collector=False):
            self.owned_id('elasticsearch-auth')
            # Passwords are provided through stdin curl config, never argv/output.
            user, secret = ('gopulse_metrics', password) if collector else ('elastic', admin_secret)
            config = 'user = "'+user+':'+secret+'"\n'
            if body is not None:
                config += 'header = "Content-Type: application/json"\ndata = '+json.dumps(json.dumps(body))+'\n'
            result = self.compose('exec', '-T', 'elasticsearch-auth', 'curl', '--silent', '--show-error',
                                  '--config', '-', '-X', method, '-w', '\n%{http_code}',
                                  'http://127.0.0.1:9200'+path, data=config.encode(), check=False)
            raw, _, code = result.stdout.rpartition(b'\n')
            return int(code) if code.isdigit() else 0, raw
        wait_until(lambda: request('/_cluster/health')[0] == 200, 'authenticated Elasticsearch startup', 180)
        assert request('/_security/role/gopulse_metrics', 'PUT', {'cluster': ['monitor'], 'indices': [{'names': ['*'], 'privileges': ['monitor']} ]})[0] == 200
        assert request('/_security/user/gopulse_metrics', 'PUT', {'password': password, 'roles': ['gopulse_metrics']})[0] == 200
        assert request('/gopulse-auth-acceptance', 'PUT', {'settings': {'number_of_replicas': 0}})[0] == 200
        assert request('/gopulse-auth-acceptance/_doc/1?refresh=true', 'PUT', {'acceptance': True})[0] == 201
        assert request('/_cluster/health', collector=True)[0] == 200
        assert request('/_stats/docs,store?level=cluster', collector=True)[0] == 200
        assert request('/gopulse-forbidden', 'PUT', {}, collector=True)[0] == 403
        assert request('/gopulse-auth-acceptance/_search', collector=True)[0] == 403
        self.compose('up', '-d', '--no-build', 'elasticsearch-auth-exporter')
        cid = self.owned_id('elasticsearch-auth-exporter')
        def scrape():
            raw = self.compose('exec', '-T', 'elasticsearch-auth-exporter', 'sh', '-c',
                '{ cat; sleep 4; } | nc -w 5 127.0.0.1 9125',
                data=b'GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n').stdout
            header, body = raw.split(b'\r\n\r\n', 1)
            return int(header.split()[1]), body
        wait_until(lambda: scrape()[0] == 200, 'real authenticated Exporter')
        changed = request('/_security/user/gopulse_metrics/_password', 'POST', {'password': password+'-rotated'})
        assert changed[0] == 200
        code, body = scrape()
        assert code == 503 and body == b'# TYPE gopulse_elasticsearch_up gauge\ngopulse_elasticsearch_up 0\n'
        assert request('/_security/user/gopulse_metrics/_password', 'POST', {'password': password})[0] == 200
        wait_until(lambda: scrape()[0] == 200, 'same-process authentication recovery')
        assert self.owned_id('elasticsearch-auth-exporter') == cid
        info = json.loads(command(['docker', 'inspect', cid]).stdout)[0]
        assert info['RestartCount'] == 0
        output = self.compose('logs', '--no-color', 'elasticsearch-auth-exporter').stdout
        assert password.encode() not in output and admin_secret.encode() not in output
        self.record('real ES authentication and least named privileges', {'health': 200, 'stats': 200, 'create_index': 403, 'search_documents': 403, 'wrong_password_scrape': 503, 'recovered': True, 'restarts': 0})
        self.mark('real Elasticsearch authentication failure/recovery and monitor-only account allow/deny checks')

    def browser_topology(self):
        result = command(['docker', 'run', '--rm', '--network', self.project+'_edge',
            '-e', 'GOPULSE_BASE_URL=http://frontend:8080', '-e', 'GOPULSE_P14_ADMIN='+self.admin_name,
            '-e', 'GOPULSE_P14_PASSWORD='+self.auth_password,
            '-v', str(ROOT/'frontend/e2e/phase14-topology.spec.ts')+':/work/frontend/e2e/phase14-topology.spec.ts:ro',
            'gopulse/acceptance:1.10.6', 'e2e/phase14-topology.spec.ts'], timeout=200, check=False)
        output = (result.stdout+result.stderr).decode(errors='replace').replace(self.auth_password, '[REDACTED]')
        (self.work/'browser.log').write_text(output)
        assert result.returncode == 0, 'browser failed; see redacted browser.log'
