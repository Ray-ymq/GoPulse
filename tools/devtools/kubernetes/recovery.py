"""记录恢复验收基线，核对启动身份、节点地址、凭据和已有业务数据。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

import acceptance
import cluster
import k8s


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def capture(mode):
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署，不能记录恢复验收')
    cluster.check()
    node = json.loads(k8s.kube('get', 'node/gopulse-wsl', '-o', 'json'))
    host_boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    node_boot = node['status']['nodeInfo']['bootID']
    if host_boot != node_boot:
        raise ValueError('Node 尚未记录本次主机启动，等待 kubelet 恢复后重试')
    ns = k8s.namespace(mode)
    namespace = json.loads(k8s.kube('get', 'namespace', ns, '-o', 'json'))
    resources = {}
    for kind in ('pvc', 'secret'):
        rows = json.loads(k8s.kube('-n', ns, 'get', kind, '-o', 'json'))['items']
        for row in rows:
            value = {'uid': row['metadata']['uid']}
            if kind == 'pvc':
                if row['status']['phase'] != 'Bound':
                    raise ValueError('PVC 尚未绑定：' + row['metadata']['name'])
                value['volume'] = row['spec']['volumeName']
            else:
                value['data_sha256'] = fingerprint(row.get('data', {}))
            resources[kind + '/' + row['metadata']['name']] = value
    directory = k8s.PRIVATE / mode
    credentials = {name: hashlib.sha256((directory / name).read_bytes()).hexdigest()
                   for name in ('credentials.json', 'selfmonitor-credentials.json')}
    evidence = json.loads((directory / 'acceptance.json').read_text())
    # 只登录既有验收账号，避免用新账号或新数据代替恢复证据。
    account = json.loads((directory / 'acceptance-user.json').read_text())
    k8s.access(mode)
    api = acceptance.API('http://127.0.0.1:' + ('18081' if mode == 'observability' else '18080'))
    api.call('POST', '/api/v1/auth/login', account)
    data = {}
    if mode != 'observability':
        post = evidence['business']
        actual = api.call('GET', '/api/v1/posts/' + str(post['post_id']))['data']
        if actual['title'] != post['title']:
            raise ValueError('原验收文章未恢复')
        data['business'] = {'post_id': post['post_id'], 'title': post['title']}
    if mode != 'business':
        plugins = api.call('GET', '/api/v1/exporter-plugins/catalog')['data']
        if not any(row['id'] == 'mysql-exporter' and row['configured'] for row in plugins):
            raise ValueError('原 MySQL 插件配置未恢复')
        if not api.call('GET', '/api/v1/observability/metrics?metric=gopulse_mysql_up')['data']['series']:
            raise ValueError('原 MySQL 插件指标不可查询')
        data['configured_plugins'] = sorted(row['id'] for row in plugins if row['configured'])
    monitoring = api.call('GET', '/api/v1/admin/self-monitor')['data']
    expected = {'business': 11, 'observability': 10, 'all': 16}[mode]
    if monitoring['state'] != 'running' or len(monitoring['targets']) != expected or not all(row['up'] for row in monitoring['targets']):
        raise ValueError('自监控尚未恢复全部预期目标')
    return {'schema': 1, 'workspace': str(k8s.ROOT), 'mode': mode, 'checked_at': acceptance.now(),
            'host_boot_id': host_boot, 'node_boot_id': node_boot, 'node_uid': node['metadata']['uid'],
            'namespace_uid': namespace['metadata']['uid'],
            'address': next(row['address'] for row in node['status']['addresses'] if row['type'] == 'InternalIP'),
            'resources': resources, 'credentials': credentials, 'data': data, 'healthy_targets': expected}


def compare(before, after, require=None):
    for key in ('schema', 'workspace', 'mode', 'node_uid', 'namespace_uid', 'resources', 'credentials', 'data', 'healthy_targets'):
        if before[key] != after[key]:
            raise ValueError('恢复前后记录不一致：' + key)
    if require == 'reboot' and before['host_boot_id'] == after['host_boot_id']:
        raise ValueError('主机 boot ID 未变化，不能记为实际重启通过')
    if require == 'address' and before['address'] == after['address']:
        raise ValueError('节点地址未变化，不能记为地址恢复通过')


def save(path, value):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    # O_EXCL 防止覆盖基线；快照只写入本机私有目录。
    with open(path, 'x', opener=lambda file, flags: os.open(file, flags, 0o600)) as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('capture', 'verify'))
    parser.add_argument('--mode', choices=k8s.MODES, default='all')
    parser.add_argument('--name', required=True, help='本机记录名，只接受字母、数字、连字符和下划线')
    parser.add_argument('--baseline', help='同一模式已有的基线记录名')
    parser.add_argument('--require', choices=('reboot', 'address'))
    args = parser.parse_args()
    def path(name):
        if not name or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-' for c in name):
            raise ValueError('记录名无效')
        return k8s.PRIVATE / args.mode / 'recovery' / (name + '.json')
    output = path(args.name)
    if output.exists():
        raise ValueError('记录已存在，请使用新的记录名')
    if args.action == 'capture' and (args.baseline or args.require):
        raise ValueError('capture 不接受基线或变化条件')
    baseline = json.loads(path(args.baseline).read_text()) if args.action == 'verify' else None
    with k8s.lock():
        current = capture(args.mode)
        if baseline is not None:
            compare(baseline, current, args.require)
        save(output, current)
    print('恢复验收记录已保存：' + str(output))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, KeyError, RuntimeError, AssertionError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
