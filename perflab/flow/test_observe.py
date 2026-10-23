import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from flow.audit import parse_events
from flow.observe import Observation, recovered
from flow.sample import resource, parse_rabbitmq
from flow.diagnose import check_scene
from diagnostics.metrics import parse_metrics, summarize_source


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

    def test_recovery(self):
        value = {'ok': True, 'business': True, 'budget': {'open_requirements': 0, 'parked_messages': 0}}
        sample = {'rabbitmq': {'messages_ready': 0, 'messages_unacknowledged': 0}}
        self.assertTrue(recovered(value, sample))
        value['budget']['open_requirements'] = 1
        self.assertFalse(recovered(value, sample))

    def test_zero_backlog_before_stop_does_not_prove_recovery(self):
        value = {'ok': True, 'business': True, 'budget': {'open_requirements': 0, 'parked_messages': 0, 'sampled_at': 100}}
        sample = {'epoch': 100, 'rabbitmq': {'messages_ready': 0, 'messages_unacknowledged': 0}}
        self.assertFalse(recovered(value, sample, not_before=101))
        value['budget']['sampled_at'] = 102
        self.assertFalse(recovered(value, sample, not_before=101))
        sample['epoch'] = 102
        self.assertTrue(recovered(value, sample, not_before=101))

    def test_counter_reset_gap_and_missing(self):
        def metric(value):
            return parse_metrics(f'# TYPE requests_total counter\nrequests_total {value}\n')
        for rows in [[(0, metric(10)), (5, metric(1))], [(0, metric(1)), (20, metric(10))], [(2, metric(1))]]:
            self.assertFalse(summarize_source(rows, 0, 5)['valid'])
        with self.assertRaises(AssertionError):
            check_scene({'type': 'scene', 'errors': ['failed']})
        with self.assertRaises(AssertionError):
            parse_rabbitmq('')
