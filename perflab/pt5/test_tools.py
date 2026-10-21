"""测试五资源窗口、事件解析和配额解析的单元测试；不访问数据库或容器。"""
import json
from pathlib import Path
import tempfile
import unittest

import audit
import quota
import sample


class QuotaTest(unittest.TestCase):
    def test_parse_cpu_max(self):
        self.assertEqual(quota.parse_cpu_max('200000 100000'), (2.0, 100000))
        self.assertEqual(quota.parse_cpu_max('400000 100000'), (4.0, 100000))
        self.assertEqual(quota.parse_cpu_max(None), (None, None))

    def test_parse_memory_max(self):
        self.assertEqual(quota.parse_memory_max('2147483648'), 2147483648)
        self.assertIsNone(quota.parse_memory_max('max'))


class EventTest(unittest.TestCase):
    def test_parse_events_and_resource(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'events.log'
            events = [
                {'type': 'start', 'slot': 1, 'phase': 'measurement', 'route': 'POST /api/v1/posts',
                 'category': 'content_write', 'started_at_ms': 1000, 'scenario_started_at_ms': 0},
                {'type': 'outcome', 'slot': 1, 'phase': 'measurement', 'route': 'POST /api/v1/posts',
                 'category': 'content_write', 'started_at_ms': 1000, 'scenario_started_at_ms': 0,
                 'completed_at_ms': 1500, 'success': True, 'status': 201, 'business_code': 'none',
                 'client_error': 'none', 'sample': {'actor': 0, 'path': '/api/v1/posts',
                                                     'expected': {'title': 't', 'content': 'c'}, 'response_id': 9}},
                {'type': 'start', 'slot': 2, 'phase': 'measurement', 'route': 'PUT /api/v1/posts/:postId/like',
                 'category': 'interaction_write', 'started_at_ms': 2000, 'scenario_started_at_ms': 0},
            ]
            path.write_text('\n'.join(json.dumps({'msg': 'PT4 ' + json.dumps(event)}) for event in events) + '\n')
            requests, errors = audit.parse_events(path)
        self.assertEqual(errors, {'invalid_event_json': 0, 'outcome_without_start': 0})
        self.assertEqual(requests[1]['route'], 'POST /api/v1/posts')
        self.assertEqual(requests[1]['started'], 1.0)
        self.assertEqual(requests[1]['completed'], 1.5)
        self.assertNotIn('completed', requests[2])
        users = [{'id': 7, 'username': 'u'}]
        value = audit.resource(requests[1], users)
        self.assertEqual(value['kind'], 'post_create')
        self.assertEqual(value['key'], 9)
        self.assertEqual(value['user_id'], 7)
        self.assertEqual(value['expected'], {'title': 't', 'content': 'c'})


class SampleTest(unittest.TestCase):
    def _write(self, path, count=13, cpu_max='200000 100000', memory_max=2 * 1024**3, throttle=True):
        rows = []
        for index in range(count):
            epoch = 1000 + index * 5
            rows.append({
                'timestamp': '2026-10-21T00:00:00+00:00', 'epoch': epoch, 'container_id': 'x',
                'cpu_max': cpu_max,
                'cpu': {'usage_usec': 1_000_000 * index, 'user_usec': 0, 'system_usec': 0,
                        'nr_periods': 50 * index, 'nr_throttled': (50 if throttle else 0) * index,
                        'throttled_usec': 2_000_000 * index},
                'memory_current': 1024**3, 'memory_peak': 1024**3, 'memory_max': memory_max,
                'memory_events': {'oom_kill': 0}, 'pids_current': 40,
            })
        path.write_text('\n'.join(json.dumps(row) for row in rows) + '\n')

    def test_summary_uses_window_delta(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'resources.jsonl'
            self._write(path)
            value = sample.summarize(path, 1010, 1050, 2)
        self.assertTrue(value['valid'], value['reasons'])
        self.assertEqual(value['window']['seconds'], 40)
        self.assertAlmostEqual(value['cpu']['usage_percent_avg'], 20.0)
        self.assertEqual(value['cpu']['nr_periods'], 400)
        self.assertEqual(value['cpu']['nr_throttled'], 400)
        self.assertAlmostEqual(value['cpu']['throttled_seconds'], 16.0)

    def test_summary_rejects_quota_change(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'resources.jsonl'
            self._write(path, cpu_max='400000 100000')
            value = sample.summarize(path, 1010, 1050, 2)
        self.assertFalse(value['valid'])
        self.assertIn('quota_not_expected', value['reasons'])

    def test_summary_rejects_sampling_gap(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'resources.jsonl'
            self._write(path)
            rows = [json.loads(line) for line in path.read_text().splitlines()]
            rows = [row for row in rows if row['epoch'] not in (1025, 1030)]
            path.write_text('\n'.join(json.dumps(row) for row in rows) + '\n')
            value = sample.summarize(path, 1010, 1050, 2)
        self.assertFalse(value['valid'])
        self.assertIn('sampling_gap', value['reasons'])


if __name__ == '__main__':
    unittest.main()
