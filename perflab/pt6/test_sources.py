"""使用临时 cgroup 文件和实际采集进程验证指标来源，不访问 Docker 或业务库。"""
import os
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from urllib.error import HTTPError
from unittest.mock import patch

from common import ROOT
import common
from diagnostics.budget import parse_budget
from diagnostics.sources import ContainerSource, LocalSource, ConfiguredExporter, build_container_source


def budget(stamp=1000, success=1, open_count=0):
    fields = {'last_success_timestamp_seconds': stamp, 'sample_success': success,
              'open_requirements': open_count, 'parked_messages': 0,
              'retained_groups': 7, 'retained_bytes': 2048}
    return ''.join(f'# TYPE gopulse_backend_async_budget_{key} gauge\n'
                   f'gopulse_backend_async_budget_{key} {value}\n' for key, value in fields.items())


class BudgetSourceTest(unittest.TestCase):
    def test_zero_is_valid_only_with_fresh_successful_sample(self):
        value = parse_budget(budget(), now=1005)
        self.assertEqual(value['open_requirements'], 0)
        self.assertEqual(value['scope'], 'all_epochs')
        self.assertEqual(value['retained_groups'], 7)
        for body in (budget(success=0), budget(stamp=980), budget(stamp=1010),
                     budget(open_count=-1), budget(open_count=.5), budget().replace('open_requirements', 'other')):
            with self.subTest(body=body), self.assertRaises(ValueError):
                parse_budget(body, now=1005)
        with self.assertRaises(ValueError):
            parse_budget(budget(), now=1005, not_before=1001)

    def test_drain_waits_for_new_sample_and_uses_no_budget_sql(self):
        with patch.object(common.time, 'time', return_value=1000), \
             patch.object(common.time, 'sleep'), \
             patch.object(common, 'metrics', side_effect=[budget(stamp=999), budget(open_count=1), budget()]) as fetch, \
             patch.object(common, 'sql', side_effect=AssertionError('不得查询业务预算表')), \
             patch.object(common, 'command', return_value='queue\t0\t0\n'):
            common.drained()
        self.assertEqual(fetch.call_count, 3)


class ContainerProcessTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.build.name) / 'container-exporter'
        build_container_source(ROOT, cls.binary)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        self.cgroup = root / 'cgroup/container'
        self.cgroup.mkdir(parents=True)
        proc = root / 'proc/42'
        proc.mkdir(parents=True)
        (proc / 'cgroup').write_text('0::/container\n')
        for name, body in {'cpu.max': '100000 100000', 'cpu.stat': 'usage_usec 2000000\nnr_periods 10\nnr_throttled 2\nthrottled_usec 200000',
                           'memory.current': '1024', 'memory.peak': '2048', 'memory.max': '4096',
                           'memory.swap.max': '0', 'memory.events': 'oom 0\noom_kill 0', 'pids.current': '3'}.items():
            (self.cgroup / name).write_text(body)
        self.source = ContainerSource(self.binary, root / 'run',
                                      ('--procfs', root / 'proc', '--cgroupfs', root / 'cgroup')).start()
        self.addCleanup(self.source.close)
        self.targets = [{'name': 'mysql', 'id': 'mysql-container-1', 'pid': 42}]

    def test_actual_process_preserves_counters_and_private_manifest(self):
        row = self.source.snapshot(self.targets)
        self.assertEqual(row['containers']['mysql']['cpu']['usage_usec'], 2000000)
        self.assertEqual(os.stat(self.source.targets_file).st_mode & 0o777, 0o600)
        self.assertIn(b'gopulse_container_cpu_usage_seconds_total', self.source.fetch('/metrics'))
        # 保留计数回退，交给窗口汇总判定重置，采集器不改写源数据。
        (self.cgroup / 'cpu.stat').write_text('usage_usec 1\nnr_periods 1\nnr_throttled 0\nthrottled_usec 0')
        row = self.source.snapshot(self.targets)
        self.assertEqual(row['containers']['mysql']['cpu']['usage_usec'], 1)
        self.source.close()
        self.assertIsNotNone(self.source.process.poll())
        self.assertTrue(self.source.process.stdout.closed)

    def test_missing_required_file_fails_and_optional_peak_is_null(self):
        (self.cgroup / 'memory.peak').unlink()
        self.assertIsNone(self.source.snapshot(self.targets)['containers']['mysql']['memory_peak'])
        (self.cgroup / 'memory.current').unlink()
        with self.assertRaises(HTTPError):
            self.source.snapshot(self.targets)

    def test_invalid_manifest_has_no_partial_data(self):
        with self.assertRaises(HTTPError):
            self.source.snapshot(self.targets + self.targets)

    def test_start_failure_reaps_process(self):
        source = LocalSource('/bin/false', self.source.directory / 'failed', 'failed', 'unused')
        with self.assertRaises(RuntimeError):
            source.start()
        self.assertIsNotNone(source.process.poll())
        self.assertTrue(source.log.closed)


class MySQLSourceProcessTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.build.name) / 'gopulse-mysql-exporter'
        subprocess.run(['go', '-C', str(ROOT / 'exporters/mysql'), 'build', '-o', str(cls.binary),
                        '.'], check=True, timeout=600)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def configuration(self, directory, host='127.0.0.1', port=1):
        path = Path(directory) / 'template.json'
        path.write_text(json.dumps({
            'config': {'host': host, 'port': port, 'database': 'fixture', 'username': 'observer',
                       'connect_timeout': '1s', 'scrape_timeout': '2s'},
            'secrets': {'password': 'fixture-secret'},
            'runtime': {'mode': 'host', 'listen_host': '127.0.0.1', 'listen_port': 9122,
                        'shutdown_timeout': '3s'}}))
        path.chmod(0o600)
        return path

    def test_standard_entrypoint_health_and_unreachable_database(self):
        with tempfile.TemporaryDirectory() as temp:
            path = self.configuration(temp)
            before = path.read_bytes()
            source = ConfiguredExporter(self.binary, Path(temp) / 'run', path).start()
            try:
                self.assertEqual(source.process.args[1:3], ['serve', '--config'])
                self.assertEqual(json.loads(source.fetch('/health')), {'status': 'ok', 'contract_version': '2'})
                active = Path(source.process.args[3])
                self.assertEqual(active.stat().st_mode & 0o777, 0o600)
                config = json.loads(active.read_text())
                self.assertEqual(config['config']['host'], '127.0.0.1')
                self.assertEqual(config['secrets']['password'], 'fixture-secret')
                self.assertEqual(path.read_bytes(), before)
                with self.assertRaises(HTTPError) as caught:
                    source.fetch('/metrics')
                self.assertEqual(caught.exception.code, 503)
                self.assertIn(b'gopulse_mysql_up 0', caught.exception.read())
            finally:
                source.close()
            self.assertEqual(source.process.returncode, 0)
            self.assertTrue(source.log.closed)
            self.assertNotIn('fixture-secret', Path(source.log.name).read_text())

    def test_standard_config_rejects_public_target_without_secret_in_log(self):
        with tempfile.TemporaryDirectory() as temp:
            path = self.configuration(temp, host='8.8.8.8')
            source = ConfiguredExporter(self.binary, Path(temp) / 'run', path)
            with self.assertRaises(RuntimeError):
                source.start()
            self.assertIsNone(source.process)
            self.assertTrue(source.log.closed)
            self.assertNotIn('fixture-secret', Path(source.log.name).read_text())

    def test_private_target_uses_standard_config_without_connecting_database(self):
        with tempfile.TemporaryDirectory() as temp:
            path = self.configuration(temp, host='172.19.0.2')
            result = subprocess.run([str(self.binary), 'check-config', '--config', str(path)],
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0)
            self.assertTrue(json.loads(result.stdout)['valid'])

    def test_failed_process_is_reaped(self):
        with tempfile.TemporaryDirectory() as temp:
            path = self.configuration(temp)
            # true 的配置校验成功，serve 随即退出，验证启动失败时的进程回收。
            source = ConfiguredExporter('/bin/true', Path(temp) / 'run', path)
            with self.assertRaises(RuntimeError):
                source.start()
            self.assertIsNotNone(source.process.poll())
            self.assertTrue(source.log.closed)
