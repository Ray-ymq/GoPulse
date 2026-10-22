"""生成专用数据，完成冒烟及显式基线恢复。"""
from http.cookiejar import CookieJar
import ipaddress
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

from .common import (ROOT, PROJECT, APPS, command, compose, containers, digest,
                    drained, es, flush_redis, helper, http, private_env,
                    sql, utc, write_json)


def seed(directory):
    helper(directory, 'recipe', '--dsn-file', '/input/mysql.dsn', '--password-file', '/input/load-password',
           '--receipt', '/input/recipe.json', '--corpus', '/input/corpus.json', '--credentials', '/input/credentials.json',
           '--candidate-version', (ROOT / 'VERSION').read_text().strip(),
           '--candidate-revision', private_env(directory)['GOPULSE_REVISION'],
           timeout=1200, log=directory / 'seed.log')
    corpus = json.loads((directory / 'corpus.json').read_text())
    credentials = json.loads((directory / 'credentials.json').read_text())
    for key in ('users', 'editable_post_ids', 'delete_post_ids'):
        corpus[key] = corpus[key][:512]
    credentials['users'] = credentials['users'][:512]
    # 单接口 DELETE 以最高可能正常档执行，每个账号必须有足够的一次性语料。
    per_actor = (6400 * 330 + 511) // 512 + 2
    for start in range(0, 512):
        values = []
        for actor in range(start, start + 1):
            user = corpus['users'][actor]['id']
            values.extend(f"({user},'PT delete {actor} {i}','PT disposable corpus')" for i in range(per_actor))
        first, count = map(int, sql('INSERT INTO posts(author_id,title,content) VALUES ' + ','.join(values) +
                                    '; SELECT LAST_INSERT_ID(),ROW_COUNT();').strip().split('\t'))
        assert count == len(values), '删除语料插入数量不符'
        for offset, actor in enumerate(range(start, start + 1)):
            corpus['delete_post_ids'][actor] = list(range(first + offset * per_actor, first + (offset + 1) * per_actor))
    write_json(directory / 'corpus.json', corpus)
    write_json(directory / 'credentials.json', credentials)
    helper(directory, 'validate-corpus', '--dsn-file', '/input/mysql.dsn', '--corpus', '/input/corpus.json',
           '--credentials', '/input/credentials.json', timeout=600, log=directory / 'validate-corpus.log')


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
    assert result.returncode == 0, 'PT 数据库备份失败'
    write_json(directory / 'baseline.json', {'sql_sha256': digest(directory / 'baseline.sql'),
               'posts': int(sql('SELECT COUNT(*) FROM posts;').strip()),
               'users': int(sql('SELECT COUNT(*) FROM users;').strip()),
               'environment': sql('SELECT data_epoch,status,mode FROM business_environment;').strip()})


def smoke(directory):
    started_at = utc()
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
    post = request(0, 'POST', '/api/v1/posts', {'title': 'PT smoke', 'content': 'PT smoke'}, 201)['data']['id']
    path = f'/api/v1/posts/{post}'
    request(0, 'PATCH', path, {'title': 'PT edited', 'content': 'PT edited'})
    assert request(0, 'GET', path)['data']['content'] == 'PT edited'
    request(1, 'PUT', path + '/like', status=204)
    request(1, 'PUT', path + '/bookmark', status=204)
    request(1, 'POST', path + '/comments', {'content': 'PT async smoke'}, 201)
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        found = es('/gopulse-post-search-v1/_search', 'POST', {'query': {'term': {'post_id': post}}, 'size': 1})['hits']['hits']
        if found and found[0]['_source']['content'] == 'PT edited' and int(sql('SELECT COUNT(*) FROM notifications;').strip()) > before:
            break
        time.sleep(.5)
    else:
        raise RuntimeError('冒烟的异步通知或搜索索引未完成')
    request(0, 'DELETE', path, status=204)
    replicas = smoke_api_replicas(credentials)
    drained()
    write_json(directory / 'smoke.json', {'ok': True, 'started_at': started_at, 'finished_at': utc(),
               'api_replicas': replicas, 'checks': ['login', 'read', 'search', 'create', 'edit', 'like', 'bookmark', 'comment', 'notification', 'index', 'delete', 'replicas', 'drain']})


def smoke_api_replicas(credentials):
    """逐实例检查业务处理能力，不依赖少量请求的代理分配结果。"""
    rows = containers()
    evidence = {}
    user = credentials['users'][0]
    for role in ('backend', 'backend-2'):
        item = rows[role]
        network = item['NetworkSettings']['Networks'].get(PROJECT + '_edge')
        assert network is not None, f'{role} 缺少专用入口网络'
        address = ipaddress.ip_address(network['IPAddress'])
        assert address.is_private and address.version == 4, f'{role} 网络地址不是私有 IPv4'
        base = f'http://{address}:8080'
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(CookieJar()))
        assert http(base, 'POST', '/api/v1/auth/login', {'username': user['username'], 'password': credentials['password']}, opener)[0] == 200, f'{role} 登录失败'
        assert http(base, 'GET', '/api/v1/posts?limit=1', opener=opener)[0] == 200, f'{role} 读取失败'
        evidence[role] = {'container_id': item['Id'], 'login': True, 'read': True}
    return evidence


def restore(directory):
    rows = containers()
    assert all(rows[name]['State']['Running'] for name in APPS), '恢复前实例已停止，拒绝自动修复'
    drained()
    compose(directory, 'stop', *APPS, log=directory / 'restore.log')
    baseline = json.loads((directory / 'baseline.json').read_text())
    assert digest(directory / 'baseline.sql') == baseline['sql_sha256'], '基线快照摘要不符'
    shell = 'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql -u"$MYSQL_USER" "$MYSQL_DATABASE"'
    with (directory / 'baseline.sql').open('rb') as source:
        result = subprocess.run(['docker', 'exec', '-i', f'{PROJECT}-mysql-1', 'sh', '-c', shell],
                                stdin=source, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=600)
    assert result.returncode == 0, 'PT 数据库恢复失败'
    reindex(directory)
    flush_redis()
    compose(directory, 'up', '-d', '--wait', '--wait-timeout', '180', *APPS, timeout=300, log=directory / 'restore.log')
    drained()
    assert sql('SELECT data_epoch,status,mode FROM business_environment;').strip() == baseline['environment'], '恢复后的数据 epoch 不符'
    assert int(sql('SELECT COUNT(*) FROM posts;').strip()) == baseline['posts'], '恢复后帖子数不符'
    assert int(sql('SELECT COUNT(*) FROM users;').strip()) == baseline['users'], '恢复后账号数不符'


def verify_replicas(directory):
    smoke_record = json.loads((directory / 'smoke.json').read_text())
    evidence = {}
    for role in ('backend', 'backend-2', 'business-worker', 'business-worker-2', 'search-indexer', 'search-indexer-2'):
        total = 0
        for line in command(['docker', 'logs', '--since', smoke_record['started_at'], '--until', smoke_record['finished_at'],
                             f'{PROJECT}-{role}-1']).splitlines():
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

