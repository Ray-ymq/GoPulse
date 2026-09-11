#!/usr/bin/env python3
"""Owned MySQL/Backend role acceptance; never uses daily volumes."""
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]


def valid_project(name):
    return re.fullmatch(r'gopulse-roles-[0-9a-f]{12}', name) is not None


def ports(count):
    sockets = [socket.socket() for _ in range(count)]
    try:
        for s in sockets:
            s.bind(('127.0.0.1', 0))
        return [s.getsockname()[1] for s in sockets]
    finally:
        for s in sockets:
            s.close()


class Client:
    def __init__(self, url):
        self.url = url
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, method='GET', body=None, status=200):
        request = urllib.request.Request(self.url+'/api/v1/'+path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Content-Type': 'application/json'})
        try:
            result = self.opener.open(request, timeout=10)
        except urllib.error.HTTPError as e:
            result = e
        content = result.read().decode()
        assert result.code == status, (method, path, result.code, status, content)
        for secret in ('password_hash', 'SELECT ', 'UPDATE ', 'INSERT ', 'sensitive-fixture-hash'):
            assert secret not in content, (path, secret)
        return json.loads(content)


class Acceptance:
    def __init__(self, directory):
        self.directory = Path(directory)
        self.project = 'gopulse-roles-'+secrets.token_hex(6)
        self.password = secrets.token_hex(20)
        self.processes = []
        self.started = False
        self.env = dict(os.environ)
        mysql, redis, rabbit, es, vm, monitor, backend = ports(7)
        self.url = f'http://127.0.0.1:{backend}'
        self.env.update(APP_ENV='test', MYSQL_HOST='127.0.0.1', MYSQL_PORT=str(mysql),
            MYSQL_DATABASE='gopulse_roles', MYSQL_USER='gopulse', MYSQL_PASSWORD=self.password,
            REDIS_HOST='127.0.0.1', REDIS_PORT=str(redis), REDIS_PASSWORD=self.password, REDIS_DB='0',
            RABBITMQ_URL=f'amqp://gopulse:{self.password}@127.0.0.1:{rabbit}/',
            ELASTICSEARCH_URL=f'http://127.0.0.1:{es}', AUTH_JWT_SECRET=secrets.token_hex(32),
            AUTH_COOKIE_NAME='gopulse_roles_session', AUTH_COOKIE_SECURE='false',
            HTTP_HOST='127.0.0.1', HTTP_PORT=str(backend),
            MONITOR_URL=f'http://127.0.0.1:{monitor}', MONITOR_API_TOKEN=secrets.token_hex(32),
            BACKEND_VICTORIAMETRICS_URL=f'http://127.0.0.1:{vm}',
            BACKEND_VICTORIAMETRICS_USERNAME='gopulse', BACKEND_VICTORIAMETRICS_PASSWORD=self.password)
        self.env.update(GOPULSE_RUNTIME_MODE='host', GOPULSE_VERSION='1.12.1', LOG_MONITOR_INGEST_TOKEN=secrets.token_hex(32))
        for component in ('BACKEND', 'MONITOR', 'BUSINESS_WORKER', 'SEARCH_INDEXER', 'ROUTER', 'MARSHALLER'):
            self.env[component+'_METRICS_TOKEN'] = secrets.token_hex(32)
        self.env.update(MONITOR_HTTP_HOST='127.0.0.1', MONITOR_HTTP_PORT=str(monitor),
            MONITOR_PLUGIN_ROOT=str(self.directory/'plugins'))
        services = {
            'mysql': {'image': 'mysql:8.4.0', 'environment': {
                'MYSQL_DATABASE': 'gopulse_roles', 'MYSQL_USER': 'gopulse',
                'MYSQL_PASSWORD': self.password, 'MYSQL_ROOT_PASSWORD': self.password},
                'ports': [f'127.0.0.1:{mysql}:3306'],
                'healthcheck': {'test': ['CMD-SHELL', f'mysqladmin ping -h 127.0.0.1 -uroot -p{self.password} --silent'], 'interval': '2s', 'timeout': '2s', 'retries': 60}},
            'redis': {'image': 'redis:7.2.5-alpine', 'command': ['redis-server', '--requirepass', self.password], 'ports': [f'127.0.0.1:{redis}:6379']},
            'rabbitmq': {'image': 'rabbitmq:3.13.3-management-alpine', 'environment': {'RABBITMQ_DEFAULT_USER': 'gopulse', 'RABBITMQ_DEFAULT_PASS': self.password}, 'ports': [f'127.0.0.1:{rabbit}:5672'],
                'healthcheck': {'test': ['CMD', 'rabbitmq-diagnostics', '-q', 'ping'], 'interval': '3s', 'timeout': '5s', 'retries': 50}},
            'elasticsearch': {'image': 'docker.elastic.co/elasticsearch/elasticsearch:9.5.2', 'environment': {'discovery.type': 'single-node', 'xpack.security.enabled': 'false', 'ES_JAVA_OPTS': '-Xms512m -Xmx512m'}, 'ports': [f'127.0.0.1:{es}:9200'],
                'healthcheck': {'test': ['CMD-SHELL', "curl -fsS 'http://127.0.0.1:9200/_cluster/health?wait_for_status=yellow&timeout=1s' >/dev/null"], 'interval': '3s', 'timeout': '3s', 'retries': 60}},
            'victoriametrics': {'image': 'victoriametrics/victoria-metrics:v1.151.0',
                'command': ['-httpAuth.username=gopulse', f'-httpAuth.password={self.password}'], 'ports': [f'127.0.0.1:{vm}:8428']},
        }
        # Monitor is unchanged by this batch. Its source-only binary intentionally
        # has no trusted package catalog; use the existing six-plugin image.
        monitor_env = {key: self.env[key] for key in self.env if key.endswith('_METRICS_TOKEN')}
        monitor_env.update(GOPULSE_RUNTIME_MODE='container', GOPULSE_VERSION='1.11.5',
            MONITOR_HTTP_HOST='0.0.0.0', MONITOR_HTTP_PORT='9090',
            MONITOR_API_TOKEN=self.env['MONITOR_API_TOKEN'],
            LOG_MONITOR_INGEST_TOKEN=self.env['LOG_MONITOR_INGEST_TOKEN'],
            MONITOR_PLUGIN_ROOT='/var/lib/gopulse-monitor/plugins',
            REDIS_HOST='redis', REDIS_PORT='6379', REDIS_PASSWORD=self.password, REDIS_DB='0')
        services['monitor'] = {'image': 'gopulse/monitor:1.11.5', 'pull_policy': 'never',
            'environment': monitor_env, 'ports': [f'127.0.0.1:{monitor}:9090']}
        self.compose_file = self.directory/'compose.json'
        self.compose_file.write_text(json.dumps({'services': services}))

    def command(self, args, **kwargs):
        result = subprocess.run(args, cwd=ROOT, env=self.env, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, timeout=kwargs.pop('timeout', 180), **kwargs)
        if result.returncode:
            raise AssertionError(f'{args[0]} failed: {result.stderr.decode()[-3000:]}')
        return result.stdout.decode().strip()

    def compose(self, *args):
        assert valid_project(self.project)
        return self.command(['docker', 'compose', '-p', self.project, '-f', str(self.compose_file), *args], timeout=420)

    def sql(self, query, database=None):
        database = database or self.env['MYSQL_DATABASE']
        return self.compose('exec', '-T', 'mysql', 'mysql', '-uroot', '-p'+self.password,
            '--batch', '--skip-column-names', database, '-e', query)

    def binary(self, name, *args):
        return self.command([str(self.directory/name), *args])

    def start(self, name):
        log = open(self.directory/(name+'.log'), 'wb')
        process = subprocess.Popen([str(self.directory/name)], env=self.env, cwd=ROOT,
            stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        self.processes.append((process, log))
        return process

    def stop_backend(self):
        for process, log in reversed(self.processes):
            if log.name.endswith('/backend.log') and process.poll() is None:
                process.terminate()
                process.wait(timeout=20)
                return

    def wait_monitor(self):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for _ in range(80):
            try:
                request = urllib.request.Request(self.env['MONITOR_URL']+'/health', headers={'Authorization': 'Bearer '+self.env['MONITOR_API_TOKEN']})
                with opener.open(request, timeout=1) as response:
                    if response.status == 200:
                        return
            except (OSError, urllib.error.URLError):
                time.sleep(.25)
        raise AssertionError(self.compose('logs', '--tail', '30', 'monitor'))

    def upgrade_session(self):
        legacy = Client(self.url)
        identifier = legacy.call('auth/register', 'POST', {'username': 'legacy_admin', 'password': 'role-password-123'}, 201)['data']['id']
        self.stop_backend()
        self.binary('migrate', 'down')
        self.sql(f"UPDATE users SET role='admin' WHERE id={identifier}")
        self.binary('migrate', 'up')
        self.start('backend')
        self.wait_backend()
        assert legacy.call('users/me')['data']['role'] == 'super_admin'
        assert legacy.call(f'admin/users/{identifier}')['data']['is_bootstrap_super_admin']
        print('PASS pre-upgrade Cookie survives legacy admin migration and Backend restart')
        self.stop_backend()
        self.sql("CREATE DATABASE gopulse_fresh; GRANT ALL ON gopulse_fresh.* TO 'gopulse'@'%'")
        self.env['MYSQL_DATABASE'] = 'gopulse_fresh'
        self.binary('migrate', 'up')
        self.start('backend')
        self.wait_backend()

    def wait_backend(self):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for _ in range(120):
            try:
                with opener.open(self.url+'/health', timeout=1) as r:
                    if r.status == 200:
                        return
            except (OSError, urllib.error.URLError):
                time.sleep(.25)
        raise AssertionError((self.directory/'backend.log').read_text()[-4000:])

    def run(self):
        self.started = True
        self.compose('up', '-d', '--wait')
        for name, package in [('backend', 'server'), ('migrate', 'migrate'), ('admin-role', 'admin-role')]:
            self.command(['go', '-C', str(ROOT/'backend'), 'build', '-o', str(self.directory/name), './cmd/'+package])
        self.migrations()
        self.binary('migrate', 'up')
        self.wait_monitor()
        self.start('backend')
        self.wait_backend()
        self.upgrade_session()
        self.roles()
        print('PASS role management: migrations, bootstrap, role matrix, sessions, audit, social regression')

    def migrations(self):
        up = (ROOT/'backend/migrations/000012_super_admin.up.sql').read_text()
        down = (ROOT/'backend/migrations/000012_super_admin.down.sql').read_text()
        for label, roles in [('empty', []), ('users', ['user', 'user']), ('single', ['user', 'admin']), ('multiple', ['admin', 'user', 'admin'])]:
            db = 'fixture_'+label
            self.sql(f'CREATE DATABASE {db}')
            self.sql("CREATE TABLE users(id BIGINT UNSIGNED PRIMARY KEY,role ENUM('user','admin') NOT NULL DEFAULT 'user') ENGINE=InnoDB", db)
            for i, role in enumerate(roles, 1):
                self.sql(f"INSERT INTO users VALUES({i},'{role}')", db)
            statements = [s.strip() for s in up.split(';') if s.strip()]
            for cutoff in (1, 2, 4):
                self.sql(';'.join(statements[:cutoff]), db)
            self.sql(up, db)
            self.sql(up, db)
            expected = '\n'.join(f'{i}\t'+('super_admin' if role == 'admin' else role) for i, role in enumerate(roles, 1))
            assert self.sql('SELECT id,role FROM users ORDER BY id', db) == expected
            roots = [i for i, role in enumerate(roles, 1) if role == 'admin']
            assert self.sql('SELECT user_id FROM bootstrap_super_admin', db) == (str(min(roots)) if roots else '')
            if roots:
                self.rejected_sql(f'DELETE FROM users WHERE id={min(roots)}', db)
                self.rejected_sql(f'INSERT INTO bootstrap_super_admin VALUES(2,{max(roots)})', db)
            self.sql(down, db)
            assert self.sql('SELECT role FROM users ORDER BY id', db) == '\n'.join(roles)
            print(f'PASS MySQL 8.4 fixture {label}: DDL replay, bootstrap, down')
        self.sql("CREATE DATABASE fixture_runner; GRANT ALL ON fixture_runner.* TO 'gopulse'@'%'")
        original = self.env['MYSQL_DATABASE']
        self.env['MYSQL_DATABASE'] = 'fixture_runner'
        try:
            self.binary('migrate', 'up')
            self.binary('migrate', 'down')
            self.sql("INSERT INTO users(username,password_hash,role) VALUES('old_admin','sensitive-fixture-hash','admin')", 'fixture_runner')
            # Real DDL failure after conversion; do not force migration history.
            self.sql('CREATE TABLE bootstrap_super_admin(unrelated INT)', 'fixture_runner')
            self.rejected_command('migrate', 'up')
            assert self.sql('SELECT version,dirty FROM schema_migrations', 'fixture_runner') == '12\t1'
            self.sql('DROP TABLE bootstrap_super_admin', 'fixture_runner')
            self.binary('migrate', 'up')
            self.binary('migrate', 'up')
            assert self.sql('SELECT version,dirty FROM schema_migrations', 'fixture_runner') == '12\t0'
            assert self.sql('SELECT role FROM users', 'fixture_runner') == 'super_admin'
            print('PASS migration runner: dirty 12 recovery, one clean history row')
        finally:
            self.env['MYSQL_DATABASE'] = original

    def rejected_sql(self, sql, database=None):
        try:
            self.sql(sql, database)
        except AssertionError:
            return
        raise AssertionError('unsafe SQL unexpectedly succeeded')

    def rejected_command(self, *args):
        try:
            self.binary(*args)
        except AssertionError:
            return
        raise AssertionError('unsafe operations command unexpectedly succeeded')

    def roles(self):
        root, other, ordinary, anonymous = [Client(self.url) for _ in range(4)]
        ids = []
        for c, name in [(root, 'root_admin'), (other, 'other_admin'), (ordinary, 'social_user')]:
            ids.append(c.call('auth/register', 'POST', {'username': name, 'password': 'role-password-123'}, 201)['data']['id'])
        root_id, other_id, user_id = ids
        # Empty bootstrap remains social-usable but cannot serve management.
        self.sql(f"UPDATE users SET role='super_admin' WHERE id={root_id}")
        assert root.call('admin/audit-events', status=503)['error']['code'] == 'management_setup_unavailable'
        self.sql(f"UPDATE users SET role='user' WHERE id={root_id}")
        # Audit failure must roll back BOTH role and bootstrap declaration.
        self.sql("CREATE TRIGGER reject_audit BEFORE INSERT ON management_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sensitive-fixture-hash'")
        self.rejected_command('admin-role', 'bootstrap', '--user-id', str(root_id))
        assert self.sql('SELECT COUNT(*) FROM bootstrap_super_admin') == '0'
        assert self.sql(f'SELECT role FROM users WHERE id={root_id}') == 'user'
        self.sql('DROP TRIGGER reject_audit')
        self.binary('admin-role', 'bootstrap', '--user-id', str(root_id))
        self.binary('admin-role', 'bootstrap', '--user-id', str(root_id))
        self.binary('admin-role', 'promote', '--username', 'root_admin')
        self.rejected_command('admin-role', 'bootstrap', '--user-id', str(other_id))
        self.rejected_command('admin-role', 'promote', '--username', 'other_admin')
        assert root.call('users/me')['data']['role'] == 'super_admin'
        assert root.call(f'admin/users/{root_id}')['data']['is_bootstrap_super_admin'] is True
        self.rejected_sql(f'DELETE FROM users WHERE id={root_id}')
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == '1'
        for c, status, code in [(anonymous, 401, 'authentication_required'), (ordinary, 403, 'permission_denied')]:
            for target in [root_id, other_id, user_id, 999999, '0', '01', '-1', 'nope', '18446744073709551616']:
                for path, method, body in [(f'admin/users/{target}', 'GET', None), (f'admin/users/{target}/role', 'PUT', {'role': 'super_admin'})]:
                    assert c.call(path, method, body, status)['error']['code'] == code
            c.call('admin/audit-events', status=status)
        for target in ['0', '01', '+1', '-1', 'nope', '18446744073709551616']:
            root.call(f'admin/users/{target}', status=400)
            root.call(f'admin/users/{target}/role', 'PUT', {'role': 'user'}, 400)
        root.call('admin/users/999999', status=404)
        root.call('admin/users/999999/role', 'PUT', {'role': 'user'}, 404)
        root.call(f'admin/users/{root_id}/role', 'PUT', {'role': 'user'}, 409)
        for body in [{'role': 'admin'}, {'role': 'owner'}, {'role': 'user', 'extra': 1}, {}, None]:
            root.call(f'admin/users/{other_id}/role', 'PUT', body, 400)
        self.sql("CREATE TRIGGER reject_audit BEFORE INSERT ON management_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='sensitive-fixture-hash'")
        root.call(f'admin/users/{other_id}/role', 'PUT', {'role': 'super_admin'}, 500)
        assert self.sql(f'SELECT role FROM users WHERE id={other_id}') == 'user'
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == '1'
        self.sql('DROP TRIGGER reject_audit')
        assert root.call(f'admin/users/{other_id}/role', 'PUT', {'role': 'super_admin'})['data']['changed']
        assert not root.call(f'admin/users/{other_id}/role', 'PUT', {'role': 'super_admin'})['data']['changed']
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == '2'
        assert other.call('users/me')['data']['role'] == 'super_admin'
        assert not other.call(f'admin/users/{other_id}')['data']['is_bootstrap_super_admin']
        other.call(f'admin/users/{root_id}/role', 'PUT', {'role': 'user'}, 409)
        other.call('observability/metrics/catalog')
        catalog = other.call('exporter-plugins/catalog')['data']
        assert len(catalog) == 6
        for plugin in catalog:
            other.call('exporter-plugins/'+plugin['id'], status=404)
            ordinary.call('exporter-plugins/'+plugin['id'], status=403)
        self.query_regressions(other)
        paths = ['observability/metrics?invalid=1', 'observability/logs?invalid=1', 'observability/events?invalid=1', 'exporter-plugins/catalog', f'admin/users/{user_id}', 'admin/audit-events']
        for path in paths:
            ordinary.call(path, status=403)
        assert other.call(f'admin/users/{other_id}/role', 'PUT', {'role': 'user'})['data']['changed']
        for path in paths:
            other.call(path, status=403)
        assert other.call('users/me')['data']['role'] == 'user'
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == '3'
        ordinary.call('auth/login', 'POST', {'username': 'social_user', 'password': 'role-password-123'})
        assert ordinary.call('users/me')['data']['role'] == 'user'
        ordinary.call('posts', 'POST', {'title': 'Role regression', 'content': 'Phase 15 role migration social regression'}, 201)
        other.call('posts', 'POST', {'title': 'Demoted session', 'content': 'Self-demoted session remains social'}, 201)
        page = root.call('admin/audit-events?limit=1')
        seen = [page['data'][0]['id']]
        while page['meta']['next_cursor']:
            page = root.call('admin/audit-events?cursor='+page['meta']['next_cursor'])
            seen.extend(e['id'] for e in page['data'])
        assert len(seen) == len(set(seen)) == 3
        audit = root.call('admin/audit-events')['data']
        assert all(e['phase'] == 'completed' and e['outcome'] == 'succeeded' for e in audit)
        assert all(set(e['details_json']) == {'before', 'after'} for e in audit)
        assert len({e['operation_id'] for e in audit}) == 3
        assert len(root.call('admin/audit-events?action=user.role.change')['data']) == 2
        for query in ['limit=0', 'limit=101', 'action=password', 'resource_type=host', 'outcome=oops', 'cursor=forged', 'start=2020-01-01T00:00:00Z&end=2026-01-01T00:00:00Z']:
            root.call('admin/audit-events?'+query, status=400)
        token = root.call('admin/audit-events?limit=1')['meta']['next_cursor']
        root.call('admin/audit-events?cursor='+token[:-1]+('a' if token[-1] != 'a' else 'b'), status=400)
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == '3'

    def query_regressions(self, admin):
        # Empty aliases use the shipped mappings; this is authorization regression,
        # not a repeat of Phase 14 ingestion or exporter acceptance.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for name, file, constant in [('logs', 'client.go', 'templateBody'), ('events', 'events_client.go', 'eventTemplateBody')]:
            source = (ROOT/'marshaller/internal/elasticsearch'/file).read_text()
            match = re.search(r'const '+constant+r' = `([^`]+)`', source)
            assert match, constant
            template = json.loads(match.group(1))['template']
            req = urllib.request.Request(self.env['ELASTICSEARCH_URL']+'/gopulse-'+name+'-v1-role-fixture', data=json.dumps(template).encode(), method='PUT', headers={'Content-Type': 'application/json'})
            with opener.open(req, timeout=10) as response:
                assert response.status == 200
            admin.call('observability/'+name)
        admin.call('observability/metrics?metric=gopulse_redis_up&range=15m')

    def close(self):
        for process, log in reversed(self.processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            log.close()
        if self.started:
            ids = self.compose('ps', '-aq').split()
            for cid in ids:
                label = self.command(['docker', 'inspect', '-f', '{{index .Config.Labels "com.docker.compose.project"}}', cid])
                assert label == self.project
            self.compose('down', '--volumes', '--remove-orphans')
            assert not self.compose('ps', '-aq')
            print('PASS owned process/container/volume cleanup')


def main():
    if sys.argv[1:] == ['--self-test']:
        assert valid_project('gopulse-roles-0123456789ab')
        assert not any(valid_project(s) for s in ['', 'gopulse', 'gopulse-roles-../', 'gopulse-roles-0123456789ab-extra'])
        assert len(set(ports(7))) == 7
        print('PASS role verifier self-test (no Docker access)')
        return
    if sys.argv[1:]:
        raise SystemExit('usage: verify-role-management.sh [--self-test]')
    with tempfile.TemporaryDirectory(prefix='gopulse-roles-') as directory:
        acceptance = Acceptance(directory)
        try:
            acceptance.run()
        finally:
            acceptance.close()


if __name__ == '__main__':
    main()
