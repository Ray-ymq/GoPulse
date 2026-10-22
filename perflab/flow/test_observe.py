import json
from pathlib import Path
import tempfile
import unittest
from flow.audit import parse_events
from flow.observe import recovered
from flow.sample import resource, parse_rabbitmq
from flow.diagnose import check_scene
from diagnostics.metrics import parse_metrics, summarize_source


class ObservationTests(unittest.TestCase):
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

    def test_counter_reset_gap_and_missing(self):
        def metric(value):
            return parse_metrics(f'# TYPE requests_total counter\nrequests_total {value}\n')
        for rows in [[(0, metric(10)), (5, metric(1))], [(0, metric(1)), (20, metric(10))], [(2, metric(1))]]:
            self.assertFalse(summarize_source(rows, 0, 5)['valid'])
        with self.assertRaises(AssertionError):
            check_scene({'type': 'scene', 'errors': ['failed']})
        with self.assertRaises(AssertionError):
            parse_rabbitmq('')
