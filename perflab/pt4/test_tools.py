"""离线验证测试四的关键状态、窗口统计和 exporter 增量。"""
from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
from observe import Observation, LegacyObservation
from report import build_report
from metrics_report import summarize_source
from exporters import parse_metrics
import run


class MetricsWindowTest(unittest.TestCase):
    def snapshot(self, stamp, value, extra=''):
        return (stamp, parse_metrics('# TYPE fixture_total counter\nfixture_total ' + str(value) + '\n' + extra))

    def test_window_excludes_recovery_and_detects_reset(self):
        samples = [self.snapshot(stamp, stamp) for stamp in range(0, 31, 5)]
        result = summarize_source(samples, 6, 19)
        self.assertTrue(result['valid'])
        self.assertEqual(result['series'][0]['delta'], 15)
        reset = [self.snapshot(0, 9), self.snapshot(5, 2), self.snapshot(10, 12)]
        self.assertIn('counter_reset', summarize_source(reset, 1, 9)['reasons'])

    def test_missing_boundaries_and_sampling_gaps_are_invalid(self):
        self.assertFalse(summarize_source([self.snapshot(5, 1)], 1, 9)['valid'])
        result = summarize_source([self.snapshot(0, 1), self.snapshot(20, 2)], 1, 19)
        self.assertIn('sampling_gap', result['reasons'])

    def test_truncation_loss_and_series_disappearance_are_invalid(self):
        before = self.snapshot(0, 1, 'gopulse_mysql_collector_truncated{collector="statement_digest"} 1\n')
        result = summarize_source([before, self.snapshot(5, 2)], 1, 4)
        self.assertIn('collector_truncated', result['reasons'])
        missing = (5, parse_metrics('other_metric 1\n'))
        result = summarize_source([self.snapshot(0, 1), missing, self.snapshot(10, 2)], 1, 9)
        self.assertIn('counter_series_missing', result['reasons'])

    def test_series_first_seen_at_closing_boundary_is_outside_window(self):
        samples = [self.snapshot(0, 1), self.snapshot(5, 2),
                   self.snapshot(10, 2, '# TYPE late_total counter\nlate_total 1\n'),
                   self.snapshot(15, 2, '# TYPE late_total counter\nlate_total 2\n')]
        result = summarize_source(samples, 1, 9)
        self.assertTrue(result['valid'])
        self.assertNotIn('late_total', [row['name'] for row in result['series']])

    def test_series_first_seen_inside_window_is_partial_but_valid(self):
        samples = [self.snapshot(0, 1), self.snapshot(5, 2, '# TYPE mid_total counter\nmid_total 1\n'),
                   self.snapshot(10, 2, '# TYPE mid_total counter\nmid_total 2\n')]
        result = summarize_source(samples, 1, 9)
        self.assertTrue(result['valid'])
        row = next(item for item in result['series'] if item['name'] == 'mid_total')
        self.assertTrue(row['partial'])
        self.assertEqual(row['delta'], 2)
        self.assertEqual(result['partial_series'][0]['name'], 'mid_total')

    def test_series_missing_after_first_appearance_is_still_incomplete(self):
        samples = [self.snapshot(0, 1), self.snapshot(5, 2, '# TYPE gap_total counter\ngap_total 1\n'),
                   self.snapshot(10, 2), self.snapshot(15, 2, '# TYPE gap_total counter\ngap_total 2\n')]
        result = summarize_source(samples, 1, 14)
        self.assertIn('counter_series_missing', result['reasons'])


class CriticalStateTest(unittest.TestCase):
    def test_failed_check_does_not_refresh_environment_state(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(LegacyObservation.__init__.__globals__, {'environment': lambda: {}}):
            observer = Observation(Path(directory))
            observer.last_state = 123
            observer.database_sample({'ok': False, 'timestamp': datetime.now(timezone.utc).isoformat()}, True)
            self.assertEqual(observer.last_state, 123)
            self.assertIsNone(observer.reason)


class CohortTest(unittest.TestCase):
    def test_cross_window_unfinished_and_recovery_are_separate(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            run = {'run_id': 'fixture', 'group': 'fixture', 'warmup_seconds': 1, 'measurement_seconds': 2,
                   'load_started_epoch': 100, 'stop_requested_epoch': 103, 'load_exit_code': 1, 'rps': 1}
            (directory / 'run.json').write_text(json.dumps(run))
            events = []
            for slot, start, phase in [(0, 100.5, 'warmup'), (1, 101.1, 'measurement'), (2, 102.5, 'measurement')]:
                event = {'type': 'start', 'slot': slot, 'phase': phase, 'route': 'GET /test', 'category': 'read',
                         'started_at_ms': start * 1000, 'scenario_started_at_ms': 100000}
                events.append(event)
                if slot < 2:
                    events.append({**event, 'type': 'outcome', 'completed_at_ms': (101.5 if slot == 0 else 103.5) * 1000,
                                   'success': True, 'status': 200, 'business_code': 'none', 'client_error': 'none', 'http_ms': 100, 'end_to_end_ms': 101})
            (directory / 'events.log').write_text(''.join(json.dumps({'msg': 'PT4 ' + json.dumps(row)}) + '\n' for row in events))
            result = build_report(directory)
            self.assertEqual(result['measurement']['started'], 2)
            self.assertEqual(result['measurement']['outcomes'], 1)
            self.assertEqual(result['measurement']['unfinished'], 1)
            self.assertEqual(result['measurement']['error_rate'], .5)
            self.assertEqual(result['measurement']['wall_clock_completed'], 1)
            self.assertEqual(result['recovery']['status'], 'missing')
            self.assertEqual(result['execution'], 'incomplete')


class RecoveryTest(unittest.TestCase):
    def test_recovery_requires_fresh_environment_and_all_epochs_drained(self):
        class Client:
            def login(self):
                return 200
            def request(self, method, path, *_):
                return (201 if method == 'POST' else 204 if method == 'DELETE' else 200), {'data': {'id': 1}}
        for age, pending, expected in [(0, 0, 'passed'), (20, 0, 'failed'), (0, 1, 'failed')]:
            with self.subTest(age=age, pending=pending), tempfile.TemporaryDirectory() as directory:
                directory = Path(directory)
                (directory / 'database-state-latest.json').write_text(json.dumps({
                    'timestamp': datetime.fromtimestamp(1700000000 - age, timezone.utc).isoformat(),
                    'backlog': [{'open_requirements': '0', 'parked_messages': '0'},
                                {'open_requirements': str(pending), 'parked_messages': '0'}]}))
                clock = [0.0]
                with patch.object(run, 'legacy_module', return_value=SimpleNamespace(Client=Client)), \
                     patch.object(run, 'guard', return_value={}), \
                     patch.object(run.time, 'monotonic', side_effect=lambda: clock[0]), \
                     patch.object(run.time, 'time', return_value=1700000000), \
                     patch.object(run.time, 'sleep', side_effect=lambda seconds: clock.__setitem__(0, clock[0] + seconds)):
                    self.assertEqual(run.recovery(directory, 1)['status'], expected)

    def test_observer_and_business_failure_preserve_600_seconds(self):
        clock = [0.0]
        def sleep(seconds):
            clock[0] += seconds
        class FailedClient:
            def login(self):
                raise OSError('fixture unavailable')
        with tempfile.TemporaryDirectory() as directory, \
             patch.object(run, 'legacy_module', return_value=SimpleNamespace(Client=FailedClient)), \
             patch.object(run, 'guard', side_effect=RuntimeError('fixture observer unavailable')), \
             patch.object(run.time, 'monotonic', side_effect=lambda: clock[0]), \
             patch.object(run.time, 'sleep', side_effect=sleep):
            result = run.recovery(Path(directory), 600)
            self.assertEqual(result['observation_seconds'], 600)
            self.assertFalse(result['last_business_ok'])
            self.assertEqual(len((Path(directory) / 'recovery.jsonl').read_text().splitlines()), 60)


class SummaryResetTest(unittest.TestCase):
    def test_only_performance_schema_summary_tables_are_reset(self):
        self.assertTrue(run.RESET_SUMMARY_TABLES)
        for table in run.RESET_SUMMARY_TABLES:
            self.assertTrue(table.startswith('performance_schema.events_'))
            self.assertNotIn('setup_', table)
            self.assertNotIn('global_status', table)

    def test_reset_summary_truncates_every_table(self):
        with patch.object(run, 'sql', return_value='') as sql:
            result = run.reset_summary()
        query = sql.call_args.args[0]
        for table in run.RESET_SUMMARY_TABLES:
            self.assertIn('TRUNCATE TABLE ' + table, query)
        self.assertEqual(result['tables'], list(run.RESET_SUMMARY_TABLES))


if __name__ == '__main__':
    unittest.main()
