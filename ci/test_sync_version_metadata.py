from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

try:
    from sync_version_metadata import VersionMetadataError, sync
except ModuleNotFoundError:
    from scripts.ci.sync_version_metadata import VersionMetadataError, sync


class SyncVersionMetadataTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        (self.repo / "VERSION").write_text("1.13.3\n", encoding="utf-8")
        (self.repo / ".env.example").write_text(
            "GOPULSE_VERSION=1.13.3\nGOPULSE_IMAGE_TAG=1.13.3\n", encoding="utf-8"
        )
        for relative, name in (
            ("frontend/package.json", "gopulse-frontend"),
            ("frontend/package-lock.json", "gopulse-frontend"),
            ("admin-frontend/package.json", "gopulse-admin-frontend"),
            ("admin-frontend/package-lock.json", "gopulse-admin-frontend"),
        ):
            path = self.repo / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            if path.name == "package-lock.json":
                document = {"name": name, "version": "1.13.3", "packages": {"": {"version": "1.13.3"}}}
            else:
                document = {"name": name, "private": True, "version": "1.13.3"}
            path.write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_sync_updates_every_product_version_source(self) -> None:
        changed = sync(self.repo, "1.13.4")
        self.assertEqual(
            {str(path) for path in changed},
            {
                "VERSION",
                ".env.example",
                "frontend/package.json",
                "frontend/package-lock.json",
                "admin-frontend/package.json",
                "admin-frontend/package-lock.json",
            },
        )
        self.assertEqual((self.repo / "VERSION").read_text(encoding="utf-8"), "1.13.4\n")
        self.assertIn("GOPULSE_IMAGE_TAG=1.13.4", (self.repo / ".env.example").read_text(encoding="utf-8"))
        for relative in (
            "frontend/package.json",
            "frontend/package-lock.json",
            "admin-frontend/package.json",
            "admin-frontend/package-lock.json",
        ):
            document = json.loads((self.repo / relative).read_text(encoding="utf-8"))
            self.assertEqual(document["version"], "1.13.4")
            if relative.endswith("package-lock.json"):
                self.assertEqual(document["packages"][""]["version"], "1.13.4")
        self.assertEqual(sync(self.repo, "1.13.4"), [])

    def test_sync_rejects_invalid_version_without_partial_update(self) -> None:
        with self.assertRaises(VersionMetadataError):
            sync(self.repo, "1.13")
        self.assertEqual((self.repo / "VERSION").read_text(encoding="utf-8"), "1.13.3\n")

    def test_sync_rejects_missing_required_env_key(self) -> None:
        (self.repo / ".env.example").write_text("GOPULSE_VERSION=1.13.3\n", encoding="utf-8")
        with self.assertRaises(VersionMetadataError):
            sync(self.repo, "1.13.4")


if __name__ == "__main__":
    unittest.main()
