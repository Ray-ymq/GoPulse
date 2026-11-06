"""管理三种 GoPulse 模式，保留 PVC 和凭据。"""
import argparse
import base64
import hashlib
import ipaddress
import json
import io
import tarfile
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'tools/devtools'))
import deployment
BASE = ROOT / 'deploy/kubernetes'
PRIVATE = ROOT / '.run/kubernetes'
MODES = ('business', 'observability', 'all')
os.environ['KUBECONFIG'] = str(PRIVATE / 'cluster/admin.conf')


def run(args, *, input=None, timeout=600):
    result = subprocess.run(args, input=input, text=True, capture_output=True, timeout=timeout, cwd=ROOT)
    if result.returncode:
        # Secret 的输入和命令行参数不得出现在异常中。
        raise RuntimeError(f'{args[0]} 失败，退出码 {result.returncode}；请检查对应资源事件与日志')
    return result.stdout


def kube(*args, input=None):
    return run(['kubectl', *args], input=input)


def identity():
    version = (ROOT / 'VERSION').read_text().strip()
    revision = run(['git', 'rev-parse', 'HEAD']).strip()
    return version, revision, version + '-' + revision[:12]


def decode_resources(raw):
    decoder = json.JSONDecoder()
    rows = []
    remaining = raw.lstrip()
    while remaining:
        row, end = decoder.raw_decode(remaining)
        rows.extend(row['items'] if row.get('kind') == 'List' else [row])
        remaining = remaining[end:].lstrip()
    return rows


def render(mode):
    raw = kube('kustomize', str(BASE / 'overlays' / mode))
    items = decode_resources(kube('create', '--dry-run=client', '--validate=false', '-f', '-', '-o', 'json', input=raw))
    version, revision, tag = identity()
    for item in items:
        if item['kind'] == 'Namespace':
            item['metadata'].setdefault('annotations', {})['gopulse.io/workspace'] = str(ROOT)
        if item['kind'] == 'ConfigMap' and item['metadata']['name'] == 'gopulse-identity':
            item['data'] = {'GOPULSE_VERSION': version, 'GOPULSE_REVISION': revision}
        spec = item.get('spec', {}).get('template', {}).get('spec', {})
        for container in spec.get('containers', []):
            if container['image'].startswith('gopulse/'):
                container['image'] = container['image'].split(':')[0] + ':' + tag
    if os.environ.get('TRACE') == '1':
        tracing = json.loads((BASE / 'tracing/resources.json').read_text())['items']
        for row in tracing:
            row['metadata']['namespace'] = namespace(mode)
        for row in items:
            if row['kind'] == 'ConfigMap' and row['metadata']['name'] in ('backend-env', 'platform-api-env', 'business-worker-env', 'search-indexer-env'):
                row['data'].update(GOPULSE_TRACE_ENABLED='true',
                                  GOPULSE_TRACE_ENDPOINT=f'phase20-collector.{namespace(mode)}.svc.cluster.local:4317',
                                  GOPULSE_TRACE_SAMPLE_RATIO='1.0')
        items += tracing
    return items


def bind_config(items):
    configs = {row['metadata']['name']: row.get('data', {}) for row in items if row['kind'] == 'ConfigMap'}
    for row in items:
        template = row.get('spec', {}).get('template')
        if not template:
            continue
        spec = template['spec']
        names = {ref['configMapRef']['name'] for container in spec.get('containers', []) + spec.get('initContainers', [])
                 for ref in container.get('envFrom', []) if 'configMapRef' in ref}
        names.update(volume['configMap']['name'] for volume in spec.get('volumes', []) if 'configMap' in volume)
        data = json.dumps({name: configs[name] for name in sorted(names) if name in configs}, sort_keys=True).encode()
        template.setdefault('metadata', {}).setdefault('annotations', {})['gopulse.io/config-sha256'] = hashlib.sha256(data).hexdigest()


def apply(items):
    if items:
        print(kube('apply', '-f', '-', input=json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': items})).strip())


def namespace(mode):
    return 'gopulse-' + mode


def owned_namespace(mode):
    raw = kube('get', 'namespace', namespace(mode), '--ignore-not-found', '-o', 'json').strip()
    if not raw:
        return False
    owner = json.loads(raw)['metadata'].get('annotations', {}).get('gopulse.io/workspace')
    if owner != str(ROOT):
        raise ValueError('命名空间归属不符，停止操作：' + namespace(mode))
    return True


def ensure_credentials(mode, name, filename, generate):
    directory = PRIVATE / mode
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    path = directory / filename
    raw = kube('-n', namespace(mode), 'get', 'secret', name, '--ignore-not-found', '-o', 'json').strip()
    existing = {key: base64.b64decode(value).decode() for key, value in json.loads(raw).get('data', {}).items()} if raw else None
    if path.exists():
        values = json.loads(path.read_text())
        if existing is not None and values != existing:
            raise ValueError('本地凭据与集群 Secret 不一致，拒绝自动覆盖：' + name)
    else:
        values = existing if existing is not None else generate()
        with path.open('x') as stream:
            os.chmod(path, 0o600)
            json.dump(values, stream, indent=2)
            stream.write('\n')
    path.chmod(0o600)
    apply([{'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': name, 'namespace': namespace(mode)},
            'type': 'Opaque', 'stringData': values}])
    return values


def credentials(mode):
    def generate():
        values = {key: secrets.token_hex(32) for key in json.loads((BASE / 'secret-keys.json').read_text())}
        values['RABBITMQ_URL'] = f'amqp://gopulse:{values["RABBITMQ_PASSWORD"]}@rabbitmq:5672/'
        return values
    return ensure_credentials(mode, 'gopulse-credentials', 'credentials.json', generate)


def quantity(value, memory=False):
    value = str(value)
    factors = {'Ki': 2**10, 'Mi': 2**20, 'Gi': 2**30, 'Ti': 2**40, 'K': 1000, 'M': 10**6, 'G': 10**9, 'm': .001}
    for suffix, factor in factors.items():
        if value.endswith(suffix):
            return float(value[:-len(suffix)]) * factor
    return float(value)


def resource_check(items, mode):
    node = json.loads(kube('get', 'node', 'gopulse-wsl', '-o', 'json'))
    available = {k: quantity(v) for k, v in node['status']['allocatable'].items() if k in ('cpu', 'memory')}
    requested = {'cpu': 0, 'memory': 0}
    replacing = {item.get('metadata', {}).get('name', '') for item in items if item['kind'] in ('Deployment', 'StatefulSet', 'DaemonSet', 'Pod')}
    for item in items:
        if item['kind'] not in ('Deployment', 'StatefulSet', 'DaemonSet', 'Pod'):
            continue
        spec = item['spec'] if item['kind'] == 'Pod' else item['spec']['template']['spec']
        for container in spec['containers']:
            for key in requested:
                requested[key] += quantity(container.get('resources', {}).get('requests', {}).get(key, 0)) * item['spec'].get('replicas', 1)
    # 初始化 Job 顺序执行；部署前为最大单个 Job 预留额外请求。
    peaks = {'cpu': 0, 'memory': 0}
    for item in items:
        if item['kind'] == 'Job':
            for key in peaks:
                peaks[key] = max(peaks[key], sum(quantity(c.get('resources', {}).get('requests', {}).get(key, 0)) for c in item['spec']['template']['spec']['containers']))
    for key in requested:
        requested[key] += peaks[key]
    for pod in json.loads(kube('get', 'pods', '-A', '-o', 'json'))['items']:
        if (pod['metadata']['namespace'] == namespace(mode) and pod['metadata'].get('labels', {}).get('app.kubernetes.io/name') in replacing) or pod['status']['phase'] in ('Succeeded', 'Failed'):
            continue
        for container in pod['spec']['containers']:
            for key in requested:
                available[key] -= quantity(container.get('resources', {}).get('requests', {}).get(key, 0))
    for key in requested:
        if requested[key] > available[key]:
            raise ValueError(f'{mode} 的 {key} 预算不足：需要 {requested[key]}，余量 {available[key]}')
    mem = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    if requested['memory'] > int(mem['MemAvailable'].split()[0]) * 1024:
        raise ValueError('主机可用内存不足，停止部署')
    print(f'资源请求：{requested["cpu"]:.2f} CPU，{requested["memory"] / 2**20:.0f} MiB。')


def assert_exclusive(mode):
    pods = json.loads(kube('get', 'pods', '-A', '-l', 'app.kubernetes.io/part-of=gopulse', '-o', 'json'))['items']
    for pod in pods:
        if pod['metadata']['namespace'] != namespace(mode or '') and pod['status']['phase'] not in ('Succeeded', 'Failed'):
            raise ValueError('必须先停止现有部署：' + pod['metadata']['namespace'])
    for other in MODES:
        if other == mode:
            continue
        rows = json.loads(kube('-n', namespace(other), 'get', 'deployment,statefulset', '-o', 'json'))['items']
        if any(row['spec'].get('replicas', 1) > 0 for row in rows):
            raise ValueError(f'{other} 模式仍在运行，必须先执行 make k8s-down MODE={other}')


def up(mode):
    deployment.check("kubernetes", mode, owner=namespace(mode))
    owned_namespace(mode)
    deployment.record("kubernetes", mode, namespace(mode), "starting")
    try:
        _up(mode)
    except BaseException:
        deployment.record("kubernetes", mode, namespace(mode), "failed")
        raise


def _up(mode):
    deployment.check("kubernetes", mode, owner=namespace(mode))
    assert_exclusive(mode)
    owned_namespace(mode)
    items = render(mode)
    bind_config(items)
    resource_check(items, mode)
    apply([row for row in items if row['kind'] == 'Namespace'])
    owner = PRIVATE / mode / 'owner'
    owner.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if owner.exists() and owner.read_text().strip() != str(ROOT):
        raise ValueError('命名空间由其他工作区管理')
    owner.write_text(str(ROOT) + '\n')
    credentials(mode)
    ns = namespace(mode)
    # 数据库迁移前停止应用，避免新旧实例同时修改数据。
    for row in items:
        if row['kind'] == 'Deployment' and kube('-n', ns, 'get', 'deployment', row['metadata']['name'], '--ignore-not-found', '-o', 'name').strip():
            kube('-n', ns, 'scale', 'deployment/' + row['metadata']['name'], '--replicas=0')
            kube('-n', ns, 'wait', 'pod', '-l', 'app.kubernetes.io/name=' + row['metadata']['name'], '--for=delete', '--timeout=90s')
    apply([row for row in items if row['kind'] not in ('Namespace', 'Deployment', 'Job')])
    for row in items:
        if row['kind'] == 'StatefulSet':
            print(kube('-n', ns, 'rollout', 'status', 'statefulset/' + row['metadata']['name'], '--timeout=300s').strip())
    for name in ('migrate', 'kafka-init', 'search-init'):
        jobs = [row for row in items if row['kind'] == 'Job' and row['metadata']['name'] == name]
        if jobs:
            kube('-n', ns, 'delete', 'job', name, '--ignore-not-found', '--wait=true')
            apply(jobs)
            print(kube('-n', ns, 'wait', 'job/' + name, '--for=condition=complete', '--timeout=600s').strip())
    collectors = [row for row in items if row['kind'] == 'Deployment' and row['metadata']['name'] == 'phase20-collector']
    if collectors:
        apply(collectors)
        print(kube('-n', ns, 'rollout', 'status', 'deployment/phase20-collector', '--timeout=180s').strip())
    elif kube('-n', ns, 'get', 'deployment', 'phase20-collector', '--ignore-not-found', '-o', 'name').strip():
        kube('-n', ns, 'scale', 'deployment/phase20-collector', '--replicas=0')
        kube('-n', ns, 'wait', 'pod', '-l', 'app.kubernetes.io/name=phase20-collector', '--for=delete', '--timeout=90s')
    apply([row for row in items if row['kind'] == 'Deployment' and row not in collectors])
    for row in items:
        if row['kind'] == 'Deployment':
            print(kube('-n', ns, 'rollout', 'status', 'deployment/' + row['metadata']['name'], '--timeout=300s').strip())
    deployment.record('kubernetes', mode, ns, 'running')


def birth(pid):
    try:
        fields = Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
        return None if fields[0] == 'Z' else fields[19]
    except (OSError, IndexError):
        return None


def stop_forwards(mode, group=None):
    directory = PRIVATE / mode
    paths = [directory / ('forwards-' + group + '.json')] if group else list(directory.glob('forwards*.json'))
    for path in paths:
        if not path.exists():
            continue
        for item in json.loads(path.read_text()):
            if item['birth'] and birth(item['pid']) == item['birth']:
                (os.killpg if item.get('process_group') else os.kill)(item['pid'], signal.SIGTERM)
                for _ in range(50):
                    if birth(item['pid']) != item['birth']:
                        break
                    time.sleep(.05)
        path.unlink()


def forward(mode, mappings, group='product', via_service=False):
    if via_service and not owned_namespace(mode):
        raise ValueError('当前命名空间未部署')
    stop_forwards(mode, group)
    directory = PRIVATE / mode
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    processes = []
    try:
        for service, local, remote in mappings:
            with socket.socket() as probe:
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                probe.bind(('127.0.0.1', local))
            with (directory / f'forward-{service}-{local}.log').open('a') as log:
                command = ['kubectl', '-n', namespace(mode), 'port-forward', '--address=127.0.0.1',
                           'service/' + service, f'{local}:{remote}']
                if via_service:
                    address = str(ipaddress.IPv4Address(kube('-n', namespace(mode), 'get', 'service/' + service, '-o', 'jsonpath={.spec.clusterIP}').strip()))
                    command = ['socat', f'TCP4-LISTEN:{local},bind=127.0.0.1,reuseaddr,fork,nodelay,backlog=128', f'TCP4:{address}:{remote},nodelay']
                child = subprocess.Popen(command, stdout=log, stderr=log, start_new_session=True)
            processes.append({'pid': child.pid, 'birth': birth(child.pid), 'port': local, 'process_group': via_service})
            for _ in range(100):
                if child.poll() is not None:
                    raise ValueError('端口转发失败：' + service)
                try:
                    with socket.create_connection(('127.0.0.1', local), timeout=.2):
                        break
                except OSError:
                    time.sleep(.1)
            else:
                raise ValueError('端口转发超时：' + service)
        (directory / ('forwards-' + group + '.json')).write_text(json.dumps(processes) + '\n')
    except BaseException:
        for item in processes:
            if item['birth'] and birth(item['pid']) == item['birth']:
                (os.killpg if item.get('process_group') else os.kill)(item['pid'], signal.SIGTERM)
        raise


def access(mode):
    port = 18081 if mode == 'observability' else 18080
    service = 'admin-frontend' if mode == 'observability' else 'frontend'
    forward(mode, [(service, port, 8080)])
    print(f'访问地址：http://127.0.0.1:{port}/')




def down(mode):
    if not owned_namespace(mode):
        stop_forwards(mode)
        print('当前模式尚未部署。')
        return
    stop_forwards(mode)
    ns = namespace(mode)
    kube('-n', ns, 'scale', 'deployment,statefulset', '-l', 'app.kubernetes.io/part-of=gopulse', '--replicas=0')
    kube('-n', ns, 'delete', 'daemonset', '-l', 'app.kubernetes.io/part-of=gopulse', '--wait=true')
    kube('-n', ns, 'delete', 'jobs', '-l', 'app.kubernetes.io/part-of=gopulse', '--wait=true')
    kube('-n', ns, 'wait', 'pod', '-l', 'app.kubernetes.io/part-of=gopulse', '--for=delete', '--timeout=120s')
    deployment.record('kubernetes', mode, ns, 'stopped')
    print('部署已停止，PVC 和凭据已保留。')


def name_archive(archive, images):
    """为按 digest 导出的匿名 OCI 描述符补全名称，保留原始摘要。"""
    pinned = {}
    for name in images:
        if '@sha256:' not in name:
            continue
        repository, digest = name.split('@', 1)
        if ':' in repository.rsplit('/', 1)[-1]:
            repository = repository.rsplit(':', 1)[0]
        registry = repository.split('/')[0]
        if not ('.' in registry or ':' in registry or registry == 'localhost'):
            repository = 'docker.io/' + repository
        pinned[digest] = repository + '@' + digest
    if not pinned:
        return
    temporary = archive.with_suffix('.named.tar')
    try:
        with tarfile.open(archive) as source, tarfile.open(temporary, 'w') as target:
            for member in source:
                data = source.extractfile(member) if member.isfile() else None
                if member.name == 'index.json':
                    index = json.load(data)
                    for descriptor in index['manifests']:
                        if name := pinned.get(descriptor['digest']):
                            descriptor.setdefault('annotations', {}).update({'io.containerd.image.name': name, 'org.opencontainers.image.ref.name': name})
                    raw = json.dumps(index).encode()
                    member.size = len(raw)
                    data = io.BytesIO(raw)
                target.addfile(member, data)
        temporary.replace(archive)
        archive.chmod(0o600)
    finally:
        temporary.unlink(missing_ok=True)


def load_images(mode):
    images = sorted({container['image'] for row in render(mode)
                     for key in ('containers', 'initContainers')
                     for container in row.get('spec', {}).get('template', {}).get('spec', {}).get(key, [])})
    for source in [ROOT / 'deploy/selfmonitor/resources.json'] + ([BASE / 'tracing/resources.json'] if os.environ.get('TRACE') == '1' else []):
        images += [c['image'] for row in json.loads(source.read_text())['items']
                   for c in row.get('spec', {}).get('template', {}).get('spec', {}).get('containers', [])]
    images = sorted(set(images + ['busybox:1.37.0']))
    PRIVATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    archive = PRIVATE / 'images.tar'
    for name in images:
        try:
            run(['docker', 'image', 'inspect', name])
        except RuntimeError:
            if name.startswith('gopulse/'):
                raise ValueError('缺少产品镜像，请执行 make build-images：' + name)
            print('拉取镜像：' + name)
            run(['docker', 'pull', name])
    run(['docker', 'save', '-o', str(archive), *images], timeout=1800)
    name_archive(archive, images)
    print(run(['sudo', '-n', 'ctr', '--address', '/run/gopulse-containerd/containerd.sock', '--namespace', 'k8s.io',
               'images', 'import', str(archive)], timeout=1800))
    archive.unlink()


def status(mode):
    print(kube('-n', namespace(mode), 'get', 'pods,svc,pvc,jobs', '-o', 'wide'))


def verify(mode):
    if not owned_namespace(mode):
        raise ValueError('当前模式未部署')
    ns = namespace(mode)
    expected = {row['metadata']['name']: row['spec'].get('replicas', 1) for row in render(mode)
                if row['kind'] in ('Deployment', 'StatefulSet')}
    workloads = json.loads(kube('-n', ns, 'get', 'deployment,statefulset', '-o', 'json'))['items']
    ready = {row['metadata']['name']: row for row in workloads}
    for name, count in expected.items():
        row = ready.get(name)
        if not row or row['spec'].get('replicas', 1) != count or row.get('status', {}).get('readyReplicas', 0) < count:
            raise ValueError('产品工作负载未就绪：' + name)
    rows = json.loads(kube('-n', ns, 'get', 'pods', '-o', 'json'))['items']
    if not rows:
        raise ValueError('没有运行中的 Pod')
    for row in rows:
        if row['status']['phase'] == 'Succeeded':
            continue
        if row['status']['phase'] != 'Running' or not row['status'].get('containerStatuses') or not all(c.get('ready') for c in row['status'].get('containerStatuses', [])):
            raise ValueError('Pod 未就绪：' + row['metadata']['name'])
    access(mode)
    port = 18081 if mode == 'observability' else 18080
    with urllib.request.urlopen(f'http://127.0.0.1:{port}/frontend-health', timeout=5) as response:
        assert response.status == 204
    print('Pod 和前端端口转发检查通过。')
    from acceptance import accept
    accept(mode, reuse_access=True)


def lock():
    return deployment.lock(ROOT)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'status', 'access', 'verify', 'load-images', 'render'])
    parser.add_argument('--mode', choices=MODES, default='all')
    args = parser.parse_args()
    with lock():
        if args.action == 'render':
            print(json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': render(args.mode)}, indent=2))
        else:
            {'up': up, 'down': down, 'status': status, 'access': access, 'verify': verify, 'load-images': load_images}[args.action](args.mode)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
