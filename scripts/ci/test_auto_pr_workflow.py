import unittest
from pathlib import Path


class AutoPRWorkflowTest(unittest.TestCase):
    def test_skips_pr_and_merge_when_branch_tree_has_no_file_changes(self):
        workflow = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8")

        self.assertIn('git diff --quiet "origin/${BASE_BRANCH}...HEAD" --', workflow)
        self.assertIn('echo "has_changes=false" >> "$GITHUB_OUTPUT"', workflow)
        self.assertEqual(
            workflow.count("if: steps.changes.outputs.has_changes == 'true'"),
            2,
        )
        self.assertIn("No file changes relative to", workflow)


if __name__ == "__main__":
    unittest.main()
