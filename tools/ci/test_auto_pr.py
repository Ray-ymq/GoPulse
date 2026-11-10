import json
import os
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
const repositoryLabels = new Set(scenario.repositoryLabels || JSON.parse(process.env.CI_KNOWN_LABELS));
const refSha = scenario.eventName === 'pull_request' ? scenario.prHeadSha || 'a'.repeat(40) : scenario.sha || 'a'.repeat(40);
let heads = 0;
let lists = 0;
const fail = status => { throw Object.assign(new Error(`HTTP ${status}`), {status}); };
const github = {rest: {
  git: {getRef: async args => {
    calls.push(['ref', args]);
    const head = (scenario.heads || [refSha])[heads++];
    if (typeof head === 'number') fail(head);
    return {data: {object: {sha: head || refSha}}};
  }},
  pulls: {
    list: async args => {
      calls.push(['list', args]);
      if (scenario.listError) fail(scenario.listError);
      return {data: ((scenario.prLists || [])[lists++] || []).map(pr => ({number: 1, ...pr}))};
    },
    create: async args => {
      calls.push(['create', args]);
      if (scenario.createError) fail(scenario.createError);
      return {data: {number: 1, html_url: 'https://github.com/ray/GoPulse/pull/1'}};
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
  issues: {
    getLabel: async args => {
      calls.push(['getLabel', args]);
      if (!repositoryLabels.has(args.name)) fail(404);
      return {data: {name: args.name}};
    },
    createLabel: async args => {
      calls.push(['createLabel', args]);
      if (scenario.labelRace) { repositoryLabels.add(args.name); fail(422); }
      if (scenario.createLabelError) fail(scenario.createLabelError);
      repositoryLabels.add(args.name);
      return {data: {name: args.name}};
    },
    listLabelsOnIssue: async args => {
      calls.push(['listLabels', args]);
      return (scenario.currentLabels || []).map(name => ({name}));
    },
    addLabels: async args => {
      calls.push(['addLabels', args]);
      if (scenario.addLabelsError) fail(scenario.addLabelsError);
    },
    removeLabel: async args => {
      calls.push(['removeLabel', args]);
      if (scenario.removeLabelError) fail(scenario.removeLabelError);
    },
  },
}};
github.paginate = async (method, args) => method(args);
const context = {
  repo: {owner: 'ray', repo: 'GoPulse'},
  ref: scenario.ref || 'refs/heads/feat/auto-pr',
  sha: scenario.sha || 'a'.repeat(40),
  eventName: scenario.eventName || 'push',
  payload: {
    deleted: scenario.deleted || false, repository: {default_branch: scenario.base || 'main'},
    pull_request: {
      number: 1, html_url: 'existing', base: {ref: scenario.base || 'main'},
      head: {ref: 'feat/auto-pr', sha: scenario.prHeadSha || 'a'.repeat(40),
             repo: {full_name: scenario.fork ? 'other/GoPulse' : 'ray/GoPulse'}},
    },
  },
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
        environment = dict(os.environ)
        environment.update(
            CI_LABELS=json.dumps(scenario.get("labels", ["ci:backend/business"])),
            CI_KNOWN_LABELS=json.dumps(["ci:backend/business", "ci:backend/runtimekit", "ci:none"]),
            CI_HEAD_SHA=json.dumps(scenario.get("planHeadSha", scenario.get("prHeadSha", SHA) if scenario.get("eventName") == "pull_request" else scenario.get("sha", SHA))),
        )
        result = subprocess.run(
            ["node", "-e", RUNNER], input=json.dumps({"script": script, "scenario": scenario}),
            text=True, capture_output=True, check=True, env=environment,
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
        added = next(args for method, args in result["calls"] if method == "addLabels")
        self.assertEqual(added["labels"], ["ci:backend/business"])

    def test_reuses_existing_pr_and_only_syncs_ci_labels(self):
        result = self.run_script(prLists=[[{"html_url": "existing"}]])
        self.assertIsNone(result["error"])
        methods = [method for method, _ in result["calls"]]
        self.assertNotIn("create", methods)
        self.assertNotIn("compare", methods)
        self.assertIn("addLabels", methods)
        args = result["calls"][1][1]
        self.assertEqual((args["state"], args["head"], args["base"]), ("open", "ray:feat/auto-pr", "main"))

    def test_default_branch_tags_deletions_and_non_push_events_do_nothing(self):
        for scenario in (
            {"ref": "refs/heads/main"}, {"ref": "refs/tags/v1"}, {"deleted": True},
            {"eventName": "pull_request", "fork": True}, {"eventName": "workflow_dispatch"},
        ):
            with self.subTest(scenario=scenario):
                result = self.run_script(**scenario)
                self.assertIsNone(result["error"])
                self.assertEqual(result["calls"], [])

    def test_internal_pr_syncs_labels_using_pr_head_instead_of_merge_sha(self):
        result = self.run_script(eventName="pull_request", sha="c" * 40)
        self.assertIsNone(result["error"])
        methods = [method for method, _ in result["calls"]]
        self.assertNotIn("create", methods)
        self.assertNotIn("compare", methods)
        self.assertIn("addLabels", methods)

    def test_sync_replaces_stale_ci_labels_and_preserves_other_labels(self):
        result = self.run_script(
            prLists=[[{"html_url": "existing"}]],
            currentLabels=["bug", "ci:none", "ci:removed-module"],
            labels=["ci:backend/business", "ci:backend/runtimekit"],
        )
        self.assertIsNone(result["error"])
        added = next(args for method, args in result["calls"] if method == "addLabels")
        self.assertEqual(added["labels"], ["ci:backend/business", "ci:backend/runtimekit"])
        removed = [args["name"] for method, args in result["calls"] if method == "removeLabel"]
        self.assertEqual(removed, ["ci:none", "ci:removed-module"])

    def test_documentation_plan_removes_modules_and_adds_none(self):
        result = self.run_script(labels=["ci:none"], currentLabels=["ci:backend/business", "documentation"])
        self.assertIsNone(result["error"])
        added = next(args for method, args in result["calls"] if method == "addLabels")
        self.assertEqual(added["labels"], ["ci:none"])
        removed = [args["name"] for method, args in result["calls"] if method == "removeLabel"]
        self.assertEqual(removed, ["ci:backend/business"])

    def test_unchanged_labels_are_not_written_again(self):
        result = self.run_script(currentLabels=["ci:backend/business", "bug"])
        self.assertIsNone(result["error"])
        self.assertFalse({"addLabels", "removeLabel"} & {method for method, _ in result["calls"]})

    def test_missing_repository_labels_are_created_and_races_are_verified(self):
        for race in (False, True):
            with self.subTest(race=race):
                result = self.run_script(repositoryLabels=[], labelRace=race)
                self.assertIsNone(result["error"])
                self.assertIn("createLabel", [method for method, _ in result["calls"]])
                if race:
                    self.assertEqual([method for method, _ in result["calls"]].count("getLabel"), 2)

    def test_invalid_or_stale_label_plan_fails_before_any_api_call(self):
        for scenario in ({"labels": []}, {"labels": ["ci:unknown"]}, {"labels": ["ci:none", "ci:backend/business"]}, {"planHeadSha": "b" * 40}):
            with self.subTest(scenario=scenario):
                result = self.run_script(**scenario)
                self.assertIsNotNone(result["error"])
                self.assertEqual(result["calls"], [])

    def test_label_errors_are_not_reported_as_success(self):
        for scenario in ({"addLabelsError": 403}, {"repositoryLabels": [], "createLabelError": 422}):
            with self.subTest(scenario=scenario):
                result = self.run_script(**scenario)
                self.assertIsNotNone(result["error"])

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
