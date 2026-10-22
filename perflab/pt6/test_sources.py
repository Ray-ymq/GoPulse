"""使用临时 cgroup 文件和实际采集进程验证指标来源，不访问 Docker 或业务库。"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from urllib.error import HTTPError
from unittest.mock import patch

from common import ROOT
import common
from diagnostics.budget import parse_budget
from diagnostics.sources import ContainerSource, LocalSource, build_container_source


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
        cls.binary = Path(cls.build.name) / 'mysql-metrics-source'
        subprocess.run(['go', '-C', str(ROOT / 'exporters/mysql'), 'build', '-o', str(cls.binary),
                        './cmd/metrics-source'], check=True, timeout=600)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def test_unreachable_database_remains_http_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'diagnostic.dsn'
            path.write_text('observer:fixture-secret@tcp(127.0.0.1:1)/fixture')
            path.chmod(0o600)
            source = LocalSource(self.binary, Path(temp) / 'run', 'mysql', 'gopulse.mysql.metrics.v1',
                                 ('--dsn-file', path)).start()
            try:
                with self.assertRaises(HTTPError) as caught:
                    source.fetch('/metrics')
                self.assertEqual(caught.exception.code, 503)
                self.assertIn(b'gopulse_mysql_up 0', caught.exception.read())
            finally:
                source.close()
            self.assertIsNotNone(source.process.poll())
            self.assertNotIn('fixture-secret', Path(source.log.name).read_text())

    def test_invalid_dsn_rejected_without_secret_in_log(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'diagnostic.dsn'
            path.write_text('fixture-secret@invalid')
            source = LocalSource(self.binary, Path(temp) / 'run', 'mysql', 'gopulse.mysql.metrics.v1',
                                 ('--dsn-file', path))
            with self.assertRaises(RuntimeError):
                source.start()
            self.assertIsNotNone(source.process.poll())
            self.assertNotIn('fixture-secret', Path(source.log.name).read_text())
