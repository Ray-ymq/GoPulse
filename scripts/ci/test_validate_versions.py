from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from validate_versions import validate


class VersionMetadataTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        frontend = self.repo / "frontend"
        frontend.mkdir()
        (self.repo / "VERSION").write_text("0.2.7\n", encoding="utf-8")
        (self.repo / ".env.example").write_text("GOPULSE_VERSION=0.2.7\n", encoding="utf-8")
        (frontend / "package.json").write_text(json.dumps({"version": "0.2.7"}), encoding="utf-8")
        (frontend / "package-lock.json").write_text(
            json.dumps({"version": "0.2.7", "packages": {"": {"version": "0.2.7"}}}),
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_accepts_matching_product_metadata(self) -> None:
        self.assertEqual(validate(self.repo), [])

    def test_rejects_frontend_package_drift(self) -> None:
        package = self.repo / "frontend/package.json"
        package.write_text(json.dumps({"version": "0.2.6"}), encoding="utf-8")
        self.assertIn("frontend package version", validate(self.repo)[0])

    def test_rejects_lockfile_root_package_drift(self) -> None:
        lockfile = self.repo / "frontend/package-lock.json"
        lockfile.write_text(
            json.dumps({"version": "0.2.7", "packages": {"": {"version": "0.2.6"}}}),
            encoding="utf-8",
        )
        self.assertIn("lockfile root package version", validate(self.repo)[0])

    def test_rejects_compose_environment_version_drift(self) -> None:
        (self.repo / ".env.example").write_text("GOPULSE_VERSION=0.2.6\n", encoding="utf-8")
        self.assertIn(".env.example GOPULSE_VERSION", validate(self.repo)[0])

    def test_rejects_non_semver_root_version(self) -> None:
        (self.repo / "VERSION").write_text("0.2\n", encoding="utf-8")
        self.assertIn("major.minor.patch", validate(self.repo)[0])


if __name__ == "__main__":
    unittest.main()
