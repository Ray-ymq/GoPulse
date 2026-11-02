"""检查双部署运行契约、配置键、进程探针和镜像定义。"""
import json
from pathlib import Path
import subprocess
import sys

import jsonschema

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/devtools/kubernetes'))
import k8s


def validate():
    contract = json.loads((ROOT / 'deploy/runtime-contracts.json').read_text())
    jsonschema.validate(contract, json.loads((ROOT / 'deploy/runtime-contracts.schema.json').read_text()))
    rendered = {mode: k8s.render(mode) for mode in k8s.MODES}
    rows = rendered['all']
    configs = {row['metadata']['name']: row.get('data', {}) for row in rows if row['kind'] == 'ConfigMap'}
    workloads = {row['metadata']['name']: row for row in rows if row['kind'] in ('Deployment', 'StatefulSet')}
    command = ['docker', 'compose', '--env-file', str(ROOT / '.env.example'), '-f', str(ROOT / 'deploy/compose/compose.yaml'), 'config', '--format', 'json']
    compose = json.loads(subprocess.check_output(command, cwd=ROOT, text=True))['services']
    for component in contract['components']:
        for name in component['compose_services']:
            assert name in compose, 'Compose 服务缺失：' + name
        service = component['compose_service']
        if service:
            values = compose[service].get('environment', {})
            for env in component['environment']:
                if env['compose_value'] is not None:
                    assert env['key'] in values, f'Compose 配置键缺失：{service}/{env["key"]}'
        name = component['kubernetes_workload']
        if not name:
            continue
        row = workloads[name]
        assert row['spec'].get('replicas', 1) == component['kubernetes_replica']['configured'], 'Kubernetes 副本数不符：' + name
        container = row['spec']['template']['spec']['containers'][0]
        values = {}
        for ref in container.get('envFrom', []):
            if 'configMapRef' in ref:
                values.update(configs[ref['configMapRef']['name']])
        values.update({env['name']: env.get('value', env.get('valueFrom')) for env in container.get('env', [])})
        for env in component['environment']:
            expected = env['kubernetes_value']
            if expected is not None:
                assert values.get(env['key']) == expected, f'Kubernetes 配置绑定不符：{name}/{env["key"]}'
        for key, probe in [('startup', 'startupProbe'), ('live', 'livenessProbe'), ('ready', 'readinessProbe')]:
            assert container[probe]['httpGet']['path'] == component['probes'][key], 'Kubernetes 探针不符：' + name
    images = json.loads((ROOT / 'deploy/images.json').read_text())['target']
    for name, definition in images.items():
        assert (ROOT / definition['dockerfile']).is_file(), 'Dockerfile 不存在：' + name
    for mode, items in rendered.items():
        for row in items:
            for container in row.get('spec', {}).get('template', {}).get('spec', {}).get('containers', []):
                image = container['image']
                if image.startswith('gopulse/'):
                    assert image.split(':')[0].split('/')[1] in images, 'Kubernetes 镜像没有共享构建定义'
    print('三种 Kubernetes 模式、Compose 绑定、双部署运行契约和共享镜像定义检查通过。')


if __name__ == '__main__':
    try:
        validate()
    except (ValueError, AssertionError, OSError, jsonschema.ValidationError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
