"""Exercise cache adaptation, cold fallback, and build failure propagation."""

import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import compose_build_cache as cache


class ComposeBuildCacheTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        (self.repo / "VERSION").write_text("2.3.1\n")
        self.configs = []
        self.paths = []
        self.build_results = [(0, "", "")]
        self.rendered_env = None

    def run_command(self, command, **kwargs):
        if command[:2] == ["git", "rev-parse"]:
            return subprocess.CompletedProcess(command, 0, "a" * 40 + "\n", "")
        if command[:2] == ["docker", "compose"]:
            self.rendered_env = kwargs["env"]
            version = self.rendered_env["GOPULSE_VERSION"]
            targets = {
                name: {
                    "context": str(self.repo), "dockerfile": "deploy/docker/backend.Dockerfile",
                    "args": {"VERSION": version, "REVISION": self.rendered_env["GOPULSE_REVISION"],
                             "UPDATE_VERSION": self.rendered_env["GOPULSE_UPDATE_VERSION"]},
                    "target": name, "tags": [f"gopulse/{name}:{version}"],
                } for name in cache.TARGETS
            }
            targets["backend-2"] = dict(targets["backend"])
            return subprocess.CompletedProcess(command, 0, json.dumps({"target": targets}), "")
        self.assertEqual(command[:3], ["docker", "buildx", "bake"])
        self.assertIn("--load", command)
        path = Path(command[command.index("--file") + 1])
        self.paths.append(path)
        self.configs.append(json.loads(path.read_text()))
        status, stdout, stderr = self.build_results.pop(0)
        return subprocess.CompletedProcess(command, status, stdout, stderr)

    def execute(self, environment=None, **kwargs):
        with patch.dict(os.environ, environment or {}, clear=True), \
             patch.object(cache.subprocess, "run", side_effect=self.run_command), \
             contextlib.redirect_stdout(io.StringIO()), \
             contextlib.redirect_stderr(io.StringIO()):
            return cache.build(self.repo, **kwargs)

    def github_environment(self):
        return {"ACTIONS_RUNTIME_TOKEN": "private-token",
                "ACTIONS_RESULTS_URL": "https://cache.example.invalid"}

    def test_missing_cache_still_builds_and_cleans_temporary_configuration(self):
        self.assertEqual(self.execute(), 0)
        self.assertEqual(len(self.configs), 1)
        self.assertNotIn("cache-from", self.configs[0]["target"]["backend"])
        self.assertTrue(all(not path.exists() for path in self.paths))

    def test_cache_configuration_keeps_current_identity_and_separates_images(self):
        self.assertEqual(self.execute(self.github_environment()), 0)
        config = self.configs[0]
        self.assertEqual(set(config["target"]), set(cache.TARGETS))
        self.assertEqual(config["group"]["default"]["targets"], list(cache.TARGETS))
        scopes = []
        for name, target in config["target"].items():
            self.assertEqual(target["args"]["VERSION"], "2.3.1")
            self.assertEqual(target["args"]["REVISION"], "a" * 40)
            self.assertEqual(target["args"]["UPDATE_VERSION"], "2.3.2")
            self.assertEqual(target["tags"], [f"gopulse/{name}:2.3.1"])
            self.assertEqual(target["platforms"], ["linux/amd64"])
            self.assertEqual(target["output"], ["type=docker"])
            self.assertIn("ignore-error=true", target["cache-to"][0])
            scopes.append(target["cache-from"][0])
        self.assertEqual(len(set(scopes)), len(cache.TARGETS))
        self.assertNotIn("private-token", json.dumps(config))

    def test_cache_import_failure_retries_once_without_remote_cache(self):
        self.build_results = [(1, "", "ERROR: failed to solve: failed to configure gha cache: HTTP 503"),
                              (0, "", "")]
        self.assertEqual(self.execute(self.github_environment()), 0)
        self.assertEqual(len(self.configs), 2)
        before, after = self.configs
        for name in cache.TARGETS:
            expected = dict(before["target"][name])
            expected.pop("cache-from")
            expected.pop("cache-to")
            self.assertEqual(after["target"][name], expected)
        self.assertTrue(all(not path.exists() for path in self.paths))

    def test_cold_retry_failure_is_propagated_without_a_third_attempt(self):
        self.build_results = [(1, "", "ERROR: failed to solve: failed to import cache manifest"),
                              (23, "", "compiler failed")]
        self.assertEqual(self.execute(self.github_environment()), 23)
        self.assertEqual(len(self.configs), 2)

    def test_compiler_failure_is_not_retried_after_a_cache_warning(self):
        self.build_results = [(17, "warning: failed to import cache manifest\n",
                               "ERROR: failed to solve: go build exited with code 1")]
        self.assertEqual(self.execute(self.github_environment()), 17)
        self.assertEqual(len(self.configs), 1)
        self.assertTrue(all(not path.exists() for path in self.paths))

    def test_explicit_no_cache_build_ignores_available_runtime(self):
        self.assertEqual(self.execute(self.github_environment(), no_cache=True), 0)
        self.assertNotIn("cache-from", self.configs[0]["target"]["backend"])

    def test_print_only_never_starts_a_build(self):
        self.assertEqual(self.execute(self.github_environment(), print_only=True), 0)
        self.assertEqual(self.configs, [])


if __name__ == "__main__":
    unittest.main()
