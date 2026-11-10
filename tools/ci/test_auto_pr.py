import json
from pathlib import Path
import subprocess
import textwrap
import unittest

from commits import read_policy, validate_pr_body, validate_title


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"
SHA = "a" * 40
RUNNER = r"""
const fs = require('node:fs');
const {script, scenario} = JSON.parse(fs.readFileSync(0, 'utf8'));
const calls = [];
const logs = [];
let heads = 0;
let lists = 0;
const fail = status => { throw Object.assign(new Error(`HTTP ${status}`), {status}); };
const github = {rest: {
  git: {getRef: async args => {
    calls.push(['ref', args]);
    const head = (scenario.heads || [scenario.sha || 'a'.repeat(40)])[heads++];
    if (typeof head === 'number') fail(head);
    return {data: {object: {sha: head || scenario.sha || 'a'.repeat(40)}}};
  }},
  pulls: {
    list: async args => {
      calls.push(['list', args]);
      if (scenario.listError) fail(scenario.listError);
      return {data: (scenario.prLists || [])[lists++] || []};
    },
    create: async args => {
      calls.push(['create', args]);
      if (scenario.createError) fail(scenario.createError);
      return {data: {html_url: 'https://github.com/ray/GoPulse/pull/1'}};
    },
  },
  repos: {compareCommitsWithBasehead: async args => {
    calls.push(['compare', args]);
    return {data: {
      ahead_by: scenario.ahead ?? 1,
      files: scenario.files ?? [{filename: 'backend/business/main.go'}],
      commits: [{commit: {message: 'feat(ci): 支持自动 PR\n\n背景：已有 CI。'}}],
    }};
  }},
}};
const context = {
  repo: {owner: 'ray', repo: 'GoPulse'},
  ref: scenario.ref || 'refs/heads/feat/auto-pr',
  sha: scenario.sha || 'a'.repeat(40),
  eventName: scenario.eventName || 'push',
  payload: {deleted: scenario.deleted || false, repository: {default_branch: scenario.base || 'main'}},
  serverUrl: 'https://github.com', runId: 123,
};
const core = {info: message => logs.push(message), error: message => logs.push(message)};
(async () => {
  let error = null;
  try {
    await new (Object.getPrototypeOf(async () => {}).constructor)('github', 'context', 'core', script)(github, context, core);
  } catch (caught) { error = caught.message; }
  process.stdout.write(JSON.stringify({calls, logs, error}));
})();
"""


class AutoPRTests(unittest.TestCase):
    def run_script(self, **scenario):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        script = textwrap.dedent(workflow.split("          script: |\n", 1)[1])
        result = subprocess.run(
            ["node", "-e", RUNNER], input=json.dumps({"script": script, "scenario": scenario}),
            text=True, capture_output=True, check=True,
        )
        return json.loads(result.stdout)

    def test_creates_valid_pr_with_actual_push_ci_evidence(self):
        result = self.run_script()
        self.assertIsNone(result["error"])
        created = [args for method, args in result["calls"] if method == "create"]
        self.assertEqual(len(created), 1)
        pr = created[0]
        self.assertEqual((pr["base"], pr["head"]), ("main", "feat/auto-pr"))
        self.assertEqual(validate_title(pr["title"], read_policy(ROOT)), [])
        self.assertEqual(validate_pr_body(pr["body"]), [])
        self.assertIn("feat(ci): 支持自动 PR", pr["body"])
        self.assertNotIn("背景：已有 CI。", pr["body"])
        self.assertIn("https://github.com/ray/GoPulse/actions/runs/123", pr["body"])
        self.assertIn("本次 push 选中的模块", pr["body"])
        self.assertNotIn("labels", pr)
        comparison = next(args for method, args in result["calls"] if method == "compare")
        self.assertEqual(comparison["basehead"], f"main...{SHA}")

    def test_reuses_existing_pr_without_mutating_its_metadata(self):
        result = self.run_script(prLists=[[{"html_url": "existing"}]])
        self.assertIsNone(result["error"])
        self.assertEqual([method for method, _ in result["calls"]], ["ref", "list"])
        args = result["calls"][1][1]
        self.assertEqual((args["state"], args["head"], args["base"]), ("open", "ray:feat/auto-pr", "main"))

    def test_default_branch_tags_deletions_and_non_push_events_do_nothing(self):
        for scenario in (
            {"ref": "refs/heads/main"}, {"ref": "refs/tags/v1"}, {"deleted": True},
            {"eventName": "pull_request"}, {"eventName": "workflow_dispatch"},
        ):
            with self.subTest(scenario=scenario):
                result = self.run_script(**scenario)
                self.assertIsNone(result["error"])
                self.assertEqual(result["calls"], [])

    def test_deleted_or_superseded_branch_is_skipped(self):
        for head in (404, "b" * 40):
            with self.subTest(head=head):
                result = self.run_script(heads=[head])
                self.assertIsNone(result["error"])
                self.assertEqual([method for method, _ in result["calls"]], ["ref"])

    def test_no_ahead_commits_or_no_file_changes_is_skipped(self):
        for scenario in ({"ahead": 0}, {"files": []}):
            with self.subTest(scenario=scenario):
                result = self.run_script(**scenario)
                self.assertIsNone(result["error"])
                self.assertNotIn("create", [method for method, _ in result["calls"]])

    def test_branch_changes_during_comparison_are_skipped(self):
        for head in (404, "b" * 40):
            with self.subTest(head=head):
                result = self.run_script(heads=[SHA, head])
                self.assertIsNone(result["error"])
                self.assertNotIn("create", [method for method, _ in result["calls"]])

    def test_concurrently_created_pr_is_reused(self):
        result = self.run_script(createError=422, prLists=[[], [{"html_url": "raced"}]])
        self.assertIsNone(result["error"])
        self.assertIn("复用并发创建的 PR：raced", result["logs"])

    def test_validation_errors_are_not_mistaken_for_duplicates(self):
        result = self.run_script(createError=422)
        self.assertEqual(result["error"], "HTTP 422")

    def test_permission_errors_fail_with_configuration_guidance(self):
        result = self.run_script(createError=403)
        self.assertEqual(result["error"], "HTTP 403")
        self.assertIn("Allow GitHub Actions to create and approve pull requests", "\n".join(result["logs"]))

    def test_api_lookup_failures_are_not_treated_as_no_pr(self):
        result = self.run_script(listError=500)
        self.assertEqual(result["error"], "HTTP 500")
        self.assertNotIn("create", [method for method, _ in result["calls"]])

    def test_uses_repository_default_branch_instead_of_hardcoding_main(self):
        result = self.run_script(base="develop")
        self.assertIsNone(result["error"])
        created = next(args for method, args in result["calls"] if method == "create")
        self.assertEqual(created["base"], "develop")

    def test_branch_names_are_passed_as_api_data_and_title_is_bounded(self):
        branch = "feat/$(id)-`id`-" + "x" * 240
        result = self.run_script(ref=f"refs/heads/{branch}")
        self.assertIsNone(result["error"])
        created = next(args for method, args in result["calls"] if method == "create")
        self.assertEqual(created["head"], branch)
        self.assertLessEqual(len(created["title"]), 240)
        self.assertEqual(validate_title(created["title"], read_policy(ROOT)), [])
        self.assertIn(r"\`id\`", created["body"])


if __name__ == "__main__":
    unittest.main()
