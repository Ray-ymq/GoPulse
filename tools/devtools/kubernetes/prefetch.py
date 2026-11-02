"""校验并导入固定控制平面镜像；可选择直连下载。"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[3]
CACHE = ROOT / '.run/kubernetes/cluster/image-cache'


def download(url, direct):
    args = ['curl', '--ipv4', '--fail', '--silent', '--show-error', '--location', '--retry', '2', '--connect-timeout', '15', '--max-time', '600']
    if direct:
        args += ['--noproxy', '*']
    headers = ['-H', 'Accept: application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json'] if '/manifests/' in url else []
    return subprocess.check_output([*args, *headers, url])


def prefetch(direct=False):
    CACHE.mkdir(parents=True, exist_ok=True, mode=0o700)
    images = subprocess.check_output(['kubeadm', 'config', 'images', 'list', '--kubernetes-version', 'v1.36.5'], text=True).split()
    ready = set(subprocess.check_output(['sudo', '-n', 'ctr', '--address', '/run/gopulse-containerd/containerd.sock', '--namespace', 'k8s.io', 'images', 'check', '--quiet'], text=True).split())
    for reference in images:
        if reference in ready:
            print('镜像已完整导入：' + reference, flush=True)
            continue
        repository, tag = reference.removeprefix('registry.k8s.io/').split(':')
        base = 'https://registry.k8s.io/v2/' + repository
        cached = CACHE / (repository.replace('/', '-') + '-' + tag + '.manifest')
        if cached.exists():
            manifest = cached.read_bytes()
        else:
            manifest = download(base + '/manifests/' + tag, direct)
            cached.write_bytes(manifest)
        document = json.loads(manifest)
        if 'manifests' in document:
            descriptor = next(d for d in document['manifests'] if d['platform']['architecture'] == 'amd64' and d['platform']['os'] == 'linux')
            manifest = download(base + '/manifests/' + descriptor['digest'], direct)
            document = json.loads(manifest)
        def blob(descriptor):
            digest = descriptor['digest']
            if not re.fullmatch(r'sha256:[0-9a-f]{64}', digest):
                raise ValueError('镜像摘要格式无效')
            path = CACHE / digest[7:]
            if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != digest[7:]:
                data = download(base + '/blobs/' + digest, direct)
                if len(data) != descriptor['size'] or hashlib.sha256(data).hexdigest() != digest[7:]:
                    raise ValueError('镜像数据校验失败')
                path.write_bytes(data)
            return path
        with ThreadPoolExecutor(max_workers=4) as pool:
            paths = list(pool.map(blob, [document['config'], *document['layers']]))
        digest = hashlib.sha256(manifest).hexdigest()
        index = {'schemaVersion': 2, 'manifests': [{'mediaType': document['mediaType'], 'digest': 'sha256:' + digest,
                 'size': len(manifest), 'annotations': {'io.containerd.image.name': reference, 'org.opencontainers.image.ref.name': reference}}]}
        archive = CACHE / 'control-plane.tar'
        with tarfile.open(archive, 'w') as tar:
            for name, data in [('oci-layout', b'{"imageLayoutVersion":"1.0.0"}'), ('index.json', json.dumps(index).encode()), ('blobs/sha256/' + digest, manifest)]:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
            for path in paths:
                tar.add(path, arcname='blobs/sha256/' + path.name)
        subprocess.run(['sudo', '-n', 'ctr', '--address', '/run/gopulse-containerd/containerd.sock', '--namespace', 'k8s.io',
                        'images', 'import', '--platform', 'linux/amd64', str(archive)], check=True, stdout=subprocess.DEVNULL)
        archive.unlink()
        print('已校验并导入：' + reference, flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--direct', action='store_true')
    prefetch(parser.parse_args().direct)
