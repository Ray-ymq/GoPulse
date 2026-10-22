"""准备专用环境中的数据、工具和观测；不部署业务。"""
import json
import os
from pathlib import Path
import secrets

from .common import STATE, ROOT, command, digest, private_env, containers, drained
from .config import compose_resources, contract, fingerprint
from .runtime import utc, write_json
from .environment import source_hashes, snapshot
from . import baseline


def prepare(config):
    directory = STATE / 'private'
    env = private_env(directory)
    if env.get('MYSQL_DATABASE') != 'gopulse_pt' or env.get('MYSQL_USER') != 'pt':
        raise ValueError('准备只允许 MYSQL_DATABASE=gopulse_pt、MYSQL_USER=pt 的专用环境')
    if not containers():
        raise ValueError('专用环境未部署；按操作文档显式部署，工具准备不会部署业务')
    write_json(directory / 'resources.json', compose_resources(config['resources']))
    value = contract('preparation', ready=False, directory=str(directory), started_at=utc())
    write_json(STATE / 'prepared.json', value)
    initial = snapshot(config)
    (directory / 'bin').mkdir(exist_ok=True)
    for binary in ('recipe', 'validate-corpus', 'pt-load', 'dbprobe'):
        command(['env', 'CGO_ENABLED=0', 'go', '-C', 'perflab', 'build', '-o', str(directory / 'bin' / binary), './cmd/' + binary],
                timeout=600, log=directory / 'build-tools.log')
    password = directory / 'load-password'
    if not password.exists():
        password.write_text(secrets.token_hex(24))
    (directory / 'mysql.dsn').write_text(f"pt:{env['MYSQL_PASSWORD']}@tcp(mysql:3306)/gopulse_pt?parseTime=true&multiStatements=true&timeout=5s&readTimeout=120s&writeTimeout=120s")
    if not (directory / 'baseline.sql').exists():
        baseline.seed(directory)
        baseline.reindex(directory)
        baseline.backup(directory)
    baseline.restore(directory)
    baseline.smoke(directory)
    baseline.restore(directory)
    if snapshot(config) != initial:
        # restart 会改变 StartedAt；仅允许本次明确基线恢复造成的应用启动时间变化。
        current = snapshot(config)
        for name, row in initial['containers'].items():
            if {k: v for k, v in row.items() if k != 'started_at'} != {k: v for k, v in current['containers'][name].items() if k != 'started_at'}:
                raise ValueError('准备期间容器身份或配置改变')
    command(['docker', 'exec', 'gopulse-pt-business-rabbitmq-1', 'rabbitmq-plugins', 'enable', 'rabbitmq_prometheus'], timeout=30)
    from .diagnose import prepare as prepare_diagnostics, artifacts
    value['diagnostics'] = prepare_diagnostics(directory)
    from .observe import precheck
    precheck(directory)
    names = ['test.env', 'resources.json', 'mysql.dsn', 'load-password', 'corpus.json', 'credentials.json', 'baseline.sql', 'baseline.json',
             'bin/recipe', 'bin/validate-corpus', 'bin/pt-load', 'bin/dbprobe', *artifacts()]
    for name in names:
        (directory / name).chmod(0o700 if name.startswith('bin/') else 0o600)
    value.update(ready=True, environment=snapshot(config), source_sha256=source_hashes(),
                 artifact_sha256={name: digest(directory / name) for name in names},
                 resources_sha256=fingerprint(config['resources']), baseline=json.loads((directory / 'baseline.json').read_text()),
                 finished_at=utc())
    write_json(STATE / 'prepared.json', value)
    print('公共准备完成，未执行正式压测。')
