"""部署自监控采集器和看板，数据保存在当前模式的 PVC 中。"""
import argparse
import json
from pathlib import Path
import secrets
import subprocess
import sys

import k8s


def up(mode):
    ns = k8s.namespace(mode)
    k8s.deployment.check("kubernetes", mode, owner=ns)
    k8s.assert_exclusive(mode)
    if not k8s.owned_namespace(mode):
        raise ValueError('必须先启动当前模式')
    raw = k8s.kube('kustomize', str(k8s.ROOT / 'deploy/selfmonitor'))
    rows = k8s.decode_resources(k8s.kube('create', '--dry-run=client', '--validate=false', '-f', '-', '-o', 'json', input=raw))
    absent = {'kafka', 'observability-elasticsearch', 'victoriametrics', 'monitor', 'marshaller'} if mode == 'business' else {
        'backend', 'business-worker', 'search-indexer', 'redis', 'rabbitmq', 'elasticsearch'} if mode == 'observability' else set()
    rows = [r for r in rows if r['metadata']['name'].removeprefix('selfmonitor-').removesuffix('-exporter') not in absent]
    for row in rows:
        row['metadata']['namespace'] = ns
        if row['kind'] == 'ConfigMap' and row['metadata']['name'] == 'selfmonitor-prometheus':
            head, *jobs = row['data']['prometheus.yaml'].split('  - job_name: ')
            row['data']['prometheus.yaml'] = head + ''.join('  - job_name: ' + job for job in jobs if job.splitlines()[0] not in absent)
    k8s.bind_config(rows)
    k8s.resource_check(rows, mode)
    directory = k8s.PRIVATE / mode
    path = directory / 'selfmonitor-credentials.json'
    def generate():
        values = {key: secrets.token_hex(32) for key in ('GRAFANA_PASSWORD', 'MYSQL_PASSWORD', 'REDIS_PASSWORD')}
        values['MYSQL_CNF'] = f'[client]\nuser=selfmonitor\npassword={values["MYSQL_PASSWORD"]}\nhost=mysql\n'
        return values
    values = k8s.ensure_credentials(mode, 'selfmonitor-credentials', 'selfmonitor-credentials.json', generate)
    role = 'gopulse-selfmonitor-' + mode
    k8s.apply([{'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRole', 'metadata': {'name': role},
                'rules': [{'apiGroups': [''], 'resources': ['nodes/proxy'], 'resourceNames': ['gopulse-wsl'], 'verbs': ['get']}]},
               {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRoleBinding', 'metadata': {'name': role},
                'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'ClusterRole', 'name': role},
                'subjects': [{'kind': 'ServiceAccount', 'name': 'selfmonitor', 'namespace': ns}]}])
    mysql_script = '''export MYSQL_PWD="$ROOT_PASSWORD"
exec mysql -hmysql -uroot -e "CREATE USER IF NOT EXISTS 'selfmonitor'@'%' IDENTIFIED BY '$OBSERVER_PASSWORD' WITH MAX_USER_CONNECTIONS 3; ALTER USER 'selfmonitor'@'%' IDENTIFIED BY '$OBSERVER_PASSWORD' WITH MAX_USER_CONNECTIONS 3; GRANT PROCESS, REPLICATION CLIENT ON *.* TO 'selfmonitor'@'%'; GRANT SELECT ON performance_schema.* TO 'selfmonitor'@'%';"
'''
    init = {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': 'selfmonitor-mysql-init', 'namespace': ns, 'labels': {'app.kubernetes.io/part-of': 'gopulse'}},
            'spec': {'backoffLimit': 3, 'activeDeadlineSeconds': 180, 'template': {'spec': {'restartPolicy': 'Never',
            'automountServiceAccountToken': False, 'containers': [{'name': 'mysql', 'image': 'mysql:8.4.0',
            'command': ['sh', '-ec', mysql_script],
            'resources': {'requests': {'cpu': '100m', 'memory': '64Mi'}, 'limits': {'cpu': '500m', 'memory': '128Mi'}}, 'env': [
                {'name': 'ROOT_PASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'gopulse-credentials', 'key': 'MYSQL_ROOT_PASSWORD'}}},
                {'name': 'OBSERVER_PASSWORD', 'valueFrom': {'secretKeyRef': {'name': 'selfmonitor-credentials', 'key': 'MYSQL_PASSWORD'}}}]}]}}}}
    k8s.resource_check(rows + [init], mode)
    k8s.kube('-n', ns, 'delete', 'job', 'selfmonitor-mysql-init', '--ignore-not-found', '--wait=true')
    k8s.apply([init])
    k8s.kube('-n', ns, 'wait', 'job/selfmonitor-mysql-init', '--for=condition=complete', '--timeout=180s')
    if 'redis' not in absent:
        # 参数从私有文件读取，命令输出不包含凭据。
        script = 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning ACL SETUSER selfmonitor on ">' + values['REDIS_PASSWORD'] + r'" +ping +info +config\|get +client\|list +slowlog\|get +latency\|latest +latency\|histogram +memory\|stats +cluster\|info +cluster\|slots +cluster\|nodes +command'
        if k8s.kube('-n', ns, 'exec', 'redis-0', '--', 'sh', '-ec', script).strip() != 'OK':
            raise ValueError('Redis 观测账号初始化失败')
        if k8s.kube('-n', ns, 'exec', 'redis-0', '--', 'sh', '-ec', 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning ACL SAVE').strip() != 'OK':
            raise ValueError('Redis 观测账号持久保存失败')
    k8s.apply(rows)
    for row in rows:
        if row['kind'] in ('Deployment', 'StatefulSet', 'DaemonSet'):
            k8s.kube('-n', ns, 'rollout', 'status', row['kind'].lower() + '/' + row['metadata']['name'], '--timeout=180s')
    print('自监控已部署。Grafana 凭据位于 ' + str(path))


def down(mode):
    if not k8s.owned_namespace(mode):
        return
    ns = k8s.namespace(mode)
    rows = json.loads(k8s.kube('-n', ns, 'get', 'deployment,statefulset,daemonset', '-o', 'json'))['items']
    for row in rows:
        name = row['metadata']['name']
        if not name.startswith('selfmonitor-'):
            continue
        if row['kind'] == 'DaemonSet':
            patch = {'spec': {'template': {'spec': {'nodeSelector': {'gopulse.io/selfmonitor-disabled': 'true'}}}}}
            k8s.kube('-n', ns, 'patch', 'daemonset', name, '--type=merge', '-p', json.dumps(patch))
        else:
            k8s.kube('-n', ns, 'scale', row['kind'].lower() + '/' + name, '--replicas=0')
    k8s.stop_forwards(mode, 'selfmonitor')
    print('自监控已请求停止，保留 PVC、Secret 和凭据。')


def status(mode):
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署')
    rows = json.loads(k8s.kube('-n', k8s.namespace(mode), 'get', 'pods', '-o', 'json'))['items']
    print(json.dumps([{'name': r['metadata']['name'], 'phase': r['status']['phase']} for r in rows
                      if r['metadata']['name'].startswith('selfmonitor-')], ensure_ascii=False, indent=2))


def access(mode):
    k8s.forward(mode, [('selfmonitor-grafana', 23000, 3000), ('selfmonitor-prometheus', 29090, 9090)], group='selfmonitor')
    print('Grafana：http://127.0.0.1:23000/；Prometheus：http://127.0.0.1:29090/')


def verify(mode):
    from acceptance import API, now, user, wait
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署')
    port = '18081' if mode == 'observability' else '18080'
    api = API('http://127.0.0.1:' + port)
    user(api, mode)
    endpoint = '/api/v1/admin/self-monitor'
    def pods():
        return [row for row in json.loads(k8s.kube('-n', k8s.namespace(mode), 'get', 'pods', '-o', 'json'))['items']
                if row['metadata']['name'].startswith('selfmonitor-') and row['status']['phase'] not in ('Succeeded', 'Failed')]
    access(mode)
    wait('自监控采集', lambda: api.call('GET', endpoint)['data']['state'] == 'running', seconds=180)
    previous = {row['metadata']['uid'] for row in pods()}
    api.call('POST', endpoint + '/stop')
    wait('自监控停止', lambda: api.call('GET', endpoint)['data']['state'] == 'stopped', seconds=120)
    wait('自监控 Pod 停止', lambda: not pods(), seconds=180)
    api.call('POST', endpoint + '/start')
    def restarted():
        rows = pods()
        return rows and len(rows) == len(previous) and all(row['metadata']['uid'] not in previous and
            any(c['type'] == 'Ready' and c['status'] == 'True' for c in row['status'].get('conditions', [])) for row in rows)
    wait('自监控 Pod 重建', restarted, seconds=180)
    access(mode)
    wait('自监控恢复采集', lambda: api.call('GET', endpoint)['data']['state'] == 'running', seconds=180)
    result = {'checked_at': now(), 'mode': mode, 'runtime': 'kubernetes',
              'stop_start': True, 'pods_recreated': True, **api.call('GET', endpoint)['data']}
    path = k8s.PRIVATE / mode / 'selfmonitor-acceptance.json'
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    print('自监控采集、受限 API 启停和数据源检查通过。')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'status', 'access', 'verify'])
    parser.add_argument('--mode', choices=k8s.MODES, default='all')
    args = parser.parse_args()
    try:
        with k8s.lock():
            {'up': up, 'down': down, 'status': status, 'access': access, 'verify': verify}[args.action](args.mode)
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
