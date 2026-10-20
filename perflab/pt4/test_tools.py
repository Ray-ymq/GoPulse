"""离线验证测试四的关键状态、窗口统计和 SQL 增量。"""
import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
from observe import Observation, LegacyObservation
from report import build_report
from sql_delta import COUNTERS, delta, summarize
import run


def snapshot(second, count):
    timestamp = datetime.fromtimestamp(1700000000 + second, timezone.utc).isoformat()
    digest = {name: str(count) for name in COUNTERS}
    digest.update(SCHEMA_NAME='fixture', DIGEST='abc', DIGEST_TEXT='SELECT ?', FIRST_SEEN='unchanged')
    return {'ok': True, 'timestamp': timestamp, 'ended_at': timestamp,
            'queries': {'identity': {'rows': [{'server_uuid': 'one', 'uptime': str(100 + second), 'server_epoch': str(1700000000 + second)}]},
                        'lost': {'rows': [{'Value': '0'}]}, 'digests': {'rows': [digest]}}}


class SQLDeltaTest(unittest.TestCase):
    def test_known_query_counts(self):
        result = delta(snapshot(0, 30), snapshot(10, 38))
        self.assertTrue(result['valid'])
        self.assertEqual(result['digests'][0]['COUNT_STAR'], 8)

    def test_reset_restart_loss_and_missing_digest(self):
        before = snapshot(0, 30)
        after = snapshot(10, 38)
        after['queries']['identity']['rows'][0]['uptime'] = '5'
        self.assertFalse(delta(before, after)['valid'])
        self.assertFalse(delta(before, snapshot(10, 2))['valid'])
        after = snapshot(10, 38); after['queries']['lost']['rows'][0]['Value'] = '1'
        self.assertFalse(delta(before, after)['valid'])
        after = snapshot(10, 38); after['queries']['digests']['rows'] = []
        self.assertFalse(delta(before, after)['valid'])

    def test_intermediate_reset_invalidates_whole_window(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'digest.jsonl'
            path.write_text(''.join(json.dumps(row) + '\n' for row in [snapshot(0, 30), snapshot(10, 2), snapshot(20, 50)]))
            result = summarize(path, Path(directory) / 'delta.json')
            self.assertFalse(result['valid'])
            self.assertIn('counter_reset', result['reasons'])

    def test_measurement_does_not_include_recovery(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'digest.jsonl'
            path.write_text(''.join(json.dumps(snapshot(second, second)) + '\n' for second in range(0, 61, 10)))
            result = summarize(path, Path(directory) / 'delta.json', 1700000011, 1700000029)
            self.assertEqual(result['digests'][0]['COUNT_STAR'], 20)
            self.assertEqual(result['boundary_extra_seconds'], {'before': 1, 'after': 1})


class CriticalStateTest(unittest.TestCase):
    def test_resource_identity_compares_expected_fields(self):
        matches = LegacyObservation.resources.__globals__['configuration_matches']
        actual = {'NanoCpus': 2000000000, 'RestartPolicy': {'Name': 'no', 'MaximumRetryCount': 0}}
        self.assertTrue(matches(actual, {'NanoCpus': 2000000000, 'RestartPolicy': {'Name': 'no'}}))
        self.assertFalse(matches(actual, {'NanoCpus': 4000000000}))

    def test_diagnostic_failure_does_not_refresh_critical_state(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(LegacyObservation.__init__.__globals__, {'environment': lambda: {}}):
            observer = Observation(Path(directory))
            observer.last_state = 123
            observer.database_sample({'ok': False}, True)
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


if __name__ == '__main__':
    unittest.main()
