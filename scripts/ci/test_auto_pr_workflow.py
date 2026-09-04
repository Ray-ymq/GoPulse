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

    def test_uses_push_gates_without_waiting_for_bot_created_pull_request_ci(self):
        workflow = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8")

        create_position = workflow.index("- name: Create or find pull request")
        merge_position = workflow.index("- name: Enable auto-merge")
        self.assertLess(create_position, merge_position)
        self.assertNotIn("Wait for pull-request CI", workflow)
        self.assertNotIn("gh run watch", workflow)
        self.assertNotIn("actions: read", workflow)
        self.assertFalse(Path(".github/workflows/ci.yml").exists())

    def test_update_push_runs_governance_only(self):
        caller = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8")
        gates = Path(".github/workflows/quality-gates.yml").read_text(encoding="utf-8")

        self.assertIn("run_product_checks: ${{ github.ref_name != 'update' }}", caller)
        self.assertIn("run_product_checks:", gates)
        self.assertIn("default: true", gates)
        self.assertEqual(gates.count("if: inputs.run_product_checks"), 9)

    def test_integration_migration_retries_transient_mysql_startup(self):
        gates = Path(".github/workflows/quality-gates.yml").read_text(encoding="utf-8")

        migration = gates[gates.index("- name: Apply upward migrations to isolated database") :]
        migration = migration[: migration.index("- name: Run integration tests against required dependencies")]
        self.assertIn("max_attempts=5", migration)
        self.assertIn("if go run ./cmd/migrate up; then", migration)
        self.assertIn('if [[ "$attempt" -eq "$max_attempts" ]]', migration)
        self.assertIn("sleep 3", migration)


if __name__ == "__main__":
    unittest.main()
