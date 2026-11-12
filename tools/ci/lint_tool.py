#!/usr/bin/env python3
"""安装固定版本的 golangci-lint，并校验下载文件的 SHA256。"""

from __future__ import annotations

import hashlib
import json
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tarfile
import tempfile
import urllib.request


ROOT = Path(__file__).resolve().parents[2]


def matches_version(binary: Path, version: str) -> bool:
    if not binary.is_file():
        return False
    result = subprocess.run([str(binary), '--version'], capture_output=True, text=True)
    return result.returncode == 0 and version in result.stdout.split()


def ensure_lint(root: Path) -> Path:
    config = json.loads((root / 'tools/ci/toolchains.json').read_text())['golangci_lint']
    platform = subprocess.check_output(['go', 'env', 'GOHOSTOS', 'GOHOSTARCH'], text=True).split()
    spec = config['archives']['/'.join(platform)]
    directory = root / '.run/tools/golangci-lint' / config['version'] / '-'.join(platform)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    archive = directory / spec['name']
    if not archive.exists():
        temporary = None
        try:
            with urllib.request.urlopen(config['release'] + '/' + spec['name'], timeout=120) as response:
                with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
                    temporary = Path(output.name)
                    while chunk := response.read(1024 * 1024):
                        output.write(chunk)
            if hashlib.sha256(temporary.read_bytes()).hexdigest() != spec['sha256'].lower():
                raise ValueError('golangci-lint 下载文件的 SHA256 与固定值不符')
            temporary.replace(archive)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != spec['sha256'].lower():
        raise ValueError(f'golangci-lint 缓存文件的 SHA256 与固定值不符：{archive}')
    binary = directory / 'golangci-lint'
    if matches_version(binary, config['version']):
        return binary
    with tarfile.open(archive, 'r:gz') as source:
        member = next((item for item in source if item.isfile()
                       and PurePosixPath(item.name).name == 'golangci-lint'
                       and '..' not in PurePosixPath(item.name).parts), None)
        if member is None:
            raise ValueError('golangci-lint 归档中缺少可执行文件')
        temporary = None
        try:
            with source.extractfile(member) as content:
                with tempfile.NamedTemporaryFile(dir=directory, delete=False) as output:
                    temporary = Path(output.name)
                    while chunk := content.read(1024 * 1024):
                        output.write(chunk)
            temporary.chmod(0o700)
            if not matches_version(temporary, config['version']):
                raise ValueError('golangci-lint 可执行文件的版本与固定版本不符')
            temporary.replace(binary)
        finally:
            if temporary is not None:
                temporary.unlink(missing_ok=True)
    return binary


def main() -> int:
    try:
        print(ensure_lint(ROOT))
        return 0
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.SubprocessError) as error:
        print(f'安装 golangci-lint 失败：{error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
