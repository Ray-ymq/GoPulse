from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from quality_scope import CHECKS, changed_files, select


class QualityScopeTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        for root in (
            "backend",
            "componentmetrics",
            "router",
            "marshaller",
            "monitor",
            "exporters/redis",
            "exporters/mysql",
            "exporters/rabbitmq",
            "exporters/elasticsearch",
            "exporters/kafka",
            "exporters/victoriametrics",
            "devtools",
        ):
            path = self.repo / root
            path.mkdir(parents=True)
            (path / "go.mod").write_text(
                f"module example/{root}\n\n"
                + ("replace example/componentmetrics => ../componentmetrics\n" if root == "backend" else ""),
                encoding="utf-8",
            )
        (self.repo / "backend/internal").mkdir(parents=True)

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_empty_diff_is_governance_only(self) -> None:
        scope = select(self.repo, [])
        self.assertEqual(scope.changed_files, ())
        self.assertFalse(any(scope.checks.values()))

    def test_documentation_only_does_not_start_product_checks(self) -> None:
        scope = select(self.repo, ["README.md", "dev/validation/local-development-tests.md"])
        self.assertFalse(any(scope.checks.values()))

    def test_native_environment_helper_selects_every_matrix_check(self) -> None:
        scope = select(self.repo, ["devtools/internal/devrun/devrun.go"])
        self.assertTrue(scope.checks["devtools"])
        self.assertTrue(scope.checks["integration_business"])
        self.assertTrue(scope.checks["integration_observe"])
        self.assertTrue(scope.checks["e2e_business"])
        self.assertTrue(scope.checks["e2e_observe"])
        self.assertFalse(scope.checks["backend"])
        self.assertFalse(scope.checks["tools"])

    def test_single_module_selects_only_that_module(self) -> None:
        scope = select(self.repo, ["monitor/internal/plugin/plugin.go"])
        self.assertTrue(scope.checks["monitor"])
        self.assertTrue(scope.checks["integration_observe"])
        self.assertTrue(scope.checks["e2e_observe"])
        self.assertFalse(scope.checks["backend"])
        self.assertFalse(scope.checks["integration_business"])

    def test_componentmetrics_selects_every_local_replace_consumer(self) -> None:
        scope = select(self.repo, ["componentmetrics/metrics.go"])
        self.assertTrue(scope.checks["componentmetrics"])
        self.assertTrue(scope.checks["backend"])
        self.assertFalse(scope.checks["monitor"])

    def test_observability_backend_paths_select_observe_and_admin_browser(self) -> None:
        scope = select(self.repo, ["backend/internal/metricquery/query.go"])
        self.assertTrue(scope.checks["backend"])
        self.assertTrue(scope.checks["integration_business"])
        self.assertTrue(scope.checks["integration_observe"])
        self.assertTrue(scope.checks["e2e_observe"])
        self.assertFalse(scope.checks["e2e_business"])

    def test_frontends_are_selected_separately(self) -> None:
        user = select(self.repo, ["frontend/src/views/Posts.vue"])
        admin = select(self.repo, ["admin-frontend/src/views/Metrics.vue"])
        self.assertTrue(user.checks["frontend"])
        self.assertTrue(user.checks["e2e_business"])
        self.assertFalse(user.checks["admin_frontend"])
        self.assertTrue(admin.checks["admin_frontend"])
        self.assertTrue(admin.checks["e2e_observe"])
        self.assertFalse(admin.checks["e2e_business"])

    def test_deployment_and_tools_have_narrow_boundaries(self) -> None:
        deployment = select(self.repo, ["deploy/docker/backend.Dockerfile"])
        tool = select(self.repo, ["scripts/ci/quality_scope.py"])
        self.assertTrue(deployment.checks["compose"])
        self.assertTrue(deployment.checks["tools"])
        self.assertFalse(deployment.checks["backend"])
        self.assertTrue(tool.checks["tools"])
        self.assertFalse(any(tool.checks[name] for name in CHECKS if name != "tools"))

    def test_unknown_product_path_is_conservative(self) -> None:
        scope = select(self.repo, ["new-product-area/source.go"])
        self.assertTrue(all(scope.checks.values()))

    def test_changed_files_uses_common_ancestor(self) -> None:
        subprocess.run(["git", "init", "--quiet"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.name", "GoPulse CI"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.email", "ci@gopulse.invalid"], cwd=self.repo, check=True)
        (self.repo / "README.md").write_text("base\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "--quiet", "-m", "base"], cwd=self.repo, check=True)
        base = subprocess.run(["git", "rev-parse", "HEAD"], cwd=self.repo, check=True, text=True, capture_output=True).stdout.strip()
        (self.repo / "backend/changed.go").write_text("package changed\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "--quiet", "-m", "change"], cwd=self.repo, check=True)
        self.assertEqual(changed_files(self.repo, base), ["backend/changed.go"])


if __name__ == "__main__":
    unittest.main()
