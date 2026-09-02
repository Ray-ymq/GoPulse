import unittest
from pathlib import Path


class AutoPRWorkflowTest(unittest.TestCase):
    def test_skips_pr_and_merge_when_branch_tree_has_no_file_changes(self):
        workflow = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8")

        self.assertIn('git diff --quiet "origin/${BASE_BRANCH}...HEAD" --', workflow)
        self.assertIn('echo "has_changes=false" >> "$GITHUB_OUTPUT"', workflow)
        self.assertEqual(
            workflow.count("if: steps.changes.outputs.has_changes == 'true'"),
            3,
        )
        self.assertIn("No file changes relative to", workflow)

    def test_waits_for_pull_request_ci_before_merge_can_delete_branch(self):
        workflow = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8")

        wait_position = workflow.index("- name: Wait for pull-request CI")
        merge_position = workflow.index("- name: Enable auto-merge")
        self.assertLess(wait_position, merge_position)
        self.assertIn("actions: read", workflow)
        self.assertIn("--workflow ci.yml", workflow)
        self.assertIn("--event pull_request", workflow)
        self.assertIn('--commit "$HEAD_SHA"', workflow)
        self.assertIn('gh run watch "$ci_run_id"', workflow)
        self.assertIn("--exit-status", workflow)


if __name__ == "__main__":
    unittest.main()
