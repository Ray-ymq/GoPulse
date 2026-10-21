"""PT6 的独立环境、容器和私有文件入口。"""
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.run/pt6'
PROJECT = 'gopulse-pt6-business'
APPS = ['backend', 'backend-2', 'platform-api', 'business-worker', 'business-worker-2',
        'search-indexer', 'search-indexer-2', 'admin-frontend', 'frontend']
SERVICES = ['mysql', 'redis', 'rabbitmq', 'elasticsearch', *APPS]
QUOTAS = {'mysql': (6, 3584), 'redis': (.5, 512), 'rabbitmq': (.5, 512),
          'elasticsearch': (2, 2048), 'backend': (.75, 256), 'backend-2': (.75, 256),
          'platform-api': (.5, 256), 'business-worker': (.5, 256),
          'business-worker-2': (.5, 256), 'search-indexer': (.5, 256),
          'search-indexer-2': (.5, 256), 'frontend': (.13, 128), 'admin-frontend': (.12, 128)}
MIX = [{'category': name, 'percent': percent} for name, percent in
       [('read', 45), ('search', 10), ('notification_bookmark_read', 10),
        ('content_write', 15), ('interaction_write', 15), ('identity_session', 5)]]


def utc():
    return datetime.now(timezone.utc).isoformat()


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def write_json(path, value):
    path = Path(path)
    temp = path.with_suffix(path.suffix + '.tmp')
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temp.chmod(0o600)
    temp.replace(path)


def manifest():
    return json.loads((STATE / 'prepared.json').read_text())


def private_env(directory):
    return dict(line.split('=', 1) for line in (Path(directory) / 'test.env').read_text().splitlines()
                if line and not line.startswith('#'))


def command(args, *, input=None, timeout=300, log=None, env=None):
    if log:
        with Path(log).open('a') as stream:
            result = subprocess.run(args, cwd=ROOT, input=input, text=True,
                                    stdout=stream, stderr=subprocess.STDOUT, timeout=timeout, env=env)
    else:
        result = subprocess.run(args, cwd=ROOT, input=input, text=True,
                                capture_output=True, timeout=timeout, env=env)
    if result.returncode:
        raise RuntimeError(f'{args[0]} 命令失败，退出码 {result.returncode}' +
                           (f'；日志：{log}' if log else ''))
    return result.stdout if not log else ''


def compose_environment(directory):
    """剔除宿主机上与被 --env-file 覆盖的同名变量，保证 compose 只读 test.env。"""
    defined = {line.split('=', 1)[0].strip() for line in (Path(directory) / 'test.env').read_text().splitlines()
               if '=' in line and not line.lstrip().startswith('#')}
    return {key: value for key, value in os.environ.items() if key not in defined}


def compose(directory, *args, **kwargs):
    return command(['docker', 'compose', '--project-name', PROJECT, '--env-file',
                    str(Path(directory) / 'test.env'), '-f', str(ROOT / 'deploy/compose.business.yaml'),
                    *args], env=compose_environment(directory), **kwargs)


def containers(include_load=False):
    ids = command(['docker', 'ps', '-aq', '--filter', f'label=com.docker.compose.project={PROJECT}']).split()
    if not ids:
        return {}
    result = json.loads(command(['docker', 'inspect', *ids]))
    return {row['Config']['Labels']['com.docker.compose.service']: row for row in result
            if row['Config']['Labels'].get('com.docker.compose.oneoff') != 'True'
            or (include_load and row['Config']['Labels']['com.docker.compose.service'] == 'k6')}


def sql(query, root=False, timeout=10):
    user, password = ('root', 'MYSQL_ROOT_PASSWORD') if root else ('$MYSQL_USER', 'MYSQL_PASSWORD')
    shell = f'export MYSQL_PWD="${password}"; exec mysql -u"{user}" -N -B "$MYSQL_DATABASE"'
    return command(['docker', 'exec', '-i', f'{PROJECT}-mysql-1', 'sh', '-c', shell], input=query, timeout=timeout)


def es(path, method='GET', data=None):
    args = ['docker', 'exec', '-i', f'{PROJECT}-elasticsearch-1', 'curl', '--fail', '--silent',
            '--show-error', '-X', method, '-H', 'Content-Type: application/json']
    if data is not None:
        args += ['--data-binary', '@-']
    args += ['http://127.0.0.1:9200' + path]
    raw = command(args, input=None if data is None else json.dumps(data))
    return json.loads(raw) if raw else {}


def helper(directory, binary, *args, **kwargs):
    return compose(directory, 'run', '--rm', '--no-deps', '--user', f'{os.getuid()}:{os.getgid()}',
                   '-v', f'{directory}:/pt6', '--entrypoint', f'/pt6/bin/{binary}', 'migrate', *args, **kwargs)


def check_quotas():
    rows = containers()
    result = {}
    for name, (cpus, mib) in QUOTAS.items():
        item = rows[name]
        config = item['HostConfig']
        assert item['State']['Running'], f'{name} 未运行'
        assert item['State'].get('Health', {}).get('Status') == 'healthy', f'{name} 未就绪'
        actual_cpu = config['NanoCpus'] / 1e9 if config['NanoCpus'] else config['CpuQuota'] / config['CpuPeriod']
        assert round(actual_cpu, 6) == cpus, f'{name} CPU 配额不符'
        assert config['Memory'] == mib * 1024**2, f'{name} 内存配额不符'
        assert config['MemorySwap'] == config['Memory'], f'{name} swap 配额不符'
        assert config['RestartPolicy']['Name'] == 'no', f'{name} 重启策略不符'
        result[name] = {'id': item['Id'], 'image': item['Image'], 'cpus': cpus, 'memory_mib': mib}
    return result


def check_host():
    cpu, memory = map(int, command(['docker', 'info', '--format', '{{.NCPU}} {{.MemTotal}}']).split())
    assert cpu >= 16 and memory >= 12 * 1024**3, 'Docker 可用资源少于 16 vCPU / 12 GiB'
    available = int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines()
                         if line.startswith('MemAvailable:'))) * 1024
    free = shutil.disk_usage(ROOT).free
    assert available >= 2 * 1024**3, '主机可用内存不足 2 GiB'
    assert free >= 10 * 1024**3, '可用磁盘不足 10 GiB'
    return {'docker_vcpus': cpu, 'docker_memory_bytes': memory,
            'host_available_memory_bytes': available, 'disk_free_bytes': free, 'checked_at': utc()}


def drained():
    values = sql('SELECT open_requirements,parked_messages FROM business_async_budget;').strip().split('\t')
    assert values == ['0', '0'], '异步需求或 parked 消息尚未归零'
    raw = command(['docker', 'exec', f'{PROJECT}-rabbitmq-1', 'rabbitmqctl', '-q', 'list_queues',
                   'name', 'messages_ready', 'messages_unacknowledged'])
    for line in raw.splitlines():
        parts = line.split('\t')
        if len(parts) == 3 and parts[1].isdigit():
            assert parts[1:] == ['0', '0'], 'RabbitMQ 队列尚未清空'


def flush_redis():
    assert command(['docker', 'exec', f'{PROJECT}-redis-1', 'redis-cli', 'FLUSHDB']).strip() == 'OK'


def source_hashes():
    paths = [ROOT / 'deploy/compose.business.yaml']
    paths += sorted((ROOT / 'perflab/pt6').glob('*.py'))
    paths += sorted((ROOT / 'perflab/pt6').glob('*.js'))
    paths += [ROOT / 'perflab/k6' / name for name in ('requests.mjs', 'sequence.mjs')]
    for relative in ('perflab/diagnostics', 'perflab/cmd/dbdiag', 'exporters/mysql'):
        paths += sorted(path for path in (ROOT / relative).rglob('*')
                        if path.suffix in ('.py', '.go', '.mod', '.sum') and '__pycache__' not in path.parts)
    paths += [ROOT / 'perflab/go.mod', ROOT / 'perflab/go.sum']
    paths += [ROOT / 'exporters/node/upstream.json']
    return {str(path.relative_to(ROOT)): digest(path) for path in paths}


def verify_prepared():
    value = manifest()
    assert value.get('ready'), 'PT6 尚未准备完成'
    assert value.get('diagnostics', {}).get('version') == 1, 'PT6 诊断尚未准备；执行 prepare.py --diagnostics'
    directory = Path(value['directory'])
    assert directory.parent == STATE and directory.name.startswith('preparation-'), '准备目录不属于 PT6'
    for path, expected in value['source_sha256'].items():
        assert digest(ROOT / path) == expected, '准备后源码或配置已改变：' + path
    for path, expected in value['artifact_sha256'].items():
        assert digest(directory / path) == expected, '准备后私有输入已改变：' + path
    return value


def http(base, method, path, data=None, opener=None):
    request = urllib.request.Request(base + path, method=method,
                                     data=None if data is None else json.dumps(data).encode())
    if data is not None:
        request.add_header('Content-Type', 'application/json')
    opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=5) as response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None
