"""管理独立指标源；只处理进程、协议和容器发现结果，不读取 cgroup。"""
import ipaddress
import json
import math
import os
from pathlib import Path
import selectors
import subprocess
import time
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise ValueError('指标源不得重定向')


class LocalSource:
    def __init__(self, binary, directory, name, schema, args=()):
        self.binary, self.directory = Path(binary), Path(directory)
        self.name, self.schema, self.args = name, schema, args
        self.process = self.log = None
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def start(self):
        try:
            self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
            self.log = (self.directory / (self.name + '.log')).open('x')
            os.chmod(self.log.name, 0o600)
            self.process = subprocess.Popen([str(self.binary), *map(str, self.args)],
                                            stdout=subprocess.PIPE, stderr=self.log)
            # 每次只读一个字节，避免部分启动输出阻塞 readline。
            payload = bytearray()
            deadline = time.monotonic() + 5
            with selectors.DefaultSelector() as selector:
                selector.register(self.process.stdout, selectors.EVENT_READ)
                while not payload.endswith(b'\n'):
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not selector.select(remaining):
                        raise RuntimeError('指标源启动超时')
                    byte = os.read(self.process.stdout.fileno(), 1)
                    if not byte or len(payload) >= 4096:
                        raise RuntimeError('指标源启动响应无效')
                    payload.extend(byte)
            ready = json.loads(payload)
            url = urllib.parse.urlsplit(ready['url'])
            if (ready.get('schema_version') != self.schema or url.scheme != 'http'
                    or not ipaddress.ip_address(url.hostname).is_loopback or not url.port
                    or url.username is not None or url.path or url.query or url.fragment):
                raise ValueError('指标源协议或地址无效')
            self.url = ready['url']
            return self
        except BaseException:
            self.close()
            raise

    def fetch(self, path):
        if self.process is None or self.process.poll() is not None:
            raise RuntimeError('指标源已退出')
        with self.opener.open(self.url + path, timeout=3) as response:
            # 启动地址受限；重定向后的地址也必须相同。
            if response.url != self.url + path:
                raise ValueError('指标源发生重定向')
            body = response.read(4 * 1024**2 + 1)
        if len(body) > 4 * 1024**2:
            raise ValueError('指标源响应超过上限')
        return body

    def close(self):
        if self.process:
            if self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)
            self.process.stdout.close()
        if self.log:
            self.log.close()


class ContainerSource(LocalSource):
    def __init__(self, binary, directory, extra_args=()):
        self.targets_file = Path(directory) / 'container-targets.json'
        super().__init__(binary, directory, 'container-exporter', 'gopulse.containerstats.v1',
                         ('--targets-file', self.targets_file, *extra_args))

    def snapshot(self, targets):
        temporary = self.targets_file.with_suffix('.tmp')
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(descriptor, 'w') as stream:
            json.dump(targets, stream)
        temporary.replace(self.targets_file)
        began = time.time()
        value = json.loads(self.fetch('/snapshot'))
        if (value.get('schema_version') != self.schema or not math.isfinite(value['epoch'])
                or not began - 1 <= value['epoch'] <= time.time() + 1):
            raise ValueError('容器指标协议或时间无效')
        expected = {target['name']: target['id'] for target in targets}
        if {name: item['id'] for name, item in value['containers'].items()} != expected:
            raise ValueError('容器指标身份不符')
        return value


def build_container_source(root, destination):
    destination = Path(destination)
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    subprocess.run(['go', '-C', str(Path(root) / 'runtimekit'), 'build', '-o', str(destination),
                    './cmd/container-exporter'], env={**os.environ, 'CGO_ENABLED': '0'}, check=True, timeout=600)
