import re
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"


class WorkflowSafetyTests(unittest.TestCase):
    def test_only_module_ci_is_installed(self):
        workflows = sorted(
            path.relative_to(ROOT).as_posix()
            for path in (ROOT / ".github" / "workflows").glob("*")
            if path.is_file()
        )
        self.assertEqual(workflows, [".github/workflows/ci.yml"])

    def test_only_auto_pr_job_can_write_pull_requests(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")

        self.assertRegex(workflow, r"(?m)^permissions:\n  contents: read$")
        jobs = re.findall(r"(?ms)^  (\w+):\n(.*?)(?=^  \w+:|\Z)", workflow.split("jobs:\n", 1)[1])
        self.assertIn("auto_pr", [name for name, _ in jobs])
        for name, section in jobs:
            if name == "auto_pr":
                self.assertRegex(section, r"(?m)^    permissions:\n      contents: read\n      pull-requests: write$")
                self.assertNotIn("actions/checkout@", section)
                self.assertNotRegex(section, r"(?m)^\s*run:")
                self.assertNotRegex(section, r"(?m)^\s*(?:contents|actions):\s*write\s*$")
            else:
                self.assertNotRegex(section, r"(?m)^\s*\S+:\s*write\s*$", name)
        self.assertNotRegex(workflow, r"\bgit\s+(?:add|commit|push|tag|merge|rebase)\b")
        self.assertNotRegex(workflow, r"create-pull-request|git-auto-commit")

    def test_auto_pr_requires_successful_ci_and_never_writes_to_fork_prs(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        section = re.search(r"(?ms)^  auto_pr:\n(.*?)(?=^  \w+:|\Z)", workflow).group(1)
        self.assertIn("needs: [plan, result]", section)
        self.assertIn("github.event_name == 'push'", section)
        self.assertIn("github.event.deleted != true", section)
        self.assertIn("github.ref_type == 'branch'", section)
        self.assertIn("github.ref_name != github.event.repository.default_branch", section)
        self.assertIn("github.event.pull_request.head.repo.full_name == github.repository", section)
        self.assertIn("CI_LABELS: ${{ needs.plan.outputs.labels }}", section)
        self.assertIn("CI_HEAD_SHA: ${{ needs.plan.outputs.head_sha }}", section)
        self.assertNotIn("always()", section)
        script = section.split("script: |\n", 1)[1]
        self.assertNotIn("${{", script)

    def test_push_runs_on_all_branches_and_skips_branch_deletion(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        self.assertRegex(workflow, r"(?m)^  push:\n    branches: \['\*\*'\]$")
        for job in ("plan", "commits", "result"):
            section = re.search(rf"(?ms)^  {job}:\n(.*?)(?=^  \w+:|\Z)", workflow).group(1)
            self.assertIn("github.event_name != 'push' || github.event.deleted != true", section)

    def test_all_checkouts_disable_persisted_credentials(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        checkout_count = len(re.findall(r"(?m)^\s*- uses: actions/checkout@", workflow))
        disabled_count = len(re.findall(r"(?m)^\s+persist-credentials: false\s*$", workflow))

        self.assertGreater(checkout_count, 0)
        self.assertEqual(disabled_count, checkout_count)


if __name__ == "__main__":
    unittest.main()
