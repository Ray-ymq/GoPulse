import copy
import json
from pathlib import Path
import tempfile
import unittest
from flow.config import load, contract
from flow.executor import Pipeline, Abort, boundary
from flow.report import report
from flow.runtime import Run, write_json


class FixtureBackend:
    def __init__(self, abnormal=80, recover=True, evidence=True):
        self.abnormal, self.recover, self.evidence = abnormal, recover, evidence
        self.calls = []

    def idle(self): self.calls.append('idle')
    def restore(self): self.calls.append('restore')
    def routes(self): return [['read', 'GET', '/posts'], ['read', 'GET', '/users']]
    def refine(self, low, high):
        if (high-low)/low <= .1: return None
        return {'target_rps': (high+low)//2}
    def group(self, name, target, seconds, scan, route):
        self.calls.append(name)
        targets = [target] if not scan else ([target, min(6400, target*2)] if target < 6400 else [6400])
        stages = [dict(name=f'{name}-{r}', target_rps=r, business_passed=self.abnormal is None or r < self.abnormal,
                       execution_completed=True, stop_reason='') for r in targets]
        return contract('group', group_id=name, execution_completed=True, business_passed=all(s['business_passed'] for s in stages),
                        stop_reason='', load={'stages': stages}, recovery={'passed': self.recover}, audit={'passed': True},
                        evidence={'passed': self.evidence}, resources={'passed': True})


class PipelineTests(unittest.TestCase):
    def test_high_start_first_abnormal_uses_low(self):
        backend = FixtureBackend(abnormal=200)
        pipeline = Pipeline(load('250'), backend)
        self.assertEqual(pipeline.run(), 0)
        self.assertTrue(pipeline.capacity['valid'])
        self.assertGreaterEqual(pipeline.capacity['normal_rps'], 10)
        self.assertIn('repeat-3', backend.calls)
        self.assertIn('soak-80', backend.calls)
        self.assertIn('route-02', backend.calls)
        self.assertEqual(pipeline.groups[0]['group_id'], 'low')

    def test_no_abnormal_and_upper_limit(self):
        backend = FixtureBackend(abnormal=None)
        pipeline = Pipeline(load('6400'), backend)
        self.assertEqual(pipeline.run(), 0)
        self.assertNotIn('repeat-1', backend.calls)
        self.assertEqual(pipeline.capacity['normal_rps'], 6400)
        self.assertFalse(pipeline.capacity['valid'])
        self.assertEqual(boundary([])['normal_rps'], None)

    def test_over_50_current_group_then_continue(self):
        backend = FixtureBackend(abnormal=200)
        original = backend.group
        def stopped(name, *args):
            value = original(name, *args)
            if name == 'scan':
                for stage in value['load']['stages']:
                    stage.update(execution_completed=False, business_passed=False, stop_reason='failure_rate_over_50_percent')
            return value
        backend.group = stopped
        self.assertEqual(Pipeline(load('250'), backend).run(), 0)
        self.assertIn('route-02', backend.calls)

    def test_recovery_or_evidence_stops(self):
        for kwargs in [{'recover': False}, {'evidence': False}]:
            backend = FixtureBackend(**kwargs)
            with self.assertRaises(Abort): Pipeline(load(), backend).run()
            self.assertNotIn('scan', backend.calls)

    def test_low_failure_stops_capacity(self):
        backend = FixtureBackend(abnormal=10)
        pipeline = Pipeline(load(), backend)
        self.assertEqual(pipeline.run(), 1)
        self.assertNotIn('scan', backend.calls)

    def test_report_offline_and_partial(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = Run(load('250'), Path(tmp))
            run.finish(3, 'resource_changed')
            first = report(run.directory)
            second = report(run.directory)
            self.assertEqual(first, second)
            self.assertFalse(first['execution_completed'])
            self.assertEqual(first['skipped_scan_rps'], [10, 20, 40, 80, 160])
            self.assertEqual(first['measured_scan_rps'], [])
