"""核对迁移后的源码、脚本和准备指纹，不访问业务环境。"""
import unittest

from flow.config import ROOT
from flow.environment import source_hashes
from flow.executor import routes


class LayoutTests(unittest.TestCase):
    def test_load_routes_from_the_relocated_javascript_module(self):
        self.assertEqual(len(routes()), 22)

    def test_preparation_fingerprint_covers_relocated_build_inputs(self):
        hashes = source_hashes()
        for relative in (
            'backend/business/cmd/server/main.go',
            'backend/runtimekit/runtime.go',
            'backend/observability/exporters/mysql/collector/diagnostics.go',
            'tools/perflab/flow/runtime.py',
            'deploy/docker/backend.Dockerfile',
        ):
            with self.subTest(path=relative):
                self.assertTrue((ROOT / relative).is_file())
                self.assertIn(relative, hashes)
                self.assertEqual(len(hashes[relative]), 64)
        self.assertTrue((ROOT / '.git').exists())
