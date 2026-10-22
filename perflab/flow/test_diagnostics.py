"""离线验证 公共 PT 的诊断窗口、故障现场留存和停止流程，不访问业务环境。"""
from datetime import datetime, timezone
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from flow.common import QUOTAS
from flow.diagnose import Diagnostics, check_metrics
from flow import diagnose
from flow.analysis import summarize, removed_veth
from diagnostics.metrics import parse_metrics, summarize_source


def counter(name, value, labels=''):
    return f'# TYPE {name} counter\n{name}{labels} {value}\n'


class NodePreparationTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.directory = self.root / 'preparation'
        self.binary = self.directory / 'bin/node_exporter'
        self.binary.parent.mkdir(parents=True)
        self.cache = self.root / '.run/tools/node_exporter/1.9.1/linux-amd64/node_exporter'
        self.body = b'\x7fELF\x02\x01' + bytes(12) + (62).to_bytes(2, 'little') + b'fixture'
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode='w:gz') as archive:
            member = tarfile.TarInfo('node_exporter-1.9.1.linux-amd64/node_exporter')
            member.size = len(self.body)
            archive.addfile(member, io.BytesIO(self.body))
        self.archive = output.getvalue()
        release = self.root / 'exporters/node/upstream.json'
        release.parent.mkdir(parents=True)
        release.write_text(json.dumps({'version': '1.9.1', 'sha256': {'amd64': hashlib.sha256(self.archive).hexdigest()}}))
        for mocked in (patch.object(diagnose, 'ROOT', self.root),
                       patch.object(diagnose.platform, 'system', return_value='Linux'),
                       patch.object(diagnose.platform, 'machine', return_value='x86_64'),
                       patch.object(diagnose.subprocess, 'run', return_value=SimpleNamespace(
                           returncode=0, stdout='node_exporter, version 1.9.1 (branch: HEAD)\n', stderr=''))):
            mocked.start()
            self.addCleanup(mocked.stop)

    def test_existing_binary_reused_without_download_or_packaging(self):
        self.binary.write_bytes(self.body)
        inode = self.binary.stat().st_ino
        with patch.object(diagnose.urllib.request, 'urlopen', side_effect=AssertionError('禁止联网')), \
             patch.object(diagnose, 'command', side_effect=AssertionError('禁止打包')):
            result = diagnose.prepare_node(self.directory)
        self.assertFalse(result['downloaded'])
        self.assertEqual(result['source'], str(self.binary))
        self.assertEqual(self.cache.read_bytes(), self.body)
        self.assertEqual(self.binary.stat().st_ino, inode)

    def test_cached_and_previous_public_binaries_are_reused(self):
        previous = []
        for candidate in (self.cache, *previous):
            with self.subTest(source=str(candidate)):
                for path in (self.binary, self.cache, *previous):
                    path.unlink(missing_ok=True)
                candidate.parent.mkdir(parents=True, exist_ok=True)
                candidate.write_bytes(self.body)
                with patch.object(diagnose.urllib.request, 'urlopen', side_effect=AssertionError('禁止联网')):
                    result = diagnose.prepare_node(self.directory)
                self.assertEqual(result['source'], str(candidate))
                self.assertEqual(self.binary.read_bytes(), self.body)

    def test_missing_binary_downloads_pinned_release(self):
        with patch.object(diagnose.urllib.request, 'urlopen', return_value=io.BytesIO(self.archive)) as download:
            result = diagnose.prepare_node(self.directory)
        self.assertTrue(result['downloaded'])
        self.assertEqual(self.binary.read_bytes(), self.body)
        self.assertEqual(self.cache.read_bytes(), self.body)
        self.assertIn('/v1.9.1/node_exporter-1.9.1.linux-amd64.tar.gz', download.call_args.args[0])

    def test_bad_checksum_preserves_existing_file(self):
        self.binary.write_bytes(b'previous binary')
        with patch.object(diagnose.urllib.request, 'urlopen', return_value=io.BytesIO(b'invalid archive')):
            with self.assertRaisesRegex(AssertionError, '校验失败'):
                diagnose.prepare_node(self.directory)
        self.assertEqual(self.binary.read_bytes(), b'previous binary')
        self.assertFalse(self.cache.exists())

    def test_wrong_version_and_architecture_are_rejected(self):
        self.binary.write_bytes(self.body)
        with patch.object(diagnose.subprocess, 'run', return_value=SimpleNamespace(
                returncode=0, stdout='node_exporter, version 1.9.10\n', stderr='')):
            self.assertFalse(diagnose.valid_node(self.binary, '1.9.1', 'amd64'))
        with patch.object(diagnose.subprocess, 'run') as execute:
            self.assertFalse(diagnose.valid_node(self.binary, '1.9.1', 'arm64'))
            execute.assert_not_called()


class DiagnosticReportTest(unittest.TestCase):
    def fixture(self, directory):
        rows = []
        diagnostics = []
        for stamp in range(0, 16, 5):
            applications = {name: counter('fixture_requests_total', stamp) for name in
                            ('backend', 'backend-2', 'platform-api', 'business-worker', 'business-worker-2',
                             'search-indexer', 'search-indexer-2', 'node')}
            for name, multiplier in (('backend', 2), ('backend-2', 3)):
                applications[name] += counter('gopulse_backend_mysql_pool_wait_duration_seconds_total',
                                               stamp * multiplier, '{pool="api"}')
                applications[name] += counter('gopulse_backend_mysql_pool_wait_duration_seconds_total',
                                               stamp * 100, '{pool="outbox"}')
            resources = {name: {'id': name, 'cpu_max': f'{int(cpus * 100000)} 100000',
                         'cpu': {'usage_usec': stamp * 10000, 'nr_periods': stamp * 10,
                                 'nr_throttled': stamp, 'throttled_usec': stamp * 100},
                         'memory_current': 1024, 'memory_peak': 2048, 'memory_events': {'oom_kill': 0},
                         'pids_current': 2} for name, (cpus, _) in QUOTAS.items()}
            rows.append({'epoch': stamp, 'timestamp': datetime.fromtimestamp(stamp, timezone.utc).isoformat(),
                         'containers': resources, 'application_metrics': applications})
            body = counter('gopulse_mysql_statement_event_executions_total', stamp * 2, '{event="statement/sql/commit"}')
            body += counter('gopulse_mysql_statement_event_seconds_total', stamp / 100, '{event="statement/sql/commit"}')
            diagnostics.append({'type': 'metrics', 'epoch': stamp, 'status': 200, 'body': body})
        for i in range(5, 50):
            diagnostics.append({'type': 'scene', 'epoch': i / 5, 'elapsed_seconds': .01,
                                'errors': [], 'truncated': [], 'row_locks': [{'waiting_trx_id': 1}],
                                'metadata_locks': [], 'sync_history': [{'THREAD_ID': 2, 'EVENT_ID': 1, 'seconds': .1}]})
        (directory / 'resources.jsonl').write_text(''.join(json.dumps(row) + '\n' for row in rows))
        # 触发采样与周期采样可以交错完成，汇总必须按采样开始时间计算间隔。
        diagnostics[-1], diagnostics[-2] = diagnostics[-2], diagnostics[-1]
        (directory / 'database-diagnostics.jsonl').write_text(''.join(json.dumps(row) + '\n' for row in diagnostics))
        (directory / 'diagnostic-runtime.json').write_text('{"error":null}')
        (directory / 'service-logs.json').write_text('{"errors":[]}')

    def test_two_api_pools_and_sync_events_are_not_double_counted(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            self.fixture(directory)
            result = summarize(directory, 1, 9.8)
            self.assertEqual(result['status'], 'complete', result)
            self.assertEqual(result['indicators']['api_pool_wait_seconds_total'], 50)
            self.assertEqual(result['indicators']['commit_events']['statement/sql/commit']['mean_ms'], 5)
            saved = json.loads((directory / 'diagnostics-summary.json').read_text())
            self.assertEqual(saved['scene']['sync_events'], 1)
            self.assertEqual(saved['scene']['sync_seconds_observed'], .1)

    def test_missing_source_and_scene_failure_are_incomplete(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            self.fixture(directory)
            path = directory / 'resources.jsonl'
            rows = [json.loads(line) for line in path.read_text().splitlines()]
            for row in rows:
                del row['application_metrics']['node']
            path.write_text(''.join(json.dumps(row) + '\n' for row in rows))
            with (directory / 'database-diagnostics.jsonl').open('a') as output:
                output.write(json.dumps({'type': 'scene', 'epoch': 5, 'elapsed_seconds': .1,
                                         'errors': ['metadata_locks'], 'truncated': ['statements']}) + '\n')
            result = summarize(directory, 1, 9.8)
            self.assertEqual(result['status'], 'incomplete')
            self.assertIn('metrics_incomplete', result['reasons'])
            self.assertIn('scene_missing_or_failed', result['reasons'])
            self.assertIn('scene_truncated', result['reasons'])

    def test_false_mysql_up_does_not_pass(self):
        with self.assertRaises(AssertionError):
            check_metrics('# TYPE gopulse_mysql_up gauge\ngopulse_mysql_up 0\n')

    def test_unverified_sync_clock_is_not_treated_as_complete(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            self.fixture(directory)
            path = directory / 'database-diagnostics.jsonl'
            rows = [json.loads(line) for line in path.read_text().splitlines()]
            for row in rows:
                if row['type'] == 'scene':
                    row['history_windows'] = {'sync_history': {'begin_timer_exclusive': 0,
                                                               'end_timer_inclusive': 100, 'advanced': True}}
            path.write_text(''.join(json.dumps(row) + '\n' for row in rows))
            result = summarize(directory, 1, 9.8)
            self.assertIn('sync_history_clock_unverified', result['reasons'])
            self.assertEqual(result['status'], 'incomplete')
            for row in rows:
                if row['type'] == 'scene':
                    row['history_windows']['sync_history']['timer_domain'] = 'wait'
            path.write_text(''.join(json.dumps(row) + '\n' for row in rows))
            self.assertEqual(summarize(directory, 1, 9.8)['status'], 'complete')


class NodeClosingBoundaryTest(unittest.TestCase):
    def snapshot(self, stamp, device='vethabc123', present=True, packets=False):
        body = counter('host_total', stamp)
        if present:
            body += counter('node_network_receive_bytes_total', stamp * 10, '{device="' + device + '"}')
        if packets:
            body += counter('node_network_receive_packets_total', stamp, '{device="' + device + '"}')
        return stamp, parse_metrics(body)

    def test_veth_removed_only_at_closing_boundary_is_omitted(self):
        samples = [self.snapshot(0), self.snapshot(5), self.snapshot(10, present=False)]
        result = summarize_source(samples, 1, 9, allow_closing_disappearance=removed_veth)
        self.assertTrue(result['valid'], result)
        self.assertEqual(len(result['omitted_series']), 1)
        self.assertEqual(result['omitted_series'][0]['labels']['device'], 'vethabc123')
        self.assertNotIn('node_network_receive_bytes_total', [item['name'] for item in result['series']])
        self.assertIn('counter_series_missing', summarize_source(samples, 1, 9)['reasons'])

    def test_interior_gaps_stable_devices_and_partial_scrapes_still_fail(self):
        cases = [
            [self.snapshot(0), self.snapshot(5, present=False), self.snapshot(10), self.snapshot(15, present=False)],
            [self.snapshot(0, device='eth0'), self.snapshot(5, device='eth0'), self.snapshot(10, present=False)],
            [self.snapshot(0, packets=True), self.snapshot(5, packets=True), self.snapshot(10, present=False, packets=True)],
        ]
        for samples in cases:
            with self.subTest(samples=len(samples)):
                result = summarize_source(samples, 1, samples[-1][0] - 1, allow_closing_disappearance=removed_veth)
                self.assertFalse(result['valid'])
                self.assertIn('counter_series_missing', result['reasons'])


