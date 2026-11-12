import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from flow.audit import parse_events
from flow.observe import Observation


class ObservationTests(unittest.TestCase):
    def test_clock_steps_abort_but_scheduling_delays_do_not(self):
        for wall, monotonic, fails in [(105, 15, False), (105.8, 15, True), (104.2, 15, True)]:
            with self.subTest(wall=wall), tempfile.TemporaryDirectory() as tmp:
                with patch('flow.observe.time.time', return_value=100), patch('flow.observe.time.monotonic', return_value=10):
                    observation = Observation(tmp, tmp, {})
                with patch('flow.observe.time.time', return_value=wall), patch('flow.observe.time.monotonic', return_value=monotonic):
                    if fails:
                        with self.assertRaisesRegex(RuntimeError, '系统时间跳变'):
                            observation.check()
                        error = json.loads((Path(tmp) / 'clock-error.json').read_text())
                        self.assertEqual(error['reason'], 'host_clock_discontinuity')
                    else:
                        observation.check()
                        self.assertFalse((Path(tmp) / 'clock-error.json').exists())

    def test_event_pairing(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'events.jsonl'
            start = {'schema_version': 'gopulse.pt.request.v1', 'type': 'start', 'request_id': 'x', 'route': 'POST /api/v1/posts',
                     'category': 'content_write', 'phase': 'warmup', 'started_at_ms': 1000}
            outcome = dict(start, type='outcome', completed_at_ms=2000, success=True, sample={'response_id': 42})
            path.write_text('\n'.join(json.dumps(v) for v in [start, outcome]) + '\n')
            requests, errors = parse_events(path)
            self.assertFalse(any(errors.values()))
            self.assertEqual(requests['x']['completed'], 2)
            path.write_text(json.dumps(outcome) + '\n{')
            _, errors = parse_events(path)
            self.assertEqual(sum(errors.values()), 2)

    def test_checks_use_business_budget_and_limit_query_frequency(self):
        with tempfile.TemporaryDirectory() as tmp:
            observation = Observation(tmp, tmp, {'check_seconds': 5}, {'id': 'fixed'})
            path = Path(tmp) / 'safety.json'
            with patch('flow.observe.snapshot', return_value={'id': 'fixed'}) as snapshot, \
                 patch('flow.observe.budget', return_value={'open_requirements': 7}):
                first = observation.safety(path)
                self.assertEqual(json.loads(path.read_text())['backlog'], 7)
                self.assertEqual(observation.safety(path), first)
                self.assertEqual(snapshot.call_count, 1)
                observation.checked_at -= 6
                snapshot.return_value = {'id': 'changed'}
                with self.assertRaisesRegex(RuntimeError, '环境身份'):
                    observation.safety(path)

    def test_database_failure_does_not_create_ready_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'safety.json'
            observation = Observation(tmp, tmp, {'check_seconds': 5})
            with patch('flow.observe.snapshot', return_value={}), \
                 patch('flow.observe.budget', side_effect=RuntimeError('database unavailable')):
                with self.assertRaises(RuntimeError):
                    observation.safety(path)
            self.assertFalse(path.exists())
