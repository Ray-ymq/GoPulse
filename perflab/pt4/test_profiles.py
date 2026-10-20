"""在临时 Go 进程中验证诊断 overlay，不启动业务服务。"""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.request
from common import ROOT


class ProfileOverlayTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.directory = Path(cls.temp.name)
        (cls.directory / 'go.mod').write_text('module pt4profilefixture\ngo 1.26\n')
        (cls.directory / 'main.go').write_text('package main\nimport "time"\nfunc main(){time.Sleep(10*time.Second)}\n')
        (cls.directory / 'overlay.json').write_text(json.dumps({'Replace': {str(cls.directory / 'pt4_overlay.go'): str(ROOT / 'perflab/pt4/profile-overlay.go.tmpl')}}))
        cls.binary = cls.directory / 'probe'
        subprocess.run(['go', 'build', '-overlay', str(cls.directory / 'overlay.json'), '-o', str(cls.binary), '.'],
                       cwd=cls.directory, env=dict(os.environ, GOWORK='off'), check=True, capture_output=True, timeout=60)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_non_loopback_is_rejected(self):
        result = subprocess.run([str(self.binary)], env=dict(os.environ, PERFLAB_PPROF_ADDR='0.0.0.0:0'), capture_output=True, timeout=3)
        self.assertNotEqual(result.returncode, 0)

    def test_goroutine_and_cpu_profiles(self):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        process = subprocess.Popen([str(self.binary)], env=dict(os.environ, PERFLAB_PPROF_ADDR=f'127.0.0.1:{port}'), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        try:
            for _ in range(100):
                try:
                    with opener.open(f'http://127.0.0.1:{port}/debug/pprof/goroutine', timeout=.2) as response:
                        self.assertEqual(response.read(2), b'\x1f\x8b')
                    break
                except OSError:
                    time.sleep(.02)
            else:
                self.fail('诊断入口未启动')
            with opener.open(f'http://127.0.0.1:{port}/debug/pprof/profile?seconds=1', timeout=3) as response:
                self.assertEqual(response.read(2), b'\x1f\x8b')
        finally:
            process.terminate(); process.wait(timeout=3)
