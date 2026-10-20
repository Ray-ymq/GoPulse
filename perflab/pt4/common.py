"""测试四的私有目录、文件校验和环境入口。"""
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.run/pt4'
os.environ['GOPULSE_PERFLAB_STATE'] = 'pt4'
sys.path.append(str(ROOT / 'perflab/pt3'))
from environment import CONTAINERS, MYSQL, environment, restore, sql, start_services  # noqa: E402


def utc():
    return datetime.now(timezone.utc).isoformat()


def sha256(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def atomic_json(path, value):
    path = Path(path)
    temp = path.with_suffix(path.suffix + '.tmp')
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    temp.chmod(0o600)
    temp.replace(path)


def verify_prepared():
    prepared = json.loads((STATE / 'prepared.json').read_text())
    if not prepared.get('tools_ready'):
        raise RuntimeError('测试四工具尚未准备完成')
    for relative, expected in prepared['sha256'].items():
        if sha256(ROOT / relative) != expected:
            raise RuntimeError('准备后文件发生变化，必须重新执行 prepare.py：' + relative)
    for tool in prepared['tools'].values():
        if sha256(tool['path']) != tool['sha256']:
            raise RuntimeError('准备后工具二进制发生变化，必须重新执行 prepare.py')
    return prepared


def legacy_module(name):
    spec = importlib.util.spec_from_file_location('pt4_legacy_' + name, ROOT / 'perflab/pt3' / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def rows(sample, name):
    return sample.get('queries', {}).get(name, {}).get('rows', [])


def parse_event(line):
    record = json.loads(line)
    message = record.get('msg', '')
    return json.loads(message[4:]) if message.startswith('PT4 ') else None
