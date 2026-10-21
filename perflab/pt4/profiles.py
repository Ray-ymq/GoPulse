"""构建测试专用 pprof 入口，按需采集 Go 剖析数据。"""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import urllib.request
from common import ROOT, STATE, atomic_json, sha256, utc

PACKAGES = {'server': 'server', 'worker': 'business-worker', 'indexer': 'search-indexer'}
PORTS = {'server': 19201, 'worker': 19202, 'indexer': 19203}
ENVIRONMENT_KEYS = ('GOPULSE_PERFLAB_PROFILE_ROLE', 'PERFLAB_PPROF_ADDR', 'PERFLAB_BLOCK_RATE', 'PERFLAB_MUTEX_FRACTION')


def settings(role, block_rate=0, mutex_fraction=0):
    if role is not None and role not in PACKAGES:
        raise ValueError('未知的 pprof 服务角色')
    if any(value < 0 or value > 2**63 - 1 for value in (block_rate, mutex_fraction)):
        raise ValueError('pprof 采样参数必须为非负的有符号 64 位整数')
    if role is None and (block_rate or mutex_fraction):
        raise ValueError('设置采样参数前必须指定 --profile-role')
    return {'profile_role': role, 'profile_sampling': {'block_rate': block_rate, 'mutex_fraction': mutex_fraction}}


def configure(role, block_rate=0, mutex_fraction=0):
    settings(role, block_rate, mutex_fraction)
    for key in ENVIRONMENT_KEYS:
        os.environ.pop(key, None)
    if role:
        os.environ.update(GOPULSE_PERFLAB_PROFILE_ROLE=role, PERFLAB_PPROF_ADDR=f'127.0.0.1:{PORTS[role]}',
                          PERFLAB_BLOCK_RATE=str(block_rate), PERFLAB_MUTEX_FRACTION=str(mutex_fraction))


def build(role):
    directory = STATE / 'profile-build' / role
    directory.mkdir(parents=True, exist_ok=True)
    (STATE / 'bin').mkdir(parents=True, exist_ok=True)
    source = directory / 'profile.go'
    shutil.copyfile(ROOT / 'perflab/pt4/profile-overlay.go.tmpl', source)
    destination = ROOT / 'backend/cmd' / PACKAGES[role] / 'pt4_profile_overlay.go'
    overlay = directory / 'overlay.json'
    atomic_json(overlay, {'Replace': {str(destination): str(source)}})
    target = STATE / 'bin' / (role + '-profile')
    subprocess.run(['go', '-C', 'backend', 'build', '-overlay', str(overlay), '-o', str(target), './cmd/' + PACKAGES[role]],
                   cwd=ROOT, check=True)
    return {'binary': str(target), 'sha256': sha256(target)}


def capture_go(directory, role, seconds):
    if role not in PACKAGES or not 1 <= seconds <= 60:
        raise ValueError('pprof 角色或采样时长无效')
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    binary = STATE / 'bin' / (role + '-profile')
    binary_hash = sha256(binary)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    started = utc()
    profiles = {}
    for name, query in [('profile', '?seconds=' + str(seconds)), ('heap', ''), ('goroutine', ''),
                        ('block', '?seconds=1'), ('mutex', '?seconds=1')]:
        with opener.open(f'http://127.0.0.1:{PORTS[role]}/debug/pprof/{name}' + query, timeout=seconds + 10) as response:
            body = response.read()
        if not body.startswith(b'\x1f\x8b'):
            raise ValueError('pprof 未返回压缩剖析数据')
        target = directory / (role + '-' + name + '.pb.gz')
        target.write_bytes(body)
        target.chmod(0o600)
        profiles[name] = {'file': target.name, 'sha256': sha256(target)}
    with (directory / (role + '-cpu-top.txt')).open('w') as output:
        subprocess.run(['go', 'tool', 'pprof', '-top', str(binary), str(directory / (role + '-profile.pb.gz'))],
                       stdout=output, check=True)
    value = {'role': role, 'cpu_seconds': seconds, 'started_at': started, 'finished_at': utc(),
             'binary': str(binary), 'binary_sha256': binary_hash, 'profiles': profiles,
             'block_mutex': '只有显式设置采样参数才有采样数据；空 profile 不证明没有等待'}
    atomic_json(directory / (role + '-profile-manifest.json'), value)
    return value


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--role', choices=PACKAGES, required=True)
    parser.add_argument('--seconds', type=int, choices=range(1, 61), default=30)
    parser.add_argument('--directory', type=Path, required=True)
    args = parser.parse_args()
    capture_go(args.directory, args.role, args.seconds)
    print('pprof 已保存到 ' + str(args.directory))
