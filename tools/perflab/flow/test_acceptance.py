"""在隔离 fixture 中验证固定阶段和独立验收流程。"""
import unittest
from contextlib import nullcontext
from unittest.mock import patch
from flow.config import load
from flow.executor import Pipeline, routes
from flow.test_executor import FixtureBackend


class FixedFlowAcceptanceTests(unittest.TestCase):
    def test_acceptance_observes_a_full_failure_window_with_fixed_resources(self):
        from flow.__main__ import main
        with patch('sys.argv', ['pt', 'acceptance', '--start-rps', '250']), \
                patch('flow.__main__.lock', return_value=nullcontext()), \
                patch('flow.executor.execute', return_value=0) as execute:
            self.assertEqual(main(), 0)
        config = execute.call_args.args[0]
        self.assertGreater(config['measurement_seconds'], 10)
        self.assertEqual(config['recovery_seconds'], 120)
        self.assertEqual(load()['recovery_seconds'], 600)
        changed = {'acceptance', 'idle_seconds', 'warmup_seconds', 'measurement_seconds', 'soak_seconds', 'recovery_seconds'}
        self.assertEqual({k: v for k, v in config.items() if k not in changed},
                         {k: v for k, v in load('250').items() if k not in changed})

    def test_all_routes_and_independent_baselines(self):
        backend = FixtureBackend(abnormal=200)
        backend.routes = routes
        pipeline = Pipeline(load('250'), backend)
        self.assertEqual(pipeline.run(), 0)
        self.assertEqual(len([g for g in pipeline.groups if g['group_id'].startswith('route-')]), 22)
        self.assertEqual(len([g for g in pipeline.groups if g['group_id'].startswith('repeat-')]), 3)
        self.assertEqual(backend.calls.count('restore'), len(pipeline.groups)-1)
        self.assertEqual(backend.calls[0:2], ['idle', 'low'])
        self.assertEqual(pipeline.groups[-1]['group_id'], 'route-22')
