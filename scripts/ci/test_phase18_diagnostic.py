import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from phase18_diagnostic import aggregate_load_windows, backend_log_summary, outbox_summary


class DiagnosticTest(unittest.TestCase):
    def test_backend_log_summary_keeps_only_bounded_operational_facts(self):
        lines = [
            {"message": "outbox event published", "event_id": "private-event-id"},
            {"message": "outbox publish failed", "reason": "publish_timeout", "event_id": "private-event-id"},
            {"message": "http request completed", "request_id": "0123456789abcdef0123456789abcdef",
             "status": 500, "error_code": "internal_error", "method": "DELETE",
             "route": "/api/v1/posts/:postId", "duration_ms": 41},
        ]
        output = ''.join(json.dumps(line) + '\n' for line in lines)
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch('phase18_diagnostic.compose', return_value=SimpleNamespace(returncode=0, stdout=output)):
                summary = backend_log_summary(Path('/env'), Path('/compose.yaml'), 'project', '2026-09-24T00:00:00Z')
        self.assertEqual(summary['messages']['publish_success'], 1)
        self.assertEqual(summary['messages']['publish_failed'], 1)
        self.assertEqual(summary['publish_failure_reasons'], {'publish_timeout': 1})
        self.assertEqual(summary['server_requests']['0123456789abcdef0123456789abcdef']['status'], 500)
        self.assertNotIn('private-event-id', json.dumps(summary))

    def test_aggregate_load_windows_keeps_phase_statuses_and_top_steady_latency(self):
        report = {
            'windows': [
                {'phase': 'steady', 'sequence': 1, 'requests': 2, 'statuses': {'200': 2},
                 'latency': {'p95_ms': 10, 'p99_ms': 12, 'max_ms': 12},
                 'routes': {'GET /a': {'counts': {'requests': 2}, 'statuses': {'200': 2}}}},
                {'phase': 'steady', 'sequence': 2, 'requests': 3, 'statuses': {'500': 1, '200': 2},
                 'latency': {'p95_ms': 40, 'p99_ms': 50, 'max_ms': 60},
                 'routes': {'DELETE /a': {'counts': {'requests': 3}, 'statuses': {'500': 1, '200': 2}}}},
            ],
        }
        summary = aggregate_load_windows(report)
        self.assertEqual(summary['by_phase']['steady']['requests'], 5)
        self.assertEqual(summary['by_phase']['steady']['statuses'], {'200': 4, '500': 1})
        self.assertEqual(summary['top_steady_windows'][0]['sequence'], 2)

    def test_outbox_summary_reports_net_and_publish_rate(self):
        records = [
            {'observed_at': 10, 'mysql': {'status': {'pending': {'count': 100}, 'published': {'count': 20}}},
             'links': {'backend': {'gopulse_backend_outbox_last_publish_success_timestamp_seconds': 10}}},
            {'observed_at': 70, 'mysql': {'status': {'pending': {'count': 130}, 'published': {'count': 50}}},
             'links': {'backend': {'gopulse_backend_outbox_last_publish_success_timestamp_seconds': 60}}},
        ]
        summary = outbox_summary(records, 0)
        self.assertEqual(summary['pending_delta'], 30)
        self.assertEqual(summary['pending_net_per_minute'], 30)
        self.assertEqual(summary['published_delta'], 30)
        self.assertEqual(summary['publish_success_per_minute'], 30)
        self.assertEqual(summary['sample_interval_seconds']['max'], 60)


if __name__ == '__main__':
    unittest.main()
