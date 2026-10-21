"""用临时 Go 进程验证 pprof 开关、回环限制和剖析文件，不启动业务服务。"""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.request
from unittest.mock import patch
from common import ROOT
import environment
import profiles


class ProfileSettingsTest(unittest.TestCase):
    def test_default_clears_inherited_options_and_uses_normal_binaries(self):
        inherited = dict(zip(profiles.ENVIRONMENT_KEYS, ('worker', '127.0.0.1:19202', '1', '1')))
        with patch.dict(os.environ, inherited), patch.object(environment, 'STATE', ROOT / '.run/pt4'):
            profiles.configure(None)
            for key in profiles.ENVIRONMENT_KEYS:
                self.assertNotIn(key, os.environ)
            for role in profiles.PACKAGES:
                self.assertEqual(environment.service_binary(role).name, role)

    def test_only_selected_role_uses_profile_binary(self):
        with patch.dict(os.environ), patch.object(environment, 'STATE', ROOT / '.run/pt4'):
            profiles.configure('worker', 1000000, 10)
            self.assertEqual(os.environ['PERFLAB_PPROF_ADDR'], '127.0.0.1:19202')
            self.assertEqual(os.environ['PERFLAB_BLOCK_RATE'], '1000000')
            self.assertEqual(os.environ['PERFLAB_MUTEX_FRACTION'], '10')
            self.assertEqual(environment.service_binary('worker').name, 'worker-profile')
            self.assertEqual(environment.service_binary('server').name, 'server')
            self.assertEqual(environment.service_binary('indexer').name, 'indexer')
        for role, block, mutex in [(None, 1, 0), ('server', -1, 0), ('worker', 0, 2**63)]:
            with self.subTest(role=role, block=block, mutex=mutex), self.assertRaises(ValueError):
                profiles.settings(role, block, mutex)


class ProfileOverlayTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.directory = Path(cls.temp.name)
        (cls.directory / 'go.mod').write_text('module pt4profilefixture\ngo 1.26\n')
        (cls.directory / 'main.go').write_text('''package main
import ("fmt"; "runtime"; "time")
func main() {
    fmt.Println("ready")
    go func(){for {runtime.Gosched()}}()
    time.Sleep(30*time.Second)
}
''')
        (cls.directory / 'overlay.json').write_text(json.dumps({'Replace': {
            str(cls.directory / 'pt4_overlay.go'): str(ROOT / 'perflab/pt4/profile-overlay.go.tmpl')}}))
        (cls.directory / 'bin').mkdir()
        cls.binary = cls.directory / 'bin/server-profile'
        subprocess.run(['go', 'build', '-overlay', str(cls.directory / 'overlay.json'), '-o', str(cls.binary), '.'],
                       cwd=cls.directory, env=dict(os.environ, GOWORK='off'), check=True, capture_output=True, timeout=60)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_without_address_does_not_enable_profiling(self):
        env = {key: value for key, value in os.environ.items() if key not in profiles.ENVIRONMENT_KEYS}
        env['PERFLAB_BLOCK_RATE'] = 'invalid'
        process = subprocess.Popen([str(self.binary)], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            self.assertEqual(process.stdout.readline(), b'ready\n')
            self.assertIsNone(process.poll())
        finally:
            process.terminate()
            process.communicate(timeout=3)

    def test_non_loopback_and_invalid_sampling_are_rejected(self):
        for address, rate in [('0.0.0.0:0', '0'), ('127.0.0.1:0', '-1')]:
            with self.subTest(address=address, rate=rate):
                result = subprocess.run([str(self.binary)], env=dict(os.environ, PERFLAB_PPROF_ADDR=address,
                                        PERFLAB_BLOCK_RATE=rate), capture_output=True, timeout=3)
                self.assertNotEqual(result.returncode, 0)

    def test_capture_produces_five_readable_profiles(self):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        process = subprocess.Popen([str(self.binary)], env=dict(os.environ, PERFLAB_PPROF_ADDR=f'127.0.0.1:{port}',
                                   PERFLAB_BLOCK_RATE='1000000', PERFLAB_MUTEX_FRACTION='1'),
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
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
                self.fail('pprof 入口未启动')
            with patch.object(profiles, 'STATE', self.directory), patch.dict(profiles.PORTS, server=port):
                value = profiles.capture_go(self.directory / 'profiles', 'server', 1)
            self.assertEqual(set(value['profiles']), {'profile', 'heap', 'goroutine', 'block', 'mutex'})
            for item in value['profiles'].values():
                path = self.directory / 'profiles' / item['file']
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                subprocess.run(['go', 'tool', 'pprof', '-top', str(self.binary), str(path)],
                               check=True, capture_output=True, timeout=10)
            self.assertTrue((self.directory / 'profiles/server-profile-manifest.json').exists())
        finally:
            process.terminate()
            process.wait(timeout=3)


if __name__ == '__main__':
    unittest.main()
