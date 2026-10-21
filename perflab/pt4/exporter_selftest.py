"""在临时 MySQL 容器中验证 exporter 和三类检查，不访问业务测试库。"""
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tarfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from common import ROOT, STATE, atomic_json
from observe import Observation, LegacyObservation
from exporters import node_command


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class Application(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'# TYPE fixture_requests_total counter\nfixture_requests_total 1\n')


def main():
    os.umask(0o077)
    directory = STATE / 'exporter-selftest' / str(time.time_ns())
    (directory / 'bin').mkdir(parents=True)
    (directory / 'logs').mkdir()
    password = secrets.token_hex(24)
    envfile = directory / 'mysql.env'
    envfile.write_text('MYSQL_ROOT_PASSWORD=' + password + '\nMYSQL_DATABASE=pt4_fixture\nMYSQL_ROOT_HOST=%\n')
    container = 'gopulse-exporter-fixture-' + directory.name
    application = ThreadingHTTPServer(('127.0.0.1', 0), Application)
    application_thread = threading.Thread(target=application.serve_forever, daemon=True)
    application_thread.start()
    observer = thread = None
    passed = False
    try:
        subprocess.run(['docker', 'run', '--detach', '--name', container, '--memory', '768m', '--memory-swap', '768m',
                        '--cpus', '1', '--restart', 'no', '--tmpfs', '/var/lib/mysql:rw,size=512m',
                        '--env-file', str(envfile), '--publish', '127.0.0.1::3306', 'mysql:8.4.0'],
                       check=True, stdout=subprocess.DEVNULL)
        for _ in range(120):
            ready = subprocess.run(['docker', 'exec', container, 'mysqladmin', '--host=127.0.0.1', 'ping', '--silent'], capture_output=True)
            if ready.returncode == 0:
                break
            time.sleep(.5)
        else:
            raise RuntimeError('临时 MySQL 未就绪')
        port = subprocess.check_output(['docker', 'port', container, '3306/tcp'], text=True).strip().rsplit(':', 1)[1]
        schema = '''CREATE TABLE business_environment (singleton INT,data_epoch VARCHAR(10),status VARCHAR(10),mode VARCHAR(10));
INSERT INTO business_environment VALUES (1,'fixture','ready','v2');
CREATE TABLE schema_migrations (version INT,dirty INT);
INSERT INTO schema_migrations VALUES (1,0);
CREATE TABLE business_async_budget (open_requirements INT,retained_groups INT,retained_bytes INT,parked_messages INT);
INSERT INTO business_async_budget VALUES (0,0,0,0);'''
        user = 'gopulse_fixture_observer'
        schema += f"CREATE USER '{user}'@'%' IDENTIFIED BY '{password}' WITH MAX_USER_CONNECTIONS 3;"
        schema += f"GRANT PROCESS ON *.* TO '{user}'@'%'; GRANT SELECT ON performance_schema.* TO '{user}'@'%';"
        schema += ''.join(f"GRANT SELECT ON pt4_fixture.{table} TO '{user}'@'%';" for table in
                          ('business_environment', 'schema_migrations', 'business_async_budget'))
        command = ['docker', 'exec', '-i', container, 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot pt4_fixture']
        subprocess.run(command, input=schema, text=True, check=True, capture_output=True)
        for module, package, binary in [('perflab', './cmd/dbprobe', 'dbprobe'), ('exporters/mysql', '.', 'mysql-exporter')]:
            subprocess.run(['go', '-C', module, 'build', '-o', str(directory / 'bin' / binary), package], cwd=ROOT, check=True)
        archive = STATE / 'node-exporter.tar.gz'
        if not archive.exists():
            subprocess.run(['make', 'package-plugin', 'SOURCE=node', 'ARCH=amd64', 'OUTPUT=' + str(archive)], cwd=ROOT, check=True)
        with tarfile.open(archive) as source:
            (directory / 'bin/node_exporter').write_bytes(source.extractfile('bin/node_exporter').read())
        (directory / 'bin/node_exporter').chmod(0o700)
        (directory / 'probe-dsn').write_text(user + ':' + password + '@tcp(127.0.0.1:' + port + ')/pt4_fixture')
        mysql_port, node_port = free_port(), free_port()
        config = directory / 'mysql-exporter.json'
        atomic_json(config, {'config': {'host': '127.0.0.1', 'port': int(port), 'database': 'pt4_fixture', 'username': user,
                                       'connect_timeout': '500ms', 'scrape_timeout': '2s'},
                             'secrets': {'password': password},
                             'runtime': {'mode': 'host', 'listen_port': mysql_port}})
        manifest = {
            'mysql': {'url': f'http://127.0.0.1:{mysql_port}/metrics',
                      'command': [str(directory / 'bin/mysql-exporter'), 'serve', '--config', str(config)]},
            'node': {'url': f'http://127.0.0.1:{node_port}/metrics',
                     'command': node_command(directory / 'bin/node_exporter', node_port)}}
        atomic_json(directory / 'exporters.json', manifest)
        targets = {name: {'url': item['url']} for name, item in manifest.items()}
        targets.update({name: {'url': f'http://127.0.0.1:{application.server_port}/metrics'} for name in ('backend', 'worker', 'indexer')})
        globals_ = LegacyObservation.__init__.__globals__
        with patch.dict(globals_, STATE=directory, environment=lambda: {'MYSQL_DATABASE': 'pt4_fixture'}, targets=lambda _: targets):
            observer = Observation(directory / 'observations')
            thread = threading.Thread(target=observer.run)
            thread.start()
            try:
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    if observer.reason:
                        raise AssertionError('观测失败：' + observer.reason)
                    if observer.metrics_healthy and observer.last_state and observer.last_probe:
                        break
                    time.sleep(.1)
                else:
                    raise AssertionError('exporter 或专用检查未准备好')
                if observer.backlog != 0:
                    raise AssertionError('恢复积压读取错误')
                state = json.loads((observer.directory / 'database-state-latest.json').read_text())
                if set(state) != {'timestamp', 'phase', 'ok', 'identity', 'environment', 'migration', 'backlog'}:
                    raise AssertionError('环境检查包含额外指标采集')
                subprocess.run(command, input='UPDATE schema_migrations SET version=2;', text=True, check=True, capture_output=True)
                deadline = time.monotonic() + 8
                while not observer.reason and time.monotonic() < deadline:
                    time.sleep(.1)
                if observer.reason != 'business_state_or_schema_changed':
                    raise AssertionError('迁移变化未触发停压')
            finally:
                observer.stop.set()
                thread.join(timeout=12)
                if thread.is_alive() or any(child.poll() is None for child in observer.children):
                    raise AssertionError('观测或 exporter 进程未清理')
        passed = True
    finally:
        if observer:
            observer.stop.set()
        if thread:
            thread.join(timeout=12)
        application.shutdown()
        application.server_close()
        application_thread.join()
        removed = subprocess.run(['docker', 'rm', '--force', '--volumes', container], capture_output=True)
        atomic_json(directory / 'result.json', {'passed': passed, 'container_removed': removed.returncode == 0})
    if removed.returncode:
        raise RuntimeError('临时容器清理失败')
    print(json.dumps({'passed': passed, 'directory': str(directory)}, ensure_ascii=False))


if __name__ == '__main__':
    main()
