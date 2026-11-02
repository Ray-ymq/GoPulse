"""为产品部署、源码会话、集成检查和压测提供工作区级互斥。"""
from contextlib import contextmanager
from datetime import datetime, timezone
import argparse
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
MODES = ('business', 'observability', 'all')


def read(path):
    return json.loads(path.read_text())


def capture(args, env=None):
    result = subprocess.run(args, text=True, capture_output=True, env=env, timeout=20)
    if result.returncode:
        raise ValueError(f'无法检查 {args[0]} 资源状态，退出码 {result.returncode}；停止启动操作')
    return result.stdout


@contextmanager
def lock(root=ROOT):
    directory = root / '.run/deploy'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (directory / 'operation.lock').open('a') as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise ValueError('已有部署、源码、集成或压测操作占用工作区锁，请等待该操作结束') from error
        try:
            yield
        finally:
            fcntl.flock(stream, fcntl.LOCK_UN)


def record(runtime, mode, owner, status, root=ROOT):
    directory = root / '.run/deploy'
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    value = {'schema': 1, 'workspace': str(root), 'runtime': runtime, 'mode': mode,
             'owner': owner, 'status': status, 'updated_at': datetime.now(timezone.utc).isoformat()}
    path = directory / 'state.json'
    temporary = path.with_suffix('.tmp')
    with temporary.open('w') as stream:
        os.chmod(temporary, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
    temporary.replace(path)


def process_alive(row):
    pid = row.get('pid')
    if not pid:
        return False
    if sys.platform == 'darwin':
        # Darwin 没有 /proc；此处只阻止启动，不据此终止进程。
        result = subprocess.run(['ps', '-p', str(int(pid)), '-o', 'stat='], text=True, capture_output=True, timeout=5)
        return result.returncode == 0 and bool(result.stdout.strip()) and not result.stdout.strip().startswith('Z')
    try:
        fields = Path(f'/proc/{int(pid)}/stat').read_text().rsplit(')', 1)[1].split()
        return fields[0] != 'Z' and fields[19] == str(row.get('birth', row.get('start_ticks')))
    except (OSError, ValueError, IndexError):
        return False


def docker_resources(root):
    if not shutil.which('docker'):
        return []
    ids = capture(['docker', 'ps', '-q']).split()
    if not ids:
        return []
    rows = json.loads(capture(['docker', 'inspect', *ids]))
    owned = []
    for row in rows:
        labels = row.get('Config', {}).get('Labels') or {}
        working = labels.get('com.docker.compose.project.working_dir', '')
        files = labels.get('com.docker.compose.project.config_files', '')
        project = labels.get('com.docker.compose.project', '')
        if project == 'gopulse-selfmonitor':
            networks = row.get('NetworkSettings', {}).get('Networks', {})
            ids = [net['NetworkID'] for net in networks.values() if net.get('NetworkID')]
            attached = json.loads(capture(['docker', 'network', 'inspect', *ids])) if ids else []
            if any(net.get('Labels', {}).get('io.gopulse.workspace') == str(root) for net in attached):
                owned.append({'runtime': 'compose', 'mode': 'all', 'owner': 'gopulse', 'purpose': 'product', 'stop': 'make selfmonitor-down'})
                continue
        if not working or not Path(working).resolve().is_relative_to(root):
            continue
        mode = 'business' if 'compose.business.yaml' in files else 'observability' if 'compose.observability.yaml' in files else 'all'
        purpose = 'pt' if project == 'gopulse-pt-business' else 'source' if 'deploy/dev/' in files else 'product'
        owned.append({'runtime': 'compose', 'mode': mode, 'owner': project, 'purpose': purpose})
    return owned


def kubernetes_resources(root):
    config = root / '.run/kubernetes/cluster/admin.conf'
    if not config.exists():
        return []
    if not shutil.which('kubectl'):
        raise ValueError('已有 Kubernetes 配置但缺少 kubectl，无法检查部署互斥')
    env = dict(os.environ, KUBECONFIG=str(config))
    namespaces = json.loads(capture(['kubectl', 'get', 'namespaces', '-o', 'json', '--request-timeout=10s'], env))['items']
    result = []
    for ns in namespaces:
        if ns['metadata'].get('annotations', {}).get('gopulse.io/workspace') != str(root):
            continue
        name = ns['metadata']['name']
        if name not in ['gopulse-' + mode for mode in MODES]:
            continue
        rows = json.loads(capture(['kubectl', '-n', name, 'get', 'deployment,statefulset,daemonset,pods', '-o', 'json', '--request-timeout=10s'], env))['items']
        running = False
        for row in rows:
            if row['kind'] in ('Deployment', 'StatefulSet') and row['spec'].get('replicas', 1) > 0:
                running = True
            if row['kind'] == 'DaemonSet' and row['status'].get('currentNumberScheduled', 0) > 0:
                running = True
            if row['kind'] == 'Pod' and row.get('status', {}).get('phase') not in ('Succeeded', 'Failed'):
                running = True
        if running:
            result.append({'runtime': 'kubernetes', 'mode': name.removeprefix('gopulse-'), 'owner': name, 'purpose': 'product'})
    return result


def check(runtime, mode='all', purpose='product', owner=None, root=ROOT):
    root = root.resolve()
    if runtime not in ('compose', 'kubernetes') or mode not in MODES:
        raise ValueError('部署方式或 MODE 无效')
    for row in docker_resources(root) + kubernetes_resources(root):
        if runtime == row['runtime'] and purpose == row['purpose']:
            if purpose in ('source', 'pt') or (mode == row['mode'] and owner == row['owner']):
                continue
        stop = row.get('stop') or ('make k8s-down MODE=' + row['mode'] if row['runtime'] == 'kubernetes' else 'make stop' if row['purpose'] == 'source' else 'make pt-stop，并停止专用压测 Compose 项目' if row['purpose'] == 'pt' else 'make stack-down MODE=' + row['mode'] + ' COMPOSE_PROJECT_NAME=' + row['owner'])
        raise ValueError(f'工作区仍有 {row["runtime"]} {row["mode"]} 环境（{row["owner"]}）；请先执行 {stop}')
    for path in (root / '.run/local').glob('*/*state.json'):
        value = read(path)
        if value.get('workspace_root') != str(root):
            continue
        if any(process_alive(row) for row in value.get('processes', {}).values()):
            if purpose == 'source' and path.name == 'state.json':
                continue
            raise ValueError('工作区仍有源码或集成进程，请先执行 make stop 或等待集成检查结束')
    active = root / '.run/perflab/active.json'
    if active.exists() and purpose != 'pt':
        run_id = read(active)['run_id']
        if Path(run_id).name != run_id or run_id in ('.', '..'):
            raise ValueError('压测运行编号无效')
        if read(active.parent / run_id / 'state.json').get('status') != 'completed':
            raise ValueError('压测记录未完成，请先执行 make pt-stop')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['check', 'record'])
    parser.add_argument('--runtime', choices=['compose', 'kubernetes'], required=True)
    parser.add_argument('--mode', choices=MODES, default='all')
    parser.add_argument('--purpose', choices=['product', 'source', 'integration', 'pt'], default='product')
    parser.add_argument('--owner', default='')
    parser.add_argument('--status', default='running')
    args = parser.parse_args()
    if args.action == 'check':
        check(args.runtime, args.mode, args.purpose, args.owner)
    else:
        record(args.runtime, args.mode, args.owner, args.status)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
