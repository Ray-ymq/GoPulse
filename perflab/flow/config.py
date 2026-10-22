"""读取固定条件；只允许指定连续加压起点。"""
import copy
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
STAGES = ['environment', 'data', 'observation', 'idle', 'low', 'scan', 'refine', 'repeat', 'soak', 'routes', 'report']


def read(path):
    return json.loads(Path(path).read_text())


def load(start_rps='10'):
    value = read(HERE / 'config.json')
    if not str(start_rps).isascii() or not str(start_rps).isdigit() or not 0 < int(start_rps) <= value['max_rps']:
        raise ValueError(f'START_RPS 必须为 1～{value["max_rps"]} 的整数')
    value['start_rps'] = int(start_rps)
    value['resources'] = read(HERE / 'resources.json')
    return value


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def compose_resources(resources):
    # 覆盖基础 Compose 的小数 cpus，避免 float32 转换损失配额；由整数 quota/period 限制 CPU。
    return {'services': {name: {'cpus': 0, 'cpu_period': 100000, 'cpu_quota': round(row['cpus'] * 100000),
                                'mem_limit': row['memory_mib'] * 1024**2,
                                'memswap_limit': row['memory_mib'] * 1024**2, 'restart': row['restart']}
                         for name, row in resources['services'].items()}}


def contract(kind, **fields):
    return {'schema_version': f'gopulse.pt.{kind}.v1', **fields}


def validate(kind, value):
    required = {'preparation': ['source_sha256', 'artifact_sha256', 'environment', 'baseline'],
                'task': ['group_id', 'start_rps', 'stages', 'output', 'credentials_file'],
                'request': ['type', 'request_id', 'route', 'phase', 'started_at_ms'],
                'group': ['execution_completed', 'business_passed', 'stop_reason', 'recovery', 'audit', 'evidence', 'resources'],
                'result': ['groups', 'capacity', 'skipped', 'execution_completed']}
    if value.get('schema_version') != f'gopulse.pt.{kind}.v1':
        raise ValueError('数据格式版本不符：' + kind)
    missing = set(required[kind]) - value.keys()
    if missing:
        raise ValueError('缺少字段：' + ', '.join(sorted(missing)))
    return copy.deepcopy(value)
