"""构建测试四工具并生成私有清单；不恢复数据、不启动服务或业务负载。"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
from common import ROOT, STATE, CONTAINERS, atomic_json, sha256, sql, utc

BASELINE_SHA256 = '1580922c1027a5dbe3d60856664bf6e34c8de2e2d60a4ab91689e52c1e491fcd'
K6 = ROOT / '.run/tools/k6/0.56.0/linux-amd64/k6'


def run(command, **kwargs):
    return subprocess.check_output(command, cwd=ROOT, text=True, stderr=subprocess.STDOUT, **kwargs).strip()


def source_files():
    extensions = {'.go', '.mod', '.sum', '.py', '.js', '.mjs', '.json', '.sh', '.tmpl'}
    for directory in ('backend', 'runtimekit', 'perflab', 'exporters/mysql', 'exporters/node'):
        for path in (ROOT / directory).rglob('*'):
            if path.is_file() and path.suffix in extensions and '__pycache__' not in path.parts:
                yield path


def _prepare():
    os.umask(0o077)
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    for directory in ('bin', 'logs', 'snapshots', 'reports', 'observations'):
        (STATE / directory).mkdir(mode=0o700, exist_ok=True)
    baseline = ROOT / '.run/pt3/snapshots/baseline.sql'
    if sha256(baseline) != BASELINE_SHA256:
        raise RuntimeError('测试三基线快照摘要不符合测试四方案')
    for relative in ('corpus.json', 'credentials.json', 'snapshots/baseline.sql',
                     'snapshots/baseline.sha256', 'snapshots/baseline-index.json'):
        source = ROOT / '.run/pt3' / relative
        target = STATE / relative
        if not target.exists() or sha256(source) != sha256(target):
            shutil.copyfile(source, target)
        target.chmod(0o600)
    # 固定 API 连接池，避免继承测试三的连接池调整值。
    lines = (ROOT / '.run/pt3/test.env').read_text().splitlines()
    values = dict(line.split('=', 1) for line in lines if line and not line.startswith('#') and '=' in line)
    values['PT3_API_MYSQL_MAX_OPEN_CONNS'] = '8'
    values['BACKEND_REDIS_DISABLED'] = 'true'
    (STATE / 'test.env').write_text('\n'.join(key + '=' + value for key, value in values.items()) + '\n')
    (STATE / 'test.env').chmod(0o600)
    containers = json.loads(run(['docker', 'inspect', *CONTAINERS]))
    if any(item['Config']['Labels'].get('com.docker.compose.project') != 'gopulse-loadtest' for item in containers):
        raise RuntimeError('目标不是 gopulse-loadtest 测试容器')
    from exporters import prepare as prepare_exporters
    prepare_exporters()
    for name, package in (('server', 'server'), ('worker', 'business-worker'), ('indexer', 'search-indexer'), ('reindex', 'search-reindex')):
        run(['go', '-C', 'backend', 'build', '-o', str(STATE / 'bin' / name), './cmd/' + package])
    for obsolete in ('dbdiag', 'server-profile', 'worker-profile', 'indexer-profile'):
        (STATE / 'bin' / obsolete).unlink(missing_ok=True)
    tools = {}
    for name, command in {'k6': [str(K6), 'version'], 'go': ['go', 'version'], 'node': ['node', '--version'],
                          'python': ['python3', '--version'], 'docker': ['docker', '--version']}.items():
        binary = Path(shutil.which(command[0]) or command[0]).resolve()
        tools[name] = {'path': str(binary), 'sha256': sha256(binary), 'version': run(command)}
    import matplotlib
    tools['matplotlib'] = {'path': matplotlib.__file__, 'sha256': sha256(matplotlib.__file__), 'version': matplotlib.__version__}
    pins = json.loads((ROOT / 'perflab/pt4/system-tools.json').read_text())['packages']
    packages = {name: run(['dpkg-query', '-W', '-f=${Version}', name]) for name in pins}
    if packages != pins:
        raise RuntimeError('系统工具版本与 system-tools.json 不一致')
    sample = json.loads(run([str(STATE / 'bin/dbprobe'), '--dsn-file', str(STATE / 'probe-dsn'), '--details', '--once']))
    atomic_json(STATE / 'environment-precheck.json', sample)
    if not sample['ok'] or sample['identity'][0]['server_uuid'] != sql('SELECT @@server_uuid;', root=True).strip():
        raise RuntimeError('专用检查账号预检失败或指向不同的 MySQL 实例')
    artifacts = [STATE / 'bin' / name for name in ('dbprobe', 'mysql-exporter', 'node_exporter', 'server', 'worker', 'indexer', 'reindex')]
    artifacts += [p for p in (STATE / 'snapshots').iterdir() if p.is_file()]
    artifacts += [STATE / name for name in ('corpus.json', 'credentials.json', 'probe-dsn', 'test.env',
                                           'observer-password', 'mysql-exporter.json', 'exporters.json')]
    hashes = {str(path.relative_to(ROOT)): sha256(path) for path in [*source_files(), *artifacts, K6]}
    prepared = {'schema_version': 'gopulse.pt4.prepared.v1', 'tools_ready': True, 'prepared_at': utc(),
                'commit': run(['git', 'rev-parse', 'HEAD']), 'working_tree': run(['git', 'status', '--short']),
                'kernel': platform.release(), 'tools': tools, 'packages': packages, 'sha256': hashes,
                'containers': {item['Name'].lstrip('/'): item['Id'] for item in containers},
                'baseline_sha256': BASELINE_SHA256,
                'capacity_validation': '已准备 exporter；尚未执行正式压测'}
    atomic_json(STATE / 'prepared.json', prepared)
    return prepared


def prepare():
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (STATE / 'execution.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return _prepare()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    args = parser.parse_args()
    result = prepare()
    print(json.dumps({'prepared': str(STATE / 'prepared.json'), 'tools_ready': result['tools_ready'],
                      'capacity_validation': result['capacity_validation']}, ensure_ascii=False))
