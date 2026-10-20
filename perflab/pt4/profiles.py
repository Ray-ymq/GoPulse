"""构建测试专用 Go 剖析入口，或采集 MySQL 软件 CPU 栈；不启动业务负载。"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import urllib.request
from common import ROOT, STATE, MYSQL, atomic_json, sha256, utc

PACKAGES = {'server': 'server', 'worker': 'business-worker', 'indexer': 'search-indexer'}


def build(role):
    directory = STATE / 'profile-build' / role
    directory.mkdir(parents=True, exist_ok=True)
    source = directory / 'profile.go'
    shutil.copyfile(ROOT / 'perflab/pt4/profile-overlay.go.tmpl', source)
    destination = ROOT / 'backend/cmd' / PACKAGES[role] / 'pt4_profile_overlay.go'
    overlay = directory / 'overlay.json'
    atomic_json(overlay, {'Replace': {str(destination): str(source)}})
    target = STATE / 'bin' / (role + '-profile')
    subprocess.run(['go', '-C', 'backend', 'build', '-overlay', str(overlay), '-o', str(target), './cmd/' + PACKAGES[role]], cwd=ROOT, check=True)
    return {'binary': str(target), 'sha256': sha256(target), 'overlay': str(overlay)}


def capture_mysql(directory, seconds, container=MYSQL):
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    item = json.loads(subprocess.check_output(['docker', 'inspect', container], text=True))[0]
    labels = item['Config']['Labels'] or {}
    if labels.get('com.docker.compose.project') != 'gopulse-loadtest' and labels.get('gopulse.pt4.fixture') != 'true':
        raise RuntimeError('拒绝采样未经核对的容器')
    pid = int(item['State']['Pid'])
    if Path(f'/proc/{pid}/comm').read_text().strip() != 'mysqld':
        raise RuntimeError('容器主进程不是 mysqld，拒绝采样')
    data = directory / 'mysql-perf.data'
    subprocess.run(['sudo', '-n', 'perf', 'record', '-e', 'cpu-clock', '-F', '49', '-g', '--call-graph', 'dwarf',
                    '-p', str(pid), '-o', str(data), '--', 'sleep', str(seconds)], check=True)
    with (directory / 'mysql-perf.txt').open('w') as out:
        subprocess.run(['sudo', '-n', 'perf', 'report', '--stdio', '--no-children', '-i', str(data)], stdout=out, check=True)
    with (directory / 'mysql-perf-stacks.txt').open('w') as out:
        subprocess.run(['sudo', '-n', 'perf', 'script', '-i', str(data)], stdout=out, check=True)
    subprocess.run(['sudo', '-n', 'chown', str(os.getuid()) + ':' + str(os.getgid()), str(data)], check=True)
    data.chmod(0o600)
    return {'pid': pid, 'seconds': seconds, 'data': str(data), 'sha256': sha256(data), 'event': 'cpu-clock', 'frequency_hz': 49}


def capture_go(directory, role, seconds):
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    port = {'server': 19201, 'worker': 19202, 'indexer': 19203}[role]
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for profile, query in [('profile', '?seconds=' + str(seconds)), ('goroutine', ''), ('block', '?seconds=1'), ('mutex', '?seconds=1')]:
        with opener.open(f'http://127.0.0.1:{port}/debug/pprof/{profile}' + query, timeout=seconds + 10) as response:
            target = directory / (role + '-' + profile + '.pb.gz')
            target.write_bytes(response.read()); target.chmod(0o600)
    with (directory / (role + '-cpu-top.txt')).open('w') as out:
        subprocess.run(['go', 'tool', 'pprof', '-top', str(STATE / 'bin' / (role + '-profile')), str(directory / (role + '-profile.pb.gz'))], stdout=out, check=True)
    return {'role': role, 'seconds': seconds, 'directory': str(directory),
            'block_mutex': '仅在该诊断组显式设置采样率时有采样数据；空 profile 不证明没有等待'}


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('build', 'mysql', 'go'))
    parser.add_argument('--role', choices=PACKAGES, default='server')
    parser.add_argument('--seconds', type=int, choices=range(1, 61), default=30)
    parser.add_argument('--directory', type=Path)
    args = parser.parse_args()
    if args.action != 'build' and args.directory is None:
        parser.error('采集时必须指定 --directory')
    value = build(args.role) if args.action == 'build' else capture_mysql(args.directory, args.seconds) if args.action == 'mysql' else capture_go(args.directory, args.role, args.seconds)
    if args.directory:
        atomic_json(args.directory / 'profile-manifest.json', {'timestamp': utc(), **value})
    print(json.dumps(value, ensure_ascii=False))
