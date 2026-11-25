from __future__ import annotations

from pathlib import Path
import unittest

from modules import go_manifests, read_modules


ROOT = Path(__file__).resolve().parents[2]


class RepositoryLayoutTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.modules = read_modules(ROOT)
        cls.manifests = go_manifests(ROOT, cls.modules)

    def test_registry_covers_all_backend_and_tool_modules(self) -> None:
        discovered = {
            path.parent.relative_to(ROOT).as_posix()
            for domain in ("backend", "tools")
            for path in (ROOT / domain).rglob("go.mod")
        }
        self.assertEqual({module.name for module in self.modules}, discovered)

    def test_local_replacements_resolve_to_the_declared_modules(self) -> None:
        for directory, manifest in self.manifests.items():
            for replacement in manifest.get("Replace") or []:
                target = replacement["New"]
                if target.get("Version"):
                    continue
                with self.subTest(module=directory, replacement=target["Path"]):
                    resolved = (ROOT / directory / target["Path"]).resolve()
                    relative = resolved.relative_to(ROOT).as_posix()
                    self.assertEqual(
                        self.manifests[relative]["Module"]["Path"],
                        replacement["Old"]["Path"],
                    )

if __name__ == "__main__":
    unittest.main()
