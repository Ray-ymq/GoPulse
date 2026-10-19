import re
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"


class WorkflowSafetyTests(unittest.TestCase):
    def test_only_read_only_module_ci_is_installed(self):
        workflows = sorted(
            path.relative_to(ROOT).as_posix()
            for path in (ROOT / ".github" / "workflows").glob("*")
            if path.is_file()
        )
        self.assertEqual(workflows, [".github/workflows/ci.yml"])

    def test_module_ci_cannot_mutate_repository(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")

        self.assertRegex(workflow, r"(?m)^\s*permissions:\s*$")
        self.assertRegex(workflow, r"(?m)^\s*contents:\s*read\s*$")
        self.assertNotRegex(workflow, r"(?m)^\s*(?:contents|pull-requests|actions):\s*write\s*$")
        self.assertNotRegex(workflow, r"(?m)^\s*push:\s*$")
        self.assertNotRegex(workflow, r"\bgit\s+(?:add|commit|push|tag|merge|rebase)\b")
        self.assertNotRegex(workflow, r"create-pull-request|git-auto-commit")

    def test_all_checkouts_disable_persisted_credentials(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        checkout_count = len(re.findall(r"(?m)^\s*- uses: actions/checkout@", workflow))
        disabled_count = len(re.findall(r"(?m)^\s+persist-credentials: false\s*$", workflow))

        self.assertGreater(checkout_count, 0)
        self.assertEqual(disabled_count, checkout_count)


if __name__ == "__main__":
    unittest.main()
