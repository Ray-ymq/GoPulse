"""检查单节点集群、独立运行时、网络和持久存储。"""
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
PRIVATE = ROOT / '.run/kubernetes/cluster'
os.environ['KUBECONFIG'] = str(PRIVATE / 'admin.conf')


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def node_ip():
    defaults = [row for row in json.loads(run('ip', '-j', '-4', 'route')) if row.get('dst') == 'default']
    if not defaults:
        raise ValueError('没有 IPv4 默认路由，无法选择节点地址')
    route = min(defaults, key=lambda row: row.get('metric', 0))
    if route.get('prefsrc'):
        return route['prefsrc']
    if not route.get('gateway'):
        raise ValueError('默认路由没有网关或源地址，请先检查主机网络')
    return json.loads(run('ip', '-j', '-4', 'route', 'get', route['gateway'], 'oif', route['dev']))[0]['prefsrc']


def check_node_address(actual):
    config = (PRIVATE / 'kubeadm.yaml').read_text()
    expected = re.search(r'^  advertiseAddress: (\S+)\s*$', config, re.MULTILINE)
    assert expected and actual == expected[1], 'Node 地址与私有 kubeadm 配置不符，按 deploy/README.md 检查'
    addresses = json.loads(run('ip', '-j', '-4', 'address', 'show', 'scope', 'global'))
    assert any(actual == entry.get('local') for interface in addresses if interface['ifname'] != 'lo'
               for entry in interface.get('addr_info', [])), '节点地址已不在本机，按 deploy/README.md 恢复，禁止自动重置'


def routes():
    for route in json.loads(run('ip', '-j', '-4', 'route')):
        dst = route.get('dst', 'default')
        if dst == 'default':
            continue
        network = ipaddress.ip_network(dst, strict=False)
        for reserved in ('10.244.0.0/16', '10.96.0.0/12'):
            if network.overlaps(ipaddress.ip_network(reserved)) and route.get('dev') not in ('cni0', 'flannel.1'):
                raise ValueError(f'路由 {dst} 与 {reserved} 冲突')
    print('Pod 和 Service 网段未发现路由冲突。')


def addons():
    for name in ('flannel', 'local-path'):
        path = ROOT / f'deploy/kubernetes/cluster/{name}.yaml'
        run('kubectl', 'apply', '-f', str(path))


def verify():
    run('kubectl', 'wait', 'node/gopulse-wsl', '--for=condition=Ready', '--timeout=60s')
    from k8s import owned_namespace
    owned_namespace('cluster-check')
    namespace = {'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': 'gopulse-cluster-check', 'annotations': {'gopulse.io/workspace': str(ROOT)}}}
    run('kubectl', 'apply', '-f', '-', input=json.dumps(namespace))
    claim = {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim', 'metadata': {'name': 'storage-check'},
             'spec': {'accessModes': ['ReadWriteOnce'], 'storageClassName': 'local-path', 'resources': {'requests': {'storage': '64Mi'}}}}
    run('kubectl', '-n', 'gopulse-cluster-check', 'apply', '-f', '-', input=json.dumps(claim))
    for phase in ('write', 'read'):
        run('kubectl', '-n', 'gopulse-cluster-check', 'delete', 'pod', 'network-storage-check', '--ignore-not-found', '--wait=true')
        script = 'nslookup kubernetes.default.svc.cluster.local && wget -qO- http://network-check:8080 && '
        script += 'echo gopulse-storage-ok > /data/probe' if phase == 'write' else 'test "$(cat /data/probe)" = gopulse-storage-ok'
        pod = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': 'network-storage-check'}, 'spec': {
            'restartPolicy': 'Never', 'containers': [{'name': 'check', 'image': 'busybox:1.37.0',
            'command': ['sh', '-ec', script], 'volumeMounts': [{'name': 'data', 'mountPath': '/data'}]}],
            'volumes': [{'name': 'data', 'persistentVolumeClaim': {'claimName': 'storage-check'}}]}}
        if phase == 'write':
            service = {'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'network-check'},
                       'spec': {'selector': {'app': 'network-check'}, 'ports': [{'port': 8080}]}}
            server = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': {'name': 'network-check', 'labels': {'app': 'network-check'}},
                      'spec': {'containers': [{'name': 'http', 'image': 'busybox:1.37.0',
                               'command': ['sh', '-c', 'mkdir -p /www; echo service-ok > /www/index.html; httpd -f -p 8080 -h /www']}]}}
            run('kubectl', '-n', 'gopulse-cluster-check', 'apply', '-f', '-', input=json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': [service, server]}))
            run('kubectl', '-n', 'gopulse-cluster-check', 'wait', 'pod/network-check', '--for=condition=Ready', '--timeout=120s')
        run('kubectl', '-n', 'gopulse-cluster-check', 'apply', '-f', '-', input=json.dumps(pod))
        run('kubectl', '-n', 'gopulse-cluster-check', 'wait', 'pod/network-storage-check', '--for=jsonpath={.status.phase}=Succeeded', '--timeout=180s')
        print(run('kubectl', '-n', 'gopulse-cluster-check', 'logs', 'network-storage-check'))
    run('kubectl', 'delete', 'namespace', 'gopulse-cluster-check', '--wait=true')
    print('节点、DNS、Service、镜像拉取和 Pod 重建后的 PVC 数据检查通过。')


def check():
    routes()
    print(run('kubectl', 'get', 'nodes', '-o', 'wide'))
    print(run('kubectl', 'get', 'pods', '-A'))
    for service in ('gopulse-containerd', 'kubelet'):
        assert run('sudo', '-n', 'systemctl', 'is-active', service).strip() == 'active', service + ' 尚未就绪'
    print(run('kubeadm', 'version', '-o', 'short'))
    assert run('kubeadm', 'version', '-o', 'short').strip() == 'v1.36.5', 'kubeadm 版本不符'
    assert run('kubelet', '--version').strip() == 'Kubernetes v1.36.5', 'kubelet 版本不符'
    versions = json.loads(run('kubectl', 'version', '-o', 'json'))
    assert versions['clientVersion']['gitVersion'] == 'v1.36.5', 'kubectl 版本不符'
    assert versions['serverVersion']['gitVersion'] == 'v1.36.5', 'API Server 版本不符'
    node = json.loads(run('kubectl', 'get', 'node', 'gopulse-wsl', '-o', 'json'))
    assert any(row['type'] == 'Ready' and row['status'] == 'True' for row in node['status']['conditions']), '节点尚未就绪'
    actual = next(a['address'] for a in node['status']['addresses'] if a['type'] == 'InternalIP')
    check_node_address(actual)
    settings = json.loads(run('kubectl', 'get', '--raw', '/api/v1/nodes/gopulse-wsl/proxy/configz'))['kubeletconfig']
    assert settings['cgroupDriver'] == 'systemd', 'kubelet 未使用 systemd cgroup'
    assert settings['failSwapOn'] is False and settings['memorySwap']['swapBehavior'] == 'NoSwap', 'kubelet swap 策略不符'
    holds = set(run('apt-mark', 'showhold').split())
    assert {'kubeadm', 'kubelet', 'kubectl'} <= holds, 'Kubernetes 软件包未锁定'


if __name__ == '__main__':
    try:
        from k8s import lock
        with lock():
            if sys.argv[1] == 'node-ip':
                print(node_ip())
            else:
                {'routes': routes, 'addons': addons, 'verify': verify, 'check': check}[sys.argv[1]]()
    except (ValueError, AssertionError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
