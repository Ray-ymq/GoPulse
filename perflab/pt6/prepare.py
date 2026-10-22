"""部署 PT6、生成独立数据并完成冒烟与基线恢复；不执行正式压测。"""
import argparse
import fcntl
from http.cookiejar import CookieJar
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
import urllib.request

from common import (ROOT, STATE, PROJECT, APPS, MIX, command, compose, containers, digest,
                    drained, es, flush_redis, helper, http, private_env, source_hashes,
                    sql, utc, write_json, check_quotas, check_host)


def initialize():
    assert not containers(), 'PT6 项目已有容器，拒绝用新凭据覆盖；请核对准备记录'
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory = STATE / ('preparation-' + time.strftime('%Y%m%dT%H%M%S') + '-' + secrets.token_hex(3))
    directory.mkdir(mode=0o700)
    (directory / 'bin').mkdir()
    values = dict(line.split('=', 1) for line in (ROOT / '.env.example').read_text().splitlines()
                  if line and not line.startswith('#'))
    revision = command(['git', 'rev-parse', 'HEAD']).strip()
    values.update(GOPULSE_VERSION=(ROOT / 'VERSION').read_text().strip(), GOPULSE_REVISION=revision,
                  MYSQL_DATABASE='gopulse_pt6', MYSQL_USER='pt6', FRONTEND_PORT='18086', PUBLISHED_HOST='127.0.0.1')
    for key in ('MYSQL_PASSWORD', 'MYSQL_ROOT_PASSWORD', 'REDIS_PASSWORD', 'RABBITMQ_PASSWORD',
                'AUTH_JWT_SECRET', 'BACKEND_METRICS_TOKEN', 'BUSINESS_WORKER_METRICS_TOKEN',
                'SEARCH_INDEXER_METRICS_TOKEN', 'BUSINESS_STAGE_SERVICE_CREDENTIAL'):
        values[key] = secrets.token_hex(32)
    for role in ('BACKEND', 'BUSINESS_WORKER', 'SEARCH_INDEXER', 'FRONTEND', 'ADMIN_FRONTEND'):
        values[f'GOPULSE_{role}_IMAGE'] = f'gopulse/pt6-{role.lower().replace("_", "-")}:{revision[:12]}'
    (directory / 'test.env').write_text(''.join(f'{key}={value}\n' for key, value in values.items()))
    (directory / 'load-password').write_text(secrets.token_hex(16))
    (directory / 'mysql.dsn').write_text(f"pt6:{values['MYSQL_PASSWORD']}@tcp(mysql:3306)/gopulse_pt6?parseTime=true&multiStatements=true&timeout=5s&readTimeout=120s&writeTimeout=120s")
    return {'directory': str(directory), 'ready': False, 'started_at': utc(), 'revision': revision, 'completed': []}


def build(directory):
    for binary in ('recipe', 'validate-corpus'):
        command(['env', 'CGO_ENABLED=0', 'go', '-C', 'perflab', 'build', '-o', str(directory / 'bin' / binary), './cmd/' + binary],
                timeout=600, log=directory / 'build-tools.log')
    compose(directory, 'build', 'backend', 'business-worker', 'search-indexer', 'frontend', 'admin-frontend',
            timeout=1200, log=directory / 'build-images.log')


def seed(directory):
    helper(directory, 'recipe', '--dsn-file', '/pt6/mysql.dsn', '--password-file', '/pt6/load-password',
           '--receipt', '/pt6/recipe.json', '--corpus', '/pt6/corpus.json', '--credentials', '/pt6/credentials.json',
           '--candidate-version', (ROOT / 'VERSION').read_text().strip(),
           '--candidate-revision', private_env(directory)['GOPULSE_REVISION'],
           timeout=1200, log=directory / 'seed.log')
    corpus = json.loads((directory / 'corpus.json').read_text())
    credentials = json.loads((directory / 'credentials.json').read_text())
    for key in ('users', 'editable_post_ids', 'delete_post_ids'):
        corpus[key] = corpus[key][:512]
    credentials['users'] = credentials['users'][:512]
    for start in range(0, 512, 8):
        values = []
        for actor in range(start, start + 8):
            user = corpus['users'][actor]['id']
            values.extend(f"({user},'PT6 delete {actor} {i}','PT6 disposable corpus')" for i in range(256))
        first, count = map(int, sql('INSERT INTO posts(author_id,title,content) VALUES ' + ','.join(values) +
                                    '; SELECT LAST_INSERT_ID(),ROW_COUNT();').strip().split('\t'))
        assert count == len(values), '删除语料插入数量不符'
        for offset, actor in enumerate(range(start, start + 8)):
            corpus['delete_post_ids'][actor] = list(range(first + offset * 256, first + (offset + 1) * 256))
    write_json(directory / 'corpus.json', corpus)
    write_json(directory / 'credentials.json', credentials)
    helper(directory, 'validate-corpus', '--dsn-file', '/pt6/mysql.dsn', '--corpus', '/pt6/corpus.json',
           '--credentials', '/pt6/credentials.json', timeout=600, log=directory / 'validate-corpus.log')


def reindex(directory):
    compose(directory, 'run', '--rm', '--no-deps', '--entrypoint', '/usr/local/bin/search-reindex',
            'search-init', timeout=1200, log=directory / 'reindex.log')
    es('/_refresh', 'POST')
    assert int(es('/gopulse-post-search-v1/_count')['count']) == int(sql('SELECT COUNT(*) FROM posts;').strip())


def backup(directory):
    drained()
    shell = 'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysqldump -u"$MYSQL_USER" --single-transaction --no-tablespaces --set-gtid-purged=OFF --skip-dump-date "$MYSQL_DATABASE"'
    with (directory / 'baseline.sql').open('wb') as output:
        result = subprocess.run(['docker', 'exec', f'{PROJECT}-mysql-1', 'sh', '-c', shell],
                                stdout=output, stderr=subprocess.PIPE, timeout=600)
    assert result.returncode == 0, 'PT6 数据库备份失败'
    write_json(directory / 'baseline.json', {'sql_sha256': digest(directory / 'baseline.sql'),
               'posts': int(sql('SELECT COUNT(*) FROM posts;').strip()),
               'users': int(sql('SELECT COUNT(*) FROM users;').strip()),
               'environment': sql('SELECT data_epoch,status,mode FROM business_environment;').strip()})


def smoke(directory):
    base = 'http://127.0.0.1:18086'
    credentials = json.loads((directory / 'credentials.json').read_text())
    clients = []
    for user in credentials['users'][:2]:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(CookieJar()))
        assert http(base, 'POST', '/api/v1/auth/login', {'username': user['username'], 'password': credentials['password']}, opener)[0] == 200
        clients.append(opener)
    def request(actor, method, path, body=None, status=200):
        actual, result = http(base, method, path, body, clients[actor])
        assert actual == status, f'冒烟请求失败：{method} {path}'
        return result
    request(0, 'GET', '/api/v1/posts?limit=1')
    request(0, 'GET', '/api/v1/search/posts?q=Phase18&limit=1')
    request(0, 'GET', '/api/v1/notifications?limit=1')
    before = int(sql('SELECT COUNT(*) FROM notifications;').strip())
    post = request(0, 'POST', '/api/v1/posts', {'title': 'PT6 smoke', 'content': 'PT6 smoke'}, 201)['data']['id']
    path = f'/api/v1/posts/{post}'
    request(0, 'PATCH', path, {'title': 'PT6 edited', 'content': 'PT6 edited'})
    assert request(0, 'GET', path)['data']['content'] == 'PT6 edited'
    request(1, 'PUT', path + '/like', status=204)
    request(1, 'PUT', path + '/bookmark', status=204)
    request(1, 'POST', path + '/comments', {'content': 'PT6 async smoke'}, 201)
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        found = es('/gopulse-post-search-v1/_search', 'POST', {'query': {'term': {'post_id': post}}, 'size': 1})['hits']['hits']
        if found and found[0]['_source']['content'] == 'PT6 edited' and int(sql('SELECT COUNT(*) FROM notifications;').strip()) > before:
            break
        time.sleep(.5)
    else:
        raise RuntimeError('冒烟的异步通知或搜索索引未完成')
    request(0, 'DELETE', path, status=204)
    drained()
    write_json(directory / 'smoke.json', {'ok': True, 'finished_at': utc(), 'checks': ['login', 'read', 'search', 'create', 'edit', 'like', 'bookmark', 'comment', 'notification', 'index', 'delete', 'drain']})


def restore(directory):
    rows = containers()
    if any(not rows[name]['State']['Running'] for name in APPS):
        compose(directory, 'up', '-d', '--wait', '--wait-timeout', '180', *APPS,
                timeout=300, log=directory / 'restore.log')
    drained()
    compose(directory, 'stop', *APPS, log=directory / 'restore.log')
    baseline = json.loads((directory / 'baseline.json').read_text())
    assert digest(directory / 'baseline.sql') == baseline['sql_sha256'], '基线快照摘要不符'
    shell = 'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql -u"$MYSQL_USER" "$MYSQL_DATABASE"'
    with (directory / 'baseline.sql').open('rb') as source:
        result = subprocess.run(['docker', 'exec', '-i', f'{PROJECT}-mysql-1', 'sh', '-c', shell],
                                stdin=source, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=600)
    assert result.returncode == 0, 'PT6 数据库恢复失败'
    reindex(directory)
    flush_redis()
    compose(directory, 'up', '-d', '--wait', '--wait-timeout', '180', *APPS, timeout=300, log=directory / 'restore.log')
    drained()
    assert sql('SELECT data_epoch,status,mode FROM business_environment;').strip() == baseline['environment'], '恢复后的数据 epoch 不符'


def verify_replicas(directory):
    evidence = {}
    for role in ('backend', 'backend-2', 'business-worker', 'business-worker-2', 'search-indexer', 'search-indexer-2'):
        total = 0
        for line in command(['docker', 'logs', f'{PROJECT}-{role}-1']).splitlines():
            try:
                row = json.loads(line)
            except ValueError:
                continue
            if role.startswith('backend'):
                total += int(row.get('message') == 'http request completed' and
                             row.get('route', '').startswith('/api/v1/') and 200 <= row.get('status', 0) < 300)
            else:
                total += int(row.get('message') == 'event processed' and row.get('result') == 'success')
        assert total > 0, f'{role} 尚无业务处理成功记录'
        evidence[role] = total
    write_json(directory / 'replicas.json', evidence)


def collection_preflight(directory):
    from sample import Sampler
    from diagnose import Diagnostics
    output = directory / ('collection-' + secrets.token_hex(3))
    output.mkdir(mode=0o700)
    diagnostics = Diagnostics(directory, output).start()
    sampler = Sampler(output / 'resources.jsonl', diagnostics)
    try:
        sampler.start()
        deadline = time.monotonic() + 11
        while time.monotonic() < deadline:
            assert not diagnostics.error, diagnostics.error
            time.sleep(.1)
    finally:
        try:
            sampler.close()
        finally:
            diagnostics.close()
    assert not diagnostics.error, diagnostics.error
    assert sampler.samples >= 3, '采集预检样本不足'
    return sampler.samples


def prepare_diagnostics(value):
    from diagnose import prepare, artifacts
    directory = Path(value['directory'])
    value['diagnostics'] = prepare(directory)
    value.setdefault('artifact_sha256', {}).update({name: digest(directory / name) for name in artifacts()})
    value['source_sha256'] = source_hashes()
    write_json(STATE / 'prepared.json', value)


def upgrade_diagnostics(value):
    from diagnose import artifacts
    assert value.get('phase') in ('ready', 'load_stopped', 'new_round_failed'), '当前阶段不能更新诊断'
    directory = Path(value['directory'])
    assert directory.parent == STATE and directory.name.startswith('preparation-'), '准备目录不属于 PT6'
    assert check_quotas() == value['containers'], '服务容器或镜像与准备记录不符'
    for name, expected in value['artifact_sha256'].items():
        if name in artifacts():
            continue
        assert digest(directory / name) == expected, '准备后私有输入已改变：' + name
    # 保留原阶段。预检中断后重试，只有原 ready 阶段能够重新就绪。
    was_ready = value['phase'] == 'ready'
    value['ready'] = False
    write_json(STATE / 'prepared.json', value)
    prepare_diagnostics(value)
    value['collection_samples'] = collection_preflight(directory)
    value.update(ready=was_ready, updated_at=utc())
    write_json(STATE / 'prepared.json', value)


def new_round(value):
    assert value.get('phase') in ('load_stopped', 'new_round_failed'), '当前准备记录不是正式压测停压状态，拒绝开始新一轮'
    assert not value.get('ready'), '当前准备记录仍为就绪状态'
    assert not (STATE / 'STOP').exists(), '存在 PT6 停止标记；核对现场后人工移除'
    leftover = command(['docker', 'ps', '-aq', '--filter', f'name={PROJECT}-k6-active']).strip()
    assert not leftover, '仍有 k6 负载容器，先确认停压完成'
    directory = Path(value['directory'])
    assert directory.parent == STATE and directory.name.startswith('preparation-'), '准备目录不属于 PT6'
    for name, expected in value['artifact_sha256'].items():
        assert digest(directory / name) == expected, '准备后私有输入已改变：' + name
    assert check_quotas() == value['containers'], '服务容器或镜像与准备记录不符'


def rearm(value, host):
    directory = Path(value['directory'])
    if value.get('phase') == 'load_stopped':
        value['round'] = value.get('round', 1) + 1
    previous = sorted(STATE.glob('run-*/report.json'))
    if previous:
        value['previous_run'] = json.loads(previous[-1].read_text()).get('directory')
    value.update(ready=False, phase='baseline_restore', updated_at=utc())
    value.pop('error', None)
    value.pop('failed_at', None)
    write_json(STATE / 'prepared.json', value)
    print('PT6 new round: restore', flush=True)
    restore(directory)
    print('PT6 new round: smoke', flush=True)
    smoke(directory)
    print('PT6 new round: restore baseline', flush=True)
    restore(directory)
    verify_replicas(directory)
    prepare_diagnostics(value)
    value['containers'] = check_quotas()
    value['source_sha256'] = source_hashes()
    value['artifact_sha256'] = {name: digest(directory / name) for name in
                                ('test.env', 'mysql.dsn', 'corpus.json', 'credentials.json', 'baseline.sql', 'baseline.json')}
    from diagnose import artifacts
    value['artifact_sha256'].update({name: digest(directory / name) for name in artifacts()})
    value.update(phase='collection_preflight', updated_at=utc(), restored_at=utc())
    write_json(directory / 'host.json', host)
    write_json(STATE / 'prepared.json', value)
    samples = collection_preflight(directory)
    value.update(ready=True, phase='ready', updated_at=utc(), collection_samples=samples)
    write_json(STATE / 'prepared.json', value)
    print(f'PT6 第 {value["round"]} 轮准备完成，尚未执行正式压测。', flush=True)


def refresh(value):
    assert value.get('ready'), '准备尚未完成，无法刷新指纹'
    directory = Path(value['directory'])
    assert not (STATE / 'STOP').exists(), '存在 PT6 停止标记；核对现场后人工移除'
    assert check_quotas() == value['containers'], '服务容器或镜像与准备记录不符'
    for name, expected in value['artifact_sha256'].items():
        assert digest(directory / name) == expected, '准备后私有输入已改变：' + name
    baseline = json.loads((directory / 'baseline.json').read_text())
    assert sql('SELECT data_epoch,status,mode FROM business_environment;').strip() == baseline['environment'], '数据 epoch 或运行模式改变'
    drained()
    assert value.get('diagnostics', {}).get('version') == 2, '先执行 prepare.py --diagnostics 准备诊断'
    collection_preflight(directory)
    value['source_sha256'] = source_hashes()
    value.update(updated_at=utc(), refreshed_at=utc())
    write_json(STATE / 'prepared.json', value)
    print('PT6 就绪指纹已刷新，环境未重建。', flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--resume', action='store_true', help='核对失败原因后继续同一准备过程')
    parser.add_argument('--new-round', action='store_true', help='恢复基线并开始新一轮正式压测准备')
    parser.add_argument('--refresh', action='store_true', help='只复核环境并刷新源码指纹')
    parser.add_argument('--diagnostics', action='store_true', help='准备并预检诊断工具，不恢复基线或发送业务负载')
    args = parser.parse_args()
    assert sum((args.resume, args.new_round, args.refresh, args.diagnostics)) <= 1, '准备模式不能组合'
    os.umask(0o077)
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock = (STATE / 'active.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    host = check_host()
    if args.diagnostics:
        value = json.loads((STATE / 'prepared.json').read_text())
        upgrade_diagnostics(value)
        print('PT6 诊断准备和预检完成，未恢复基线或执行正式压测。', flush=True)
        return
    if args.new_round:
        value = json.loads((STATE / 'prepared.json').read_text())
        new_round(value)
        try:
            rearm(value, host)
        except Exception as error:
            value.update(ready=False, phase='new_round_failed', error=str(error), failed_at=utc())
            write_json(STATE / 'prepared.json', value)
            raise
        return
    if args.refresh:
        refresh(json.loads((STATE / 'prepared.json').read_text()))
        return
    if args.resume:
        value = json.loads((STATE / 'prepared.json').read_text())
        assert not value.get('ready'), '准备已完成，无需继续'
        assert value.get('phase') not in ('load_started', 'load_stopped'), '已执行正式压测，拒绝继续旧准备过程'
    else:
        assert not (STATE / 'prepared.json').exists(), '已有准备记录，请先核对记录'
        value = initialize()
    directory = Path(value['directory'])
    steps = [
        ('build', lambda: build(directory)),
        ('dependencies', lambda: compose(directory, 'up', '-d', '--wait', '--wait-timeout', '180', 'mysql', 'redis', 'rabbitmq', 'elasticsearch', timeout=300, log=directory / 'dependencies.log')),
        ('migration', lambda: compose(directory, 'run', '--rm', '--no-deps', 'migrate', log=directory / 'migration.log')),
        ('seed', lambda: seed(directory)),
        ('index', lambda: reindex(directory)),
        ('services', lambda: compose(directory, 'up', '-d', '--wait', '--wait-timeout', '180', *APPS, timeout=300, log=directory / 'services.log')),
        ('backup', lambda: backup(directory)),
        ('smoke', lambda: smoke(directory)),
        ('restore', lambda: restore(directory)),
    ]
    try:
        for phase, operation in steps:
            if phase in value['completed']:
                continue
            value.update(phase=phase, ready=False)
            write_json(STATE / 'prepared.json', value)
            print(f'PT6 preparation: {phase}', flush=True)
            operation()
            value['completed'].append(phase)
            write_json(STATE / 'prepared.json', value)
        verify_replicas(directory)
        prepare_diagnostics(value)
        write_json(directory / 'host.json', host)
        value['containers'] = check_quotas()
        value['source_sha256'] = source_hashes()
        value['artifact_sha256'] = {name: digest(directory / name) for name in
                                   ('test.env', 'mysql.dsn', 'corpus.json', 'credentials.json', 'baseline.sql', 'baseline.json')}
        from diagnose import artifacts
        value['artifact_sha256'].update({name: digest(directory / name) for name in artifacts()})
        value.update(ready=False, phase='collection_preflight', updated_at=utc())
        value.pop('error', None)
        value.pop('failed_at', None)
        write_json(STATE / 'prepared.json', value)
        samples = collection_preflight(directory)
        value.update(ready=True, phase='ready', updated_at=utc(), collection_samples=samples)
        write_json(STATE / 'prepared.json', value)
        print('PT6 准备完成，尚未执行正式压测。', flush=True)
    except Exception as error:
        value.update(ready=False, error=str(error), failed_at=utc())
        write_json(STATE / 'prepared.json', value)
        raise


if __name__ == '__main__':
    main()
