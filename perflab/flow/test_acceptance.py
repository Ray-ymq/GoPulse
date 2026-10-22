"""在隔离 fixture 中走完固定阶段，并验证失败分支。"""
import unittest
import json
from pathlib import Path
import tempfile
from unittest.mock import patch
from flow.acceptance import check
from flow.config import fingerprint
from flow.config import load
from flow.executor import Pipeline, routes
from flow.test_executor import FixtureBackend


class FixedFlowAcceptanceTests(unittest.TestCase):
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

    def test_formal_gate_rejects_missing_and_changed_receipts(self):
        config = load()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaises(ValueError): check(config, root)
            (root / 'fixture').mkdir()
            result = {'mode':'acceptance', 'execution_completed':True, 'exit_code':0}
            (root / 'fixture/result.json').write_text(json.dumps(result))
            receipt = {'run_id':'fixture', 'source_sha256':{'fixture':'hash'},
                       'resources_sha256':fingerprint(config['resources']), 'result_sha256':fingerprint(result)}
            (root / 'accepted.json').write_text(json.dumps(receipt))
            with patch('flow.acceptance.source_hashes', return_value={'fixture':'hash'}):
                check(config, root)
            with patch('flow.acceptance.source_hashes', return_value={'fixture':'changed'}):
                with self.assertRaises(ValueError): check(config, root)
