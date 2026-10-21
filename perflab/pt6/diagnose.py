"""准备独立只读诊断账号，管理 MySQL 现场采样与官方 node_exporter。"""
import hashlib
import io
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import resource
import secrets
import shutil
import socket
import subprocess
import sys
import tarfile
import threading
import time
import urllib.request

from common import ROOT, PROJECT, APPS, command, containers, digest, private_env, sql, utc, write_json

sys.path.insert(0, str(ROOT / 'perflab'))
from diagnostics.metrics import parse_metrics
from diagnostics.mysql import DIAGNOSTIC_INSTRUMENTS, RESET_SUMMARY_TABLES
from diagnostics.system import node_command

VERSION = 1
CONSUMERS = ('global_instrumentation', 'thread_instrumentation', 'statements_digest',
             'events_statements_current', 'events_statements_history', 'events_statements_history_long',
             'events_waits_current', 'events_waits_history_long')
EXTRA_INSTRUMENTS = ("NAME LIKE 'statement/%' OR NAME LIKE 'wait/io/file/%' OR NAME='wait/lock/metadata/sql/mdl'")


def mysql_address():
    item = containers()['mysql']
    addresses = [network['IPAddress'] for network in item['NetworkSettings']['Networks'].values()]
    assert len(addresses) == 1, 'MySQL 网络地址不唯一'
    address = ipaddress.ip_address(addresses[0])
    assert address.is_private, 'MySQL 地址不属于私有网络'
    return str(address)


def artifacts():
    return ('diagnostic.dsn', 'diagnostics.json', 'bin/dbdiag', 'bin/node_exporter')


def valid_node(binary, version, arch):
    try:
        with Path(binary).open('rb') as source:
            header = source.read(20)
        machine = {'amd64': 62, 'arm64': 183}[arch]
        if header[:6] != b'\x7fELF\x02\x01' or int.from_bytes(header[18:20], 'little') != machine:
            return False
        result = subprocess.run([str(binary), '--version'], capture_output=True, text=True, timeout=5)
        return result.returncode == 0 and re.search(
            r'^node_exporter, version ' + re.escape(version) + r'(?:\s|$)',
            result.stdout + result.stderr, re.MULTILINE) is not None
    except (OSError, subprocess.SubprocessError):
        return False


def install_node(source, destination):
    if Path(source) == Path(destination):
        return
    destination = Path(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(destination.name + '.' + secrets.token_hex(4) + '.tmp')
    try:
        shutil.copyfile(source, temporary)
        temporary.chmod(0o700)
        temporary.replace(destination)
    finally:
        temporary.unlink(missing_ok=True)


def prepare_node(directory):
    """直接准备官方程序；复用本地同版本程序，仅在缺少可用副本时下载。"""
    directory = Path(directory)
    release = json.loads((ROOT / 'exporters/node/upstream.json').read_text())
    version = release['version']
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    assert platform.system() == 'Linux' and arch in release['sha256'], 'node_exporter 运行平台不受支持'
    binary = directory / 'bin/node_exporter'
    cache = ROOT / '.run/tools/node_exporter' / version / ('linux-' + arch) / 'node_exporter'
    candidates = (binary, cache, ROOT / '.run/pt4/bin/node_exporter', ROOT / '.run/pt3/bin/node_exporter')
    for candidate in candidates:
        if valid_node(candidate, version, arch):
            install_node(candidate, cache)
            if candidate != binary:
                install_node(cache, binary)
            return {'version': version, 'arch': arch, 'source': str(candidate), 'downloaded': False}
    url = f'https://github.com/prometheus/node_exporter/releases/download/v{version}/node_exporter-{version}.linux-{arch}.tar.gz'
    with urllib.request.urlopen(url, timeout=60) as response:
        data = response.read(64 * 1024**2 + 1)
    assert len(data) <= 64 * 1024**2, 'node_exporter 发布包超过大小上限'
    assert hashlib.sha256(data).hexdigest() == release['sha256'][arch], 'node_exporter 官方发布包校验失败'
    name = f'node_exporter-{version}.linux-{arch}/node_exporter'
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        members = [item for item in archive.getmembers() if item.name == name]
        assert len(members) == 1 and members[0].isfile() and 0 < members[0].size <= 64 * 1024**2, 'node_exporter 官方程序无效'
        body = archive.extractfile(members[0]).read()
    temporary = directory / ('node-exporter-' + secrets.token_hex(4) + '.tmp')
    try:
        temporary.write_bytes(body)
        temporary.chmod(0o700)
        assert valid_node(temporary, version, arch), 'node_exporter 版本或架构不符'
        install_node(temporary, cache)
        install_node(cache, binary)
    finally:
        temporary.unlink(missing_ok=True)
    return {'version': version, 'arch': arch, 'source': url, 'downloaded': True}


def prepare(directory):
    directory = Path(directory)
    (directory / 'bin').mkdir(exist_ok=True)
    command(['env', 'CGO_ENABLED=0', 'go', '-C', 'perflab', 'build', '-o',
             str(directory / 'bin/dbdiag'), './cmd/dbdiag'], timeout=600, log=directory / 'build-diagnostics.log')
    node = prepare_node(directory)
    print('PT6 诊断：' + ('下载' if node['downloaded'] else '复用') + ' node_exporter ' + node['version'], flush=True)
    env = private_env(directory)
    database = env['MYSQL_DATABASE']
    assert re.fullmatch(r'[A-Za-z0-9_]+', database), '诊断数据库名称无效'
    password = secrets.token_hex(24)
    observer = 'gopulse_pt6_observer'
    # 管理权限只用于准备账号和启用采集对象；负载和恢复期间使用只读账号。
    sql(f"CREATE USER IF NOT EXISTS '{observer}'@'%' IDENTIFIED BY '{password}' WITH MAX_USER_CONNECTIONS 4;"
        f"ALTER USER '{observer}'@'%' IDENTIFIED BY '{password}' WITH MAX_USER_CONNECTIONS 4;"
        f"GRANT PROCESS ON *.* TO '{observer}'@'%'; GRANT SELECT ON performance_schema.* TO '{observer}'@'%';"
        f"GRANT SELECT ON `{database}`.business_environment TO '{observer}'@'%';", root=True)
    names = ','.join("'" + name + "'" for name in DIAGNOSTIC_INSTRUMENTS)
    consumers = ','.join("'" + name + "'" for name in CONSUMERS)
    before = sql('SELECT NAME,ENABLED,TIMED FROM performance_schema.setup_instruments WHERE NAME IN (' + names +
                 ') OR ' + EXTRA_INSTRUMENTS + '; SELECT NAME,ENABLED FROM performance_schema.setup_consumers;', root=True)
    sql("UPDATE performance_schema.setup_instruments SET ENABLED='YES',TIMED='YES' WHERE NAME IN (" + names +
        ') OR ' + EXTRA_INSTRUMENTS + "; UPDATE performance_schema.setup_consumers SET ENABLED='YES' WHERE NAME IN (" + consumers + ');', root=True)
    dsn = directory / 'diagnostic.dsn'
    dsn.write_text(f'{observer}:{password}@tcp({mysql_address()}:3306)/{database}?parseTime=true')
    dsn.chmod(0o600)
    previous = directory / ('diagnostic-before-reset-' + secrets.token_hex(4) + '.jsonl')
    command([str(directory / 'bin/dbdiag'), '--dsn-file', str(dsn), '--once'], timeout=15, log=previous)
    previous.chmod(0o600)
    reset = (*RESET_SUMMARY_TABLES, 'performance_schema.events_statements_history_long',
             'performance_schema.events_waits_history_long', 'performance_schema.file_summary_by_event_name',
             'performance_schema.table_io_waits_summary_by_table', 'performance_schema.table_io_waits_summary_by_index_usage')
    sql(';'.join('TRUNCATE TABLE ' + table for table in reset) + ';', root=True)
    value = {'version': VERSION, 'prepared_at': utc(), 'observer': observer, 'container_id': containers()['mysql']['Id'],
             'node_exporter': node,
             'consumers': list(CONSUMERS), 'instruments': list(DIAGNOSTIC_INSTRUMENTS),
             'previous_configuration': before, 'scene_interval_seconds': .2, 'summary_interval_seconds': 5,
             'row_limit': 128, 'row_limits': {'row_locks': 4096, 'statement_history': 4096,
                                            'sync_history': 4096, 'configuration': 1024},
             'scene_data_byte_limit': 3 * 1024**2, 'sql_text_limit_characters': 2048,
             'history_mode': 'incremental_completion_timer',
             'before_reset_snapshot': previous.name, 'reset_diagnostic_tables': list(reset),
             'server_uuid': sql('SELECT @@server_uuid;', root=True).strip()}
    write_json(directory / 'diagnostics.json', value)
    return {'version': VERSION, 'artifact_sha256': {name: digest(directory / name) for name in artifacts()}}


def check_scene(row):
    assert row.get('type') == 'scene' and not row.get('errors'), '数据库现场采集失败'
    if 'configuration' in row:
        configs = {item['NAME']: item for item in row['configuration']}
        for name in CONSUMERS:
            assert configs.get(name, {}).get('enabled') == 'YES', '诊断 consumer 未启用：' + name
        for name in (*DIAGNOSTIC_INSTRUMENTS, 'wait/lock/metadata/sql/mdl', 'statement/com/Execute', 'statement/sql/commit'):
            assert configs.get(name, {}).get('enabled') == 'YES' and configs[name]['timed'] == 'YES', '诊断 instrument 未启用：' + name
        identity = row['identity'][0]
        assert int(identity['performance_schema']) == 1, 'Performance Schema 未启用'
        assert int(identity['statement_history_size']) > 0 and int(identity['wait_history_size']) > 0, '诊断历史缓冲区为空'
        assert 'configuration' not in row.get('truncated', []), '诊断配置采集被截断'


def check_metrics(body):
    metrics = parse_metrics(body)
    values = {(item['name'], tuple(sorted(item['labels'].items()))): item['value'] for item in metrics.values()}
    assert values.get(('gopulse_mysql_up', ())) == 1, 'MySQL exporter 基础采集失败'
    assert values.get(('gopulse_mysql_performance_schema_enabled', ())) == 1, 'Performance Schema 未启用'
    for name in CONSUMERS:
        assert values.get(('gopulse_mysql_performance_schema_consumer_enabled', (('consumer', name),))) == 1, '诊断 consumer 未启用：' + name
    enabled = [item for item in metrics.values() if item['name'] == 'gopulse_mysql_collector_enabled' and item['value'] == 1]
    assert len(enabled) == 17, 'MySQL 诊断采集器未全部启用'
    for item in enabled:
        assert values.get(('gopulse_mysql_collector_success', tuple(sorted(item['labels'].items())))) == 1, 'MySQL 诊断分项采集失败'
    assert not any(item['value'] for item in metrics.values() if item['name'] == 'gopulse_mysql_collector_truncated'), 'MySQL 诊断输出被截断'
    return metrics


def fetch_node(url):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=3) as response:
        body = response.read(4 * 1024**2 + 1)
    assert len(body) <= 4 * 1024**2, 'node_exporter 响应超过上限'
    text = body.decode()
    states = [item['value'] for item in parse_metrics(text).values() if item['name'] == 'node_scrape_collector_success']
    assert states and all(value == 1 for value in states), 'node_exporter 采集失败'
    return text


class Diagnostics:
    def __init__(self, preparation, directory):
        self.preparation, self.directory = Path(preparation), Path(directory)
        self.process = self.node = None
        self.failure = None
        self.last_scene = self.last_metrics = None
        self.ready = threading.Event()
        self.stopping = False
        self.lock = threading.Lock()
        self.records = 0
        self.acknowledged = {}
        self.started = time.monotonic()
        self.cpu_before = [resource.getrusage(kind) for kind in (resource.RUSAGE_SELF, resource.RUSAGE_CHILDREN)]

    @property
    def error(self):
        if self.failure:
            return self.failure
        if self.stopping:
            return None
        if any(process and process.poll() is not None for process in (self.process, self.node)):
            return 'diagnostic_process_exited'
        now = time.monotonic()
        if self.last_scene and now - self.last_scene > 5:
            return 'diagnostic_scene_stale'
        if self.last_metrics and now - self.last_metrics > 10:
            return 'diagnostic_metrics_stale'
        return None

    def start(self):
        try:
            self.configuration = json.loads((self.preparation / 'diagnostics.json').read_text())
            assert self.configuration['version'] == VERSION, '诊断配置版本不符'
            assert self.configuration['container_id'] == containers()['mysql']['Id'], 'MySQL 诊断容器身份已改变'
            assert f'@tcp({mysql_address()}:3306)' in (self.preparation / 'diagnostic.dsn').read_text(), 'MySQL 诊断地址已改变'
            self.stream = (self.directory / 'database-diagnostics.jsonl').open('x')
            self.stream_path = self.directory / 'database-diagnostics.jsonl'
            self.stream_path.chmod(0o600)
            self.log = (self.directory / 'diagnostic-process.log').open('x')
            self.log_path = self.directory / 'diagnostic-process.log'
            self.log_path.chmod(0o600)
            self.process = subprocess.Popen([str(self.preparation / 'bin/dbdiag'), '--dsn-file',
                                             str(self.preparation / 'diagnostic.dsn')],
                                            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.log, text=True, bufsize=1)
            self.reader = threading.Thread(target=self.read, daemon=True)
            self.reader.start()
            with socket.socket() as listener:
                listener.bind(('127.0.0.1', 0))
                port = listener.getsockname()[1]
            self.node_url = f'http://127.0.0.1:{port}/metrics'
            self.node = subprocess.Popen(node_command(self.preparation / 'bin/node_exporter', port),
                                         stdout=self.log, stderr=self.log)
            deadline = time.monotonic() + 15
            while not self.ready.wait(.05):
                assert not self.error, self.error
                assert time.monotonic() < deadline, '诊断启动超时'
            while True:
                try:
                    fetch_node(self.node_url)
                    break
                except Exception:
                    assert not self.error and time.monotonic() < deadline, 'node_exporter 未就绪'
                    time.sleep(.05)
            self.trigger('manual')
            return self
        except BaseException:
            self.close(final=False)
            raise

    def read(self):
        try:
            for line in self.process.stdout:
                assert len(line) < 4 * 1024**2, '诊断记录超过大小上限'
                row = json.loads(line)
                with self.lock:
                    self.stream.write(line)
                    self.stream.flush()
                    self.records += 1
                if row['type'] == 'scene':
                    check_scene(row)
                    if 'identity' in row:
                        assert row['identity'][0]['server_uuid'] == self.configuration['server_uuid'], 'MySQL 诊断实例身份已改变'
                    self.last_scene = time.monotonic()
                elif row['type'] == 'metrics':
                    assert row['status'] == 200, 'MySQL exporter 请求失败'
                    check_metrics(row['body'])
                    self.last_metrics = time.monotonic()
                    self.acknowledged[row['trigger']] = self.acknowledged.get(row['trigger'], 0) + 1
                else:
                    raise ValueError('未知诊断记录类型')
                if self.last_scene and self.last_metrics:
                    self.ready.set()
        except Exception as error:
            if not self.stopping:
                self.failure = 'diagnostic_collection_failed:' + type(error).__name__

    def trigger(self, reason):
        assert reason in ('first_failure', 'stage_end', 'recovery_end', 'manual'), '诊断触发原因无效'
        if self.process and self.process.poll() is None:
            self.process.stdin.write(reason + '\n')
            self.process.stdin.flush()

    def close(self, final=True):
        if self.stopping:
            return
        if final and self.process and self.process.poll() is None:
            try:
                expected = self.acknowledged.get('stage_end', 0) + 1
                self.trigger('stage_end')
                deadline = time.monotonic() + 4
                # 等待确认的末次汇总，避免只发送触发后就结束进程。
                while self.acknowledged.get('stage_end', 0) < expected and time.monotonic() < deadline and not self.error:
                    time.sleep(.02)
                if self.acknowledged.get('stage_end', 0) < expected:
                    self.failure = self.failure or 'diagnostic_final_snapshot_missing'
            except (BrokenPipeError, OSError):
                self.failure = self.failure or 'diagnostic_final_snapshot_failed'
        self.stopping = True
        for process in (self.process, self.node):
            if process and process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
                    self.failure = self.failure or 'diagnostic_shutdown_timeout'
        if hasattr(self, 'reader'):
            self.reader.join(timeout=5)
            if self.reader.is_alive():
                self.failure = self.failure or 'diagnostic_reader_stuck'
        for name in ('stream', 'log'):
            if hasattr(self, name):
                getattr(self, name).close()
        cpu_after = [resource.getrusage(kind) for kind in (resource.RUSAGE_SELF, resource.RUSAGE_CHILDREN)]
        write_json(self.directory / 'diagnostic-runtime.json', {'version': VERSION, 'records': self.records,
                   'error': self.failure, 'elapsed_seconds': time.monotonic() - self.started,
                   'scene_interval_seconds': .2, 'summary_interval_seconds': 5,
                   'observer_cpu_seconds': {name: {'user': after.ru_utime - before.ru_utime,
                                                   'system': after.ru_stime - before.ru_stime}
                                            for name, before, after in zip(('python', 'children_including_docker_clients'),
                                                                          self.cpu_before, cpu_after)},
                   'process_exit_codes': {name: process.returncode if process else None
                                          for name, process in (('mysql', self.process), ('node', self.node))}})


def capture_logs(directory, since):
    """按档保存有界服务日志。截尾上限写入清单，不宣称保存了全部请求。"""
    directory = Path(directory)
    result = {'since': since, 'until': utc(), 'tail_lines_per_service': 10000, 'errors': []}
    for role in ('mysql', *APPS):
        path = directory / ('service-' + role + '.log')
        try:
            command(['docker', 'logs', '--since', since, '--until', result['until'], '--tail', '10000',
                     f'{PROJECT}-{role}-1'], timeout=10, log=path)
            path.chmod(0o600)
        except Exception:
            result['errors'].append(role)
    write_json(directory / 'service-logs.json', result)
    return result
