"""离线验证 PT6 的诊断窗口、故障现场留存和停止流程，不访问业务环境。"""
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

from common import QUOTAS
from diagnose import Diagnostics, check_metrics
import diagnose
from report import summarize, removed_veth
from diagnostics.metrics import parse_metrics, summarize_source
import prepare
import run


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
        previous = [self.root / f'.run/{phase}/bin/node_exporter' for phase in ('pt4', 'pt3')]
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


class IncidentLifecycleTest(unittest.TestCase):
    def test_recovery_window_has_initial_and_final_samples(self):
        with tempfile.TemporaryDirectory() as temp:
            clock = [100.0]
            epochs = []
            sampler = SimpleNamespace(samples=0, error=None)
            def start():
                epochs.append(clock[0])
                sampler.samples = 1
            def close():
                clock[0] += .2
                epochs.append(clock[0])
                sampler.samples += 1
            sampler.start, sampler.close = start, close
            active = SimpleNamespace(error=None, trigger=lambda _: None, close=lambda: None)
            def summary(directory, begin, end):
                self.assertLessEqual(epochs[0], begin)
                self.assertGreaterEqual(epochs[-1], end)
                self.assertEqual(end - begin, 600)
                return {'status': 'complete'}
            with patch.object(run, 'STATE', Path(temp)), \
                 patch.object(run, 'Diagnostics', return_value=SimpleNamespace(start=lambda: active)), \
                 patch.object(run, 'Sampler', return_value=sampler), \
                 patch.object(run.time, 'time', side_effect=lambda: clock[0]), \
                 patch.object(run.time, 'monotonic', side_effect=lambda: clock[0]), \
                 patch.object(run.time, 'sleep', side_effect=lambda _: clock.__setitem__(0, clock[0] + 600)), \
                 patch.object(run, 'capture_logs'), \
                 patch.object(run, 'summarize', side_effect=summary):
                run.observe_recovery(Path(temp), temp)
            result = json.loads((Path(temp) / 'recovery-result.json').read_text())
            self.assertEqual(result['observation_seconds'], 600)
            self.assertIsNone(result['collection_error'])
            self.assertEqual(result['diagnostics']['status'], 'complete')

    def test_diagnostic_stop_is_not_reported_as_business_failed_rps(self):
        with tempfile.TemporaryDirectory() as temp:
            locks = []
            prepared = {'directory': temp, 'containers': {}, 'source_sha256': {}}
            result = {'passed': False, 'load_passed': True, 'reasons': ['diagnostics_incomplete']}
            with patch.object(run, 'STATE', Path(temp)), \
                 patch.object(run, 'verify_prepared', return_value=prepared), \
                 patch.object(run, 'check_host'), patch.object(run, 'check_quotas', return_value={}), \
                 patch.object(run, 'drained'), patch.object(run, 'observe_recovery'), \
                 patch.object(run, 'stage', return_value=result) as stage, \
                 patch.object(run.fcntl, 'flock', side_effect=lambda stream, _: locks.append(stream)), \
                 patch('builtins.print'), patch('sys.argv', ['run.py']):
                run.main()
            for stream in locks:
                stream.close()
            stage.assert_called_once()
            report = json.loads(next(Path(temp).glob('run-*/report.json')).read_text())
            self.assertEqual(report['start_rps'], 250)
            self.assertIsNone(report['failed_rps'])
            self.assertIsNone(report['last_passed_rps'])
            self.assertEqual(report['load_passed_rps'], 250)
            self.assertEqual(report['stopped_rps'], 250)

    def test_custom_start_preserves_ladder_and_corpus_offset(self):
        with tempfile.TemporaryDirectory() as temp:
            locks = []
            prepared = {'directory': temp, 'containers': {}, 'source_sha256': {}}
            results = [
                {'passed': True, 'load_passed': True, 'reasons': [], 'started': 99000},
                {'passed': False, 'load_passed': False, 'reasons': ['business_failure']},
            ]
            with patch.object(run, 'STATE', Path(temp)), \
                 patch.object(run, 'verify_prepared', return_value=prepared), \
                 patch.object(run, 'check_host'), patch.object(run, 'check_quotas', return_value={}), \
                 patch.object(run, 'drained'), patch.object(run, 'observe_recovery'), \
                 patch.object(run, 'stage', side_effect=results) as stage, \
                 patch.object(run.fcntl, 'flock', side_effect=lambda stream, _: locks.append(stream)), \
                 patch('builtins.print'), patch('sys.argv', ['run.py', '--start-rps', '300']):
                run.main()
            for stream in locks:
                stream.close()
            self.assertEqual([(call.args[2], call.args[3]) for call in stage.call_args_list],
                             [(300, 0), (350, 99000)])
            report = json.loads(next(Path(temp).glob('run-*/report.json')).read_text())
            self.assertEqual(report['start_rps'], 300)
            self.assertEqual(report['last_passed_rps'], 300)
            self.assertEqual(report['load_passed_rps'], 300)
            self.assertEqual(report['failed_rps'], 350)
            self.assertEqual(report['stopped_rps'], 350)

    def test_invalid_start_is_rejected_before_environment_access(self):
        with tempfile.TemporaryDirectory() as temp:
            state = Path(temp) / 'state'
            for value in ('-1', '0', '249', '275', '300.5'):
                with self.subTest(start_rps=value), \
                     patch.object(run, 'STATE', state), \
                     patch.object(run, 'verify_prepared') as verify, \
                     patch('sys.stderr', new_callable=io.StringIO), \
                     patch('sys.argv', ['run.py', '--start-rps', value]):
                    with self.assertRaises(SystemExit) as error:
                        run.main()
                    self.assertEqual(error.exception.code, 2)
                    verify.assert_not_called()
                    self.assertFalse(state.exists())

    def test_diagnostic_failure_stops_running_load(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            now = datetime.now(timezone.utc)
            active = SimpleNamespace(error=None, node_url='mock', trigger=lambda _: None, close=lambda: None)
            sampler = SimpleNamespace(samples=1, error=None, start=lambda: None, close=lambda: None)
            def process(args, **kwargs):
                event = {'type': 'clock', 'scenario_started_at_ms': now.timestamp() * 1000 - 330000}
                kwargs['stdout'].write('PT6 ' + json.dumps(event) + '\n')
                kwargs['stdout'].flush()
                (directory / '250-rps/summary.json').write_text('{"metrics":{}}')
                active.error = 'diagnostic_scene_stale'
                return SimpleNamespace(poll=lambda: None, wait=lambda **_: 0)
            with patch.object(run, 'Diagnostics', return_value=SimpleNamespace(start=lambda: active)), \
                 patch.object(run, 'Sampler', return_value=sampler), \
                 patch.object(run.subprocess, 'Popen', side_effect=process), \
                 patch.object(run.subprocess, 'run') as stop, \
                 patch.object(run, 'compose_environment', return_value={}), \
                 patch.object(run, 'capture_logs'), \
                 patch.object(run, 'summarize', return_value={'status': 'incomplete', 'window': {}}):
                result = run.stage({'directory': str(directory)}, directory, 250, 0)
            self.assertFalse(result['passed'])
            self.assertIn('diagnostic_scene_stale', result['reasons'])
            stop.assert_called_once()
            self.assertEqual(stop.call_args.args[0][:2], ['docker', 'stop'])

    def test_upgrade_retry_preserves_ready_and_stopped_phases(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp) / 'preparation-fixture'
            directory.mkdir()
            for phase in ('ready', 'load_stopped'):
                with self.subTest(phase=phase), \
                     patch.object(prepare, 'STATE', Path(temp)), \
                     patch.object(prepare, 'check_quotas', return_value={}), \
                     patch.object(prepare, 'prepare_diagnostics'), \
                     patch.object(prepare, 'collection_preflight', side_effect=[RuntimeError('fixture'), 3]):
                    value = {'phase': phase, 'ready': phase == 'ready', 'directory': str(directory),
                             'containers': {}, 'artifact_sha256': {}}
                    with self.assertRaises(RuntimeError):
                        prepare.upgrade_diagnostics(value)
                    self.assertFalse(json.loads((Path(temp) / 'prepared.json').read_text())['ready'])
                    prepare.upgrade_diagnostics(value)
                    self.assertEqual(value['ready'], phase == 'ready')
                    self.assertEqual(value['phase'], phase)

    def test_failed_scene_is_saved_before_collection_stops(self):
        with tempfile.TemporaryDirectory() as temp:
            diagnostics = Diagnostics(temp, temp)
            row = {'type': 'scene', 'epoch': 1, 'trigger': 'periodic', 'errors': ['row_locks']}
            diagnostics.process = SimpleNamespace(stdout=io.StringIO(json.dumps(row) + '\n'))
            diagnostics.stream = io.StringIO()
            diagnostics.read()
            self.assertIn('diagnostic_collection_failed', diagnostics.failure)
            self.assertEqual(json.loads(diagnostics.stream.getvalue())['errors'], ['row_locks'])

    def test_k6_first_failure_triggers_scene_and_keeps_failure_result(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            now = datetime.now(timezone.utc)
            clock = {'type': 'clock', 'scenario_started_at_ms': now.timestamp() * 1000 - 31000}
            failure = {'type': 'first_failure', 'timestamp': now.isoformat(), 'phase': 'measurement',
                       'status': 500, 'route': 'DELETE /api/v1/posts/:postId'}
            active = SimpleNamespace(error=None, node_url='mock', trigger=lambda reason: triggers.append(reason),
                                     close=lambda: triggers.append('closed'))
            triggers = []
            def process(args, **kwargs):
                for event in (clock, failure):
                    kwargs['stdout'].write('PT6 ' + json.dumps(event) + '\n')
                kwargs['stdout'].flush()
                output = directory / '250-rps'
                (output / 'summary.json').write_text('{"metrics":{}}')
                return SimpleNamespace(poll=lambda: 108, wait=lambda **_: 108)
            sampler = SimpleNamespace(samples=1, error=None, start=lambda: None, close=lambda: None)
            with patch.object(run, 'Diagnostics', return_value=SimpleNamespace(start=lambda: active)), \
                 patch.object(run, 'Sampler', return_value=sampler), \
                 patch.object(run.subprocess, 'Popen', side_effect=process), \
                 patch.object(run, 'compose_environment', return_value={}), \
                 patch.object(run, 'capture_logs'), \
                 patch.object(run, 'summarize', return_value={'status': 'complete', 'window': {}}):
                result = run.stage({'directory': str(directory)}, directory, 250, 0)
            self.assertFalse(result['passed'])
            self.assertEqual(result['first_failure']['status'], 500)
            self.assertEqual(triggers, ['first_failure', 'closed'])
            self.assertTrue((directory / '250-rps/failure-trigger.json').exists())


if __name__ == '__main__':
    unittest.main()
