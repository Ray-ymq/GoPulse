from __future__ import annotations

import json
from pathlib import Path
import shlex
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

    def test_runtime_contract_source_paths_exist(self) -> None:
        contract = json.loads((ROOT / "deploy/runtime-contracts.json").read_text())

        def check(value):
            if isinstance(value, list):
                for item in value:
                    check(item)
            elif isinstance(value, dict):
                for key, item in value.items():
                    if key in {"entrypoint", "configuration", "catalog", "validator"}:
                        with self.subTest(field=key, path=item):
                            self.assertTrue((ROOT / item).exists(), f"源码路径不存在：{item}")
                    elif key == "module":
                        self.assertIn(item, self.manifests)
                    else:
                        check(item)

        check(contract)

    def test_docker_copy_sources_exist_in_the_checkout(self) -> None:
        for dockerfile in (ROOT / "deploy/docker").glob("*.Dockerfile"):
            for line in dockerfile.read_text().splitlines():
                if not line.lstrip().upper().startswith("COPY "):
                    continue
                fields = shlex.split(line)
                if any(field.startswith("--from=") for field in fields):
                    continue
                sources = [field for field in fields[1:-1] if not field.startswith("--")]
                for source in sources:
                    with self.subTest(dockerfile=dockerfile.name, source=source):
                        self.assertTrue((ROOT / source).exists(), f"Docker 构建输入不存在：{source}")


if __name__ == "__main__":
    unittest.main()
