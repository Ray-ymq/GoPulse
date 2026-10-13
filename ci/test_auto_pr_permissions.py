import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest


def workflow_script(step_name):
    lines = Path(".github/workflows/auto-pr-merge.yml").read_text(encoding="utf-8").splitlines()
    start = next(index for index, line in enumerate(lines) if line.strip() == f"- name: {step_name}")
    run = next(index for index in range(start + 1, len(lines)) if lines[index].strip() == "run: |")
    script = []
    for line in lines[run + 1 :]:
        if line.strip() and not line.startswith("          "):
            break
        script.append(line[10:] if line.strip() else "")
    return "\n".join(script)


class AutoPRPermissionsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.requests = self.root / "requests.jsonl"
        gh = self.root / "gh"
        gh.write_text(
            f"#!{sys.executable}\n"
            + textwrap.dedent("""\
                import json
                import os
                from pathlib import Path
                import sys

                arguments = sys.argv[1:]
                with Path(os.environ["FIXTURE_REQUESTS"]).open("a") as output:
                    output.write(json.dumps(arguments) + "\\n")
                if len(arguments) < 2 or arguments[0] != "api":
                    sys.exit(2)
                resource = next((argument for argument in arguments[1:] if argument == "user" or argument.startswith("repos/")), "")
                if resource == "user":
                    query = arguments[arguments.index("--jq") + 1]
                    print(os.environ["FIXTURE_TOKEN_LOGIN"] if query == ".login" else os.environ["FIXTURE_TOKEN_ID"])
                elif resource.endswith("/merge") and "PUT" in arguments:
                    print('{"merged": true}')
                elif "/git/refs/heads/" in resource and "DELETE" in arguments:
                    pass
                elif resource.endswith("/pulls/249") and "--method" not in arguments:
                    print(os.environ["FIXTURE_PR"])
                else:
                    sys.exit(2)
                """),
            encoding="utf-8",
        )
        gh.chmod(0o755)
        self.pr = {
            "user": {"login": "Ray-ymq", "id": 195848614},
            "state": "open",
            "draft": False,
            "head": {
                "repo": {"full_name": "Ray-ymq/GoPulse"},
                "ref": "re-factor/base",
                "sha": "a" * 40,
            },
            "base": {"repo": {"full_name": "Ray-ymq/GoPulse"}, "ref": "main"},
        }

    def run_step(self, step_name, pr=None, overrides=None):
        self.requests.unlink(missing_ok=True)
        environment = {
            **os.environ,
            "PATH": str(self.root) + os.pathsep + os.environ["PATH"],
            "GH_TOKEN": "fixture-token",
            "GITHUB_ACTOR": "Ray-ymq",
            "GITHUB_ACTOR_ID": "195848614",
            "GITHUB_TRIGGERING_ACTOR": "Ray-ymq",
            "GITHUB_STEP_SUMMARY": str(self.root / "summary.md"),
            "GH_REPO": "Ray-ymq/GoPulse",
            "HEAD_BRANCH": "re-factor/base",
            "HEAD_SHA": "a" * 40,
            "BASE_BRANCH": "main",
            "PR_NUMBER": "249",
            "PR_URL": "https://github.com/Ray-ymq/GoPulse/pull/249",
            "FIXTURE_REQUESTS": str(self.requests),
            "FIXTURE_TOKEN_LOGIN": "Ray-ymq",
            "FIXTURE_TOKEN_ID": "195848614",
            "FIXTURE_PR": json.dumps(self.pr if pr is None else pr),
            **(overrides or {}),
        }
        result = subprocess.run(
            ["bash", "-c", workflow_script(step_name)],
            env=environment,
            text=True,
            capture_output=True,
            timeout=10,
        )
        requests = [json.loads(line) for line in self.requests.read_text().splitlines()] if self.requests.exists() else []
        return result, requests

    def test_owner_identity_is_accepted(self):
        result, requests = self.run_step("Verify pull request identity")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(requests), 2)

    def test_other_pushers_and_rerun_users_are_rejected_before_token_use(self):
        for overrides in (
            {"GITHUB_ACTOR": "contributor", "GITHUB_ACTOR_ID": "123"},
            {"GITHUB_ACTOR_ID": "123"},
            {"GITHUB_TRIGGERING_ACTOR": "contributor"},
        ):
            with self.subTest(overrides=overrides):
                result, requests = self.run_step("Verify pull request identity", overrides=overrides)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(requests, [])

    def test_missing_or_other_user_tokens_are_rejected(self):
        for overrides in (
            {"GH_TOKEN": ""},
            {"FIXTURE_TOKEN_LOGIN": "contributor", "FIXTURE_TOKEN_ID": "123"},
            {"FIXTURE_TOKEN_ID": "123"},
        ):
            with self.subTest(overrides=overrides):
                result, _ = self.run_step("Verify pull request identity", overrides=overrides)
                self.assertNotEqual(result.returncode, 0)

    def test_owner_pr_at_the_tested_revision_can_merge(self):
        result, requests = self.run_step("Merge pull request")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([request[request.index("--method") + 1] for request in requests if "--method" in request], ["PUT", "DELETE"])

    def test_other_authors_stale_heads_and_unexpected_prs_cannot_mutate(self):
        cases = (
            ("other author", lambda pr: pr.update(user={"login": "contributor", "id": 123})),
            ("reused login", lambda pr: pr["user"].update(id=123)),
            ("stale revision", lambda pr: pr["head"].update(sha="b" * 40)),
            ("different head", lambda pr: pr["head"].update(ref="other-branch")),
            ("fork", lambda pr: pr["head"]["repo"].update(full_name="contributor/GoPulse")),
            ("different base", lambda pr: pr["base"].update(ref="release")),
            ("other repository", lambda pr: pr["base"]["repo"].update(full_name="Ray-ymq/Other")),
            ("draft", lambda pr: pr.update(draft=True)),
            ("closed", lambda pr: pr.update(state="closed")),
        )
        for name, change in cases:
            with self.subTest(name=name):
                pr = json.loads(json.dumps(self.pr))
                change(pr)
                result, requests = self.run_step("Merge pull request", pr=pr)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any("--method" in request for request in requests))


if __name__ == "__main__":
    unittest.main()
