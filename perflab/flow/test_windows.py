import unittest
from diagnostics.metrics import parse_metrics, summarize_source

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


