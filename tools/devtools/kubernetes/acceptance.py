"""通过真实 API 验收当前模式，结果写入私有记录。"""
import argparse
from datetime import datetime, timezone
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import k8s


def now():
    return datetime.now(timezone.utc).isoformat()


class APIError(ValueError):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


class API:
    def __init__(self, base):
        self.base = base
        self.client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, method, path, body=None, headers=None, expected=200):
        request = urllib.request.Request(self.base + path, method=method, data=None if body is None else json.dumps(body).encode(),
                                         headers={'Content-Type': 'application/json', **(headers or {})})
        try:
            with self.client.open(request, timeout=15) as response:
                status, raw = response.status, response.read()
                self.last_request_id = response.headers.get("X-Request-ID", "")
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
            self.last_request_id = error.headers.get("X-Request-ID", "")
        value = json.loads(raw) if raw else {}
        if status != expected:
            code = value.get('error', {}).get('code', '')
            raise APIError(status, f'{method} {path.split("?")[0]} 返回 {status} {code}，预期 {expected}')
        return value


def wait(description, check, seconds=120):
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        try:
            if value := check():
                return value
        except (ValueError, urllib.error.URLError) as error:
            last = error
        time.sleep(2)
    raise ValueError(description + ' 超时' + (f'：{last}' if last else ''))


def user(api, mode, name='acceptance-user'):
    path = k8s.PRIVATE / mode / (name + '.json')
    if path.exists():
        values = json.loads(path.read_text())
    else:
        values = {'username': 'k8s_' + secrets.token_hex(8), 'password': secrets.token_hex(24)}
        path.write_text(json.dumps(values) + '\n')
        path.chmod(0o600)
    try:
        result = api.call('POST', '/api/v1/auth/login', values)
    except APIError as error:
        if error.status != 401:
            raise
        result = api.call('POST', '/api/v1/auth/register', values, expected=201)
    return values, result['data']['id']


def business(api, mode, evidence):
    marker = 'k8s' + secrets.token_hex(8)
    context = api.call('GET', '/api/v1/post-commands/context')['data']
    headers = {'Idempotency-Key': str(uuid.uuid4()), 'X-GoPulse-Data-Epoch': context['data_epoch']}
    post = api.call('POST', '/api/v1/posts', {'title': marker, 'content': marker + ' persistence acceptance'}, headers=headers, expected=201)
    post_id = post['data']['id']
    assert api.call('GET', f'/api/v1/posts/{post_id}')['data']['title'] == marker
    replay = api.call('POST', '/api/v1/posts', {'title': marker, 'content': marker + ' persistence acceptance'}, headers=headers, expected=201)
    assert replay['data']['id'] == post_id
    peer = API(api.base)
    user(peer, mode, 'acceptance-peer')
    peer.call('PUT', f'/api/v1/posts/{post_id}/like', expected=204)
    peer.call('POST', f'/api/v1/posts/{post_id}/comments', {'content': marker}, expected=201)
    wait('异步通知', lambda: any(str(row.get('post_id')) == str(post_id) for row in api.call('GET', '/api/v1/notifications')['data']))
    wait('搜索索引', lambda: any(row['id'] == post_id for row in api.call('GET', '/api/v1/search/posts?q=' + marker)['data']))
    evidence.update(business={'post_id': post_id, 'title': marker, 'idempotent_replay': True, 'async_notification': True, 'search': True})




def accept(mode, reuse_access=False):
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署')
    if not reuse_access:
        k8s.access(mode)
    base = 'http://127.0.0.1:' + '18080'
    api = API(base)
    values, user_id = wait('登录服务', lambda: user(api, mode))
    ns = k8s.namespace(mode)
    api.call('POST', '/api/v1/auth/login', values)
    evidence = {'mode': mode, 'started_at': now(), 'revision': k8s.identity()[1], 'login': True}
    business(api, mode, evidence)
    evidence['images'] = {row['metadata']['name']: [c['image'] for c in row['spec']['containers']] for row in json.loads(k8s.kube('-n', ns, 'get', 'pods', '-o', 'json'))['items'] if row['status']['phase'] == 'Running'}
    evidence['finished_at'] = now()
    path = k8s.PRIVATE / mode / 'acceptance.json'
    path.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    print('功能验收通过：' + str(path))


def recreate(mode):
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署')
    path = k8s.PRIVATE / mode / 'acceptance.json'
    evidence = json.loads(path.read_text())
    ns = k8s.namespace(mode)
    selectors = ['mysql', 'redis']
    previous = {}
    for name in selectors:
        rows = json.loads(k8s.kube('-n', ns, 'get', 'pods', '-l', 'app.kubernetes.io/name=' + name, '-o', 'json'))['items']
        assert len(rows) == 1
        previous[name] = rows[0]['metadata']['uid']
        k8s.kube('-n', ns, 'delete', 'pod', rows[0]['metadata']['name'], '--wait=true')
    for name in selectors:
        k8s.kube('-n', ns, 'wait', 'pod', '-l', 'app.kubernetes.io/name=' + name, '--for=condition=Ready', '--timeout=300s')
        rows = json.loads(k8s.kube('-n', ns, 'get', 'pods', '-l', 'app.kubernetes.io/name=' + name, '-o', 'json'))['items']
        assert len(rows) == 1 and rows[0]['metadata']['uid'] != previous[name]
    k8s.access(mode)
    api = API('http://127.0.0.1:' + '18080')
    wait('用户登录恢复', lambda: user(api, mode))
    post = evidence['business']
    assert api.call('GET', '/api/v1/posts/' + str(post['post_id']))['data']['title'] == post['title']
    evidence['pod_recreation'] = {'checked_at': now(), 'previous_uids': previous, 'data_restored': True}
    path.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    path.with_name('persistence.json').write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    print('Pod 重建及持久数据恢复通过：' + mode)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--recreate', action='store_true')
    parser.add_argument('--mode', choices=k8s.MODES, default='all')
    args = parser.parse_args()
    with k8s.lock():
        (recreate if args.recreate else accept)(args.mode)
