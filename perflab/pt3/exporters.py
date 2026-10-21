"""复用仓库的 MySQL exporter 和官方 node_exporter，不实现数据库或系统指标采集。"""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import re
import secrets
import os
import signal
import subprocess
import tarfile
import time
import urllib.request
from urllib.error import HTTPError

from prometheus_client.parser import text_string_to_metric_families

from environment import ROOT, STATE, MYSQL, environment, sql

INTERVAL = 5
MAX_BODY = 4 << 20
NODE_COLLECTORS = ('cpu', 'meminfo', 'loadavg', 'diskstats', 'filesystem', 'netdev', 'stat', 'vmstat', 'time', 'uname')


def node_command(binary, port):
    return [str(binary), f'--web.listen-address=127.0.0.1:{port}', '--collector.disable-defaults',
            '--web.disable-exporter-metrics',
            '--collector.filesystem.mount-points-exclude=^/(dev|proc|sys|run/user|var/lib/docker/.+|var/lib/containers/storage/.+)($|/)'] + [
                '--collector.' + name for name in NODE_COLLECTORS]


def private_json(path, value):
    path = Path(path)
    temp = path.with_suffix('.tmp')
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temp.chmod(0o600)
    temp.replace(path)


def parse_metrics(body):
    result = {}
    for family in text_string_to_metric_families(body):
        for sample in family.samples:
            key = (sample.name, tuple(sorted(sample.labels.items())))
            if key in result or sample.timestamp is not None or not math.isfinite(sample.value):
                raise ValueError('指标重复、非有限或包含客户端时间戳')
            result[key] = {'name': sample.name, 'labels': dict(sample.labels), 'kind': family.type, 'value': sample.value}
    if not result:
        raise ValueError('指标响应为空')
    return result


def scalar(metrics, name, labels=None):
    return metrics[(name, tuple(sorted((labels or {}).items())))]['value']


def targets(env):
    manifest = json.loads((STATE / 'exporters.json').read_text())
    result = {name: {'url': item['url']} for name, item in manifest.items()}
    for name, port, token in [('backend', 19101, 'BACKEND_METRICS_TOKEN'),
                              ('worker', 19102, 'BUSINESS_WORKER_METRICS_TOKEN'),
                              ('indexer', 19103, 'SEARCH_INDEXER_METRICS_TOKEN')]:
        result[name] = {'url': f'http://127.0.0.1:{port}/internal/v1/metrics', 'token': env[token]}
    return result


def scrape(name, target):
    request = urllib.request.Request(target['url'])
    if target.get('token'):
        request.add_header('Authorization', 'Bearer ' + target['token'])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        response = opener.open(request, timeout=3)
    except HTTPError as error:
        response = error
    with response:
        raw = response.read(MAX_BODY + 1)
    if len(raw) > MAX_BODY:
        raise ValueError('指标响应超过大小上限')
    body = raw.decode('utf-8')
    metrics = parse_metrics(body)
    reason = None if response.status == 200 else 'HTTP_' + str(response.status)
    try:
        if name == 'mysql':
            if scalar(metrics, 'gopulse_mysql_up') != 1:
                reason = 'mysql_up_failed'
            else:
                enabled = [row for row in metrics.values() if row['name'] == 'gopulse_mysql_collector_enabled' and row['value'] == 1]
                if not enabled:
                    reason = 'collector_status_missing'
                elif any(scalar(metrics, 'gopulse_mysql_collector_success', row['labels']) != 1 for row in enabled):
                    reason = 'collector_failed'
        elif name == 'node':
            states = [row['value'] for row in metrics.values() if row['name'] == 'node_scrape_collector_success']
            if not states or any(value != 1 for value in states):
                reason = 'node_collector_failed'
    except KeyError:
        reason = 'collector_status_missing'
    return {'timestamp': datetime.now(timezone.utc).isoformat(), 'body': body, 'parsed': metrics, 'error': reason}


def scrape_all(config):
    successes, errors = {}, {}
    with ThreadPoolExecutor(max_workers=len(config)) as pool:
        futures = {name: pool.submit(scrape, name, target) for name, target in config.items()}
        for name, future in futures.items():
            try:
                successes[name] = future.result()
                if successes[name]['error']:
                    errors[name] = successes[name]['error']
            except Exception as error:
                # 不将请求头、凭据或响应正文写入错误输出。
                errors[name] = type(error).__name__
    return successes, errors


def prepare():
    env = environment()
    database = env['MYSQL_DATABASE']
    if not re.fullmatch(r'[A-Za-z0-9_]+', database) or env['MYSQL_HOST'] not in ('localhost', '127.0.0.1'):
        raise RuntimeError('只支持本机专用压测数据库')
    item = json.loads(subprocess.check_output(['docker', 'inspect', MYSQL], text=True))[0]
    bindings = item['HostConfig']['PortBindings'].get('3306/tcp') or []
    if (item['Config']['Labels'].get('com.docker.compose.project') != 'gopulse-loadtest' or
            not any(row['HostPort'] == env['MYSQL_PORT'] and row['HostIp'] == '127.0.0.1' for row in bindings)):
        raise RuntimeError('MySQL 端口或容器身份不符合专用压测环境')
    for name in ('bin', 'logs'):
        (STATE / name).mkdir(parents=True, exist_ok=True)
    password_file = STATE / 'observer-password'
    if not password_file.exists():
        password_file.write_text(secrets.token_hex(24))
    password_file.chmod(0o600)
    password = password_file.read_text().strip()
    if not re.fullmatch(r'[0-9a-f]{48}', password):
        raise RuntimeError('采样账号密码文件格式异常')
    user = 'gopulse_' + STATE.name + '_observer'
    statements = [f"CREATE USER IF NOT EXISTS '{user}'@'%' IDENTIFIED BY '{password}' WITH MAX_USER_CONNECTIONS 3",
                  f"ALTER USER '{user}'@'%' WITH MAX_USER_CONNECTIONS 3",
                  f"GRANT PROCESS ON *.* TO '{user}'@'%'",
                  f"GRANT SELECT ON performance_schema.* TO '{user}'@'%'"]
    statements += [f"GRANT SELECT ON `{database}`.`{table}` TO '{user}'@'%'" for table in
                   ('business_environment', 'schema_migrations', 'business_async_budget')]
    sql(';'.join(statements) + ';', root=True)
    dsn = STATE / 'probe-dsn'
    dsn.write_text(user + ':' + password + '@tcp(' + env['MYSQL_HOST'] + ':' + env['MYSQL_PORT'] + ')/' + database)
    dsn.chmod(0o600)
    config = STATE / 'mysql-exporter.json'
    private_json(config, {'config': {'host': env['MYSQL_HOST'], 'port': int(env['MYSQL_PORT']), 'database': database,
                                    'username': user, 'connect_timeout': '500ms', 'scrape_timeout': '2s'},
                          'secrets': {'password': password},
                          'runtime': {'mode': 'host', 'listen_host': '127.0.0.1', 'listen_port': 19122}})
    for module, package, name in [('perflab', './cmd/dbprobe', 'dbprobe'), ('exporters/mysql', '.', 'mysql-exporter')]:
        subprocess.run(['go', '-C', module, 'build', '-o', str(STATE / 'bin' / name), package], cwd=ROOT, check=True)
    archive = STATE / 'node-exporter.tar.gz'
    subprocess.run(['make', 'package-plugin', 'SOURCE=node', 'ARCH=amd64', 'OUTPUT=' + str(archive)], cwd=ROOT, check=True)
    with tarfile.open(archive) as source:
        member = source.getmember('bin/node_exporter')
        if not member.isfile():
            raise RuntimeError('node 插件缺少官方二进制')
        (STATE / 'bin/node_exporter').write_bytes(source.extractfile(member).read())
    (STATE / 'bin/node_exporter').chmod(0o700)
    manifest = {
        'mysql': {'url': 'http://127.0.0.1:19122/metrics',
                  'command': [str(STATE / 'bin/mysql-exporter'), 'serve', '--config', str(config)]},
        'node': {'url': 'http://127.0.0.1:19127/metrics',
                 'command': node_command(STATE / 'bin/node_exporter', 19127)}}
    private_json(STATE / 'exporters.json', manifest)
    return manifest


def stop_exporters():
    manifest_path = STATE / 'exporters.json'
    if not manifest_path.exists():
        return
    for name, item in json.loads(manifest_path.read_text()).items():
        path = STATE / 'logs' / (name + '-exporter.pid')
        if not path.exists():
            continue
        pid = int(path.read_text())
        command = Path(f'/proc/{pid}/cmdline')
        try:
            args = command.read_bytes().split(b'\0')
            if args and args[0] and args[0] != item['command'][0].encode():
                raise RuntimeError('exporter PID 身份不符')
            if args and args[0]:
                os.kill(pid, signal.SIGTERM)
                for _ in range(50):
                    if not command.exists() or not command.read_bytes().strip(b'\0'):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError('exporter 未按时退出')
        except (ProcessLookupError, FileNotFoundError):
            pass
        path.unlink(missing_ok=True)


if __name__ == '__main__':
    prepare()
    print('已准备压测 exporter 和三类专用检查。')
