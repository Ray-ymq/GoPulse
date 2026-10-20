"""构建测试四工具并生成私有清单；不恢复数据、不启动服务或业务负载。"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import subprocess
from common import ROOT, STATE, CONTAINERS, atomic_json, environment, sha256, sql, utc

BASELINE_SHA256 = '1580922c1027a5dbe3d60856664bf6e34c8de2e2d60a4ab91689e52c1e491fcd'
K6 = ROOT / '.run/tools/k6/0.56.0/linux-amd64/k6'


def run(command, **kwargs):
    return subprocess.check_output(command, cwd=ROOT, text=True, stderr=subprocess.STDOUT, **kwargs).strip()


def source_files():
    extensions = {'.go', '.mod', '.sum', '.py', '.js', '.mjs', '.json', '.sh', '.tmpl'}
    for directory in ('backend', 'runtimekit', 'perflab'):
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
    env = environment()
    database = env['MYSQL_DATABASE']
    if not re.fullmatch(r'[A-Za-z0-9_]+', database) or env['MYSQL_HOST'] not in ('localhost', '127.0.0.1'):
        raise RuntimeError('采样账号准备只支持当前本机独立测试实例')
    mysql_container = next(item for item in containers if item['Name'].endswith('mysql-1'))
    bindings = mysql_container['HostConfig']['PortBindings'].get('3306/tcp') or []
    if not any(row['HostPort'] == env['MYSQL_PORT'] and row['HostIp'] in ('127.0.0.1', '0.0.0.0') for row in bindings):
        raise RuntimeError('私有环境的 MySQL 端口与测试容器映射不一致')
    password_file = STATE / 'observer-password'
    if not password_file.exists():
        password_file.write_text(secrets.token_hex(24))
    password = password_file.read_text().strip()
    if not re.fullmatch(r'[0-9a-f]{48}', password):
        raise RuntimeError('私有采样账号密码文件格式异常')
    statements = ["CREATE USER IF NOT EXISTS 'gopulse_pt4_observer'@'%' IDENTIFIED BY '" + password + "' WITH MAX_USER_CONNECTIONS 5",
                  "GRANT PROCESS ON *.* TO 'gopulse_pt4_observer'@'%'",
                  "GRANT SELECT ON performance_schema.* TO 'gopulse_pt4_observer'@'%'"]
    statements += [f"GRANT SELECT ON `{database}`.`{table}` TO 'gopulse_pt4_observer'@'%'" for table in
                   ('business_environment', 'schema_migrations', 'business_async_budget')]
    sql(';'.join(statements) + ';', root=True)
    (STATE / 'probe-dsn').write_text('gopulse_pt4_observer:' + password + '@tcp(' + env['MYSQL_HOST'] + ':' + env['MYSQL_PORT'] + ')/' + database)
    (STATE / 'probe-dsn').chmod(0o600)
    for name in ('dbprobe', 'dbdiag'):
        run(['go', '-C', 'perflab', 'build', '-o', str(STATE / 'bin' / name), './cmd/' + name])
    for name, package in (('server', 'server'), ('worker', 'business-worker'), ('indexer', 'search-indexer'), ('reindex', 'search-reindex')):
        run(['go', '-C', 'backend', 'build', '-o', str(STATE / 'bin' / name), './cmd/' + package])
    from profiles import build as build_profile
    for role in ('server', 'worker', 'indexer'):
        build_profile(role)
    tools = {}
    for name, command in {'k6': [str(K6), 'version'], 'go': ['go', 'version'], 'node': ['node', '--version'],
                          'python': ['python3', '--version'], 'docker': ['docker', '--version'], 'perf': ['perf', 'version'],
                          'pidstat': ['pidstat', '-V'], 'iostat': ['iostat', '-V'], 'vmstat': ['vmstat', '-V'],
                          'graphviz': ['dot', '-V']}.items():
        binary = Path(shutil.which(command[0]) or command[0]).resolve()
        tools[name] = {'path': str(binary), 'sha256': sha256(binary), 'version': run(command)}
    import matplotlib
    tools['matplotlib'] = {'path': matplotlib.__file__, 'sha256': sha256(matplotlib.__file__), 'version': matplotlib.__version__}
    pins = json.loads((ROOT / 'perflab/pt4/system-tools.json').read_text())['packages']
    packages = {name: run(['dpkg-query', '-W', '-f=${Version}', name]) for name in pins}
    if packages != pins:
        raise RuntimeError('系统工具版本与 system-tools.json 不一致')
    for stream in ('critical', 'digest', 'locks'):
        sample = json.loads(run([str(STATE / 'bin/dbdiag'), '--dsn-file', str(STATE / 'probe-dsn'), '--stream', stream, '--once']))
        atomic_json(STATE / (stream + '-precheck.json'), sample)
        if not sample['ok']:
            raise RuntimeError('采样账号权限或查询预检失败：' + stream)
        if stream == 'critical' and sample['queries']['identity']['rows'][0]['server_uuid'] != sql('SELECT @@server_uuid;', root=True).strip():
            raise RuntimeError('采样 DSN 指向的 MySQL 与恢复目标不是同一实例')
    setup_file = STATE / 'mysql-setup.tsv'
    setup_file.write_text(sql('SELECT NAME,ENABLED FROM performance_schema.setup_consumers; '
                              "SELECT NAME,ENABLED,TIMED FROM performance_schema.setup_instruments WHERE NAME LIKE 'statement/%' OR NAME LIKE 'wait/lock/%';", root=True))
    mysql_variables = sql('SELECT @@version,@@innodb_buffer_pool_size,@@max_connections;', root=True).strip()
    artifacts = [p for p in STATE.rglob('*') if p.is_file() and p.parent.name in {'bin', 'snapshots'}]
    artifacts += [STATE / name for name in ('corpus.json', 'credentials.json', 'probe-dsn', 'test.env', 'observer-password', 'mysql-setup.tsv')]
    hashes = {str(path.relative_to(ROOT)): sha256(path) for path in [*source_files(), *artifacts, K6]}
    prepared = {'schema_version': 'gopulse.pt4.prepared.v1', 'tools_ready': True, 'prepared_at': utc(),
                'commit': run(['git', 'rev-parse', 'HEAD']), 'working_tree': run(['git', 'status', '--short']),
                'kernel': platform.release(), 'tools': tools, 'packages': packages, 'mysql_version_buffer_pool_connections': mysql_variables, 'sha256': hashes,
                'containers': {item['Name'].lstrip('/'): item['Id'] for item in containers},
                'baseline_sha256': BASELINE_SHA256,
                'capacity_validation': '尚未执行 A 阶段开销对照及正式压测'}
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
