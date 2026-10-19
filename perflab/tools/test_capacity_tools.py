import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import compare_ttl


class TTLGateTests(unittest.TestCase):
    def test_gate_requires_all_thresholds_and_identical_workload(self):
        meta = {'snapshot_sha256': 'a', 'sequence_sha256': 'b',
                'effective_config': {'REDIS_POST_DETAIL_TTL': '5m', 'MYSQL_MAX_OPEN_CONNS': '8', 'BACKEND_RESOURCE_REBUILD': '8'}}
        other = dict(meta, effective_config=dict(meta['effective_config'], REDIS_POST_DETAIL_TTL='30m'))
        baseline = {'hit_ratio': .40, 'origin': 1000, 'write_failures': 0,
                    'redis_memory_peak_ratio': .5}
        candidate = dict(baseline, hit_ratio=.51, origin=800, redis_memory_peak_ratio=.74)
        with patch.object(compare_ttl, 'evidence', side_effect=[(meta, baseline), (other, candidate)]):
            self.assertEqual(compare_ttl.compare(None, None)['selected_ttl'], '30m')
        for change in [{'hit_ratio': .49}, {'origin': 801},
                       {'redis_memory_peak_ratio': .75}, {'write_failures': 1}]:
            with patch.object(compare_ttl, 'evidence', side_effect=[(meta, baseline), (other, dict(candidate, **change))]):
                self.assertEqual(compare_ttl.compare(None, None)['selected_ttl'], '5m')
        with patch.object(compare_ttl, 'evidence', side_effect=[(meta, baseline), (dict(other, sequence_sha256='different'), candidate)]):
            with self.assertRaises(ValueError):
                compare_ttl.compare(None, None)

    def test_short_or_missing_samples_cannot_pass(self):
        with tempfile.TemporaryDirectory() as path:
            directory = Path(path)
            (directory / 'windows.json').write_text(json.dumps({'windows': [
                {'name': 'warmup', 'duration_seconds': 1800},
                {'name': 'measure', 'duration_seconds': 1800}]}))
            (directory / 'measure-metrics.jsonl').write_text(json.dumps({'ok': False, 'gap_slots': 0}) + '\n')
            with self.assertRaisesRegex(ValueError, 'incomplete'):
                compare_ttl.evidence(directory)

    def test_prometheus_parser_preserves_result_labels(self):
        body = 'gopulse_backend_cache_requests_total{result="hit"} 20\ngopulse_backend_cache_requests_total{result="miss"} 80\n'
        self.assertEqual(compare_ttl.metric(body, 'gopulse_backend_cache_requests_total', 'hit'), 20)


if __name__ == '__main__':
    unittest.main()
