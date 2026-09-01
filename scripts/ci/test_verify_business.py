from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts" / "verify-business.sh"


class VerifyBusinessSafetyTests(unittest.TestCase):
    def test_self_test_rejects_unsafe_targets_without_touching_docker(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "docker-called"
            fake_docker = Path(directory) / "docker"
            fake_docker.write_text(f"#!/usr/bin/env bash\ntouch {marker!s}\nexit 99\n", encoding="utf-8")
            fake_docker.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = f"{directory}:{environment['PATH']}"
            result = subprocess.run(
                ["bash", str(SCRIPT), "--self-test"],
                cwd=REPO,
                env=environment,
                text=True,
                capture_output=True,
                check=False,
            )
            docker_was_called = marker.exists()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("6 unsafe targets rejected", result.stdout)
        self.assertFalse(docker_was_called, "safety tests must reject targets before Docker access")

    def test_cleanup_and_destructive_actions_are_scoped(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("^gopulse-acceptance-[a-f0-9]{12}$", source)
        self.assertIn("^gopulse_acceptance_[a-f0-9]{12}$", source)
        self.assertIn("verify_service_ownership redis 6379", source)
        self.assertIn("assert_project_absent", source)
        self.assertLess(source.index("RESOURCES_STARTED=1"), source.index("compose up --detach"))
        self.assertIn("compose down --volumes --remove-orphans", source)
        self.assertIn("trap cleanup EXIT", source)
        self.assertIn("trap 'on_signal 130' INT", source)
        self.assertNotIn("docker volume prune", source)
        self.assertNotIn("docker system prune", source)
        self.assertNotIn("redis-cli FLUSHALL", source)

    def test_script_has_lf_line_endings_and_valid_bash_syntax(self) -> None:
        content = SCRIPT.read_bytes()
        self.assertNotIn(b"\r\n", content)
        subprocess.run(["bash", "-n", str(SCRIPT)], cwd=REPO, check=True)


if __name__ == "__main__":
    unittest.main()
