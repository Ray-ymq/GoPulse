"""测试五私有目录、容器配额与数据库入口；复用测试四已准备的测试环境。"""
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
PT4 = ROOT / '.run/pt4'
STATE = ROOT / '.run/pt5'
MYSQL = 'gopulse-loadtest-mysql-1'
CONTAINERS = [MYSQL, 'gopulse-loadtest-rabbitmq-1', 'gopulse-loadtest-elasticsearch-1']
EXPECTED_MEMORY = 2 * 1024**3
EXPECTED_SWAP = 2 * 1024**3


def utc():
    return datetime.now(timezone.utc).isoformat()


def atomic_json(path, value):
    path = Path(path)
    temp = path.with_suffix(path.suffix + '.tmp')
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temp.chmod(0o600)
    temp.replace(path)


def run(command, **kwargs):
    return subprocess.check_output(command, cwd=ROOT, text=True, stderr=subprocess.STDOUT, **kwargs).strip()


def environment():
    values = {}
    for line in (PT4 / 'test.env').read_text().splitlines():
        if line and not line.startswith('#') and '=' in line:
            key, value = line.split('=', 1)
            values[key] = value.strip().strip('"').strip("'")
    return values


def sql(query, timeout=180):
    env = environment()
    command = ['docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + env['MYSQL_PASSWORD'], MYSQL,
               'mysql', '-u' + env['MYSQL_USER'], '-N', '-B', env['MYSQL_DATABASE']]
    result = subprocess.run(command, input=query, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(result.stderr.replace(env['MYSQL_PASSWORD'], '[redacted]'))
    return result.stdout


def container():
    return json.loads(run(['docker', 'inspect', MYSQL]))[0]


def container_id():
    return container()['Id']


def cgroup_directory():
    pid = container()['State']['Pid']
    for line in Path(f'/proc/{pid}/cgroup').read_text().splitlines():
        if line.startswith('0::'):
            return Path('/sys/fs/cgroup') / line[3:].lstrip('/')
    raise RuntimeError('未找到 MySQL 容器的 cgroup v2 目录')


def cgroup(name):
    path = cgroup_directory() / name
    return path.read_text().strip() if path.exists() else None


def bytes_to_gib(value):
    return None if value is None else value / 1024**3
