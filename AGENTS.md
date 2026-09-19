# Automatic Commit Rule

- After every task that adds, deletes, or modifies project files, create a Git commit before reporting completion.
- Stage and commit only files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write commit messages in English and use Conventional Commits when practical.
- Run appropriate checks before committing. If checks cannot be run or fail, report that clearly.
- Do not create an empty commit when no project files were changed.

# Version and Branch Lifecycle Rule

- Use the three-part `major.minor.patch` version format.
- Phase 0 through Phase 3 use the following pre-1.0 version lines:
  - Phase 0: `0.1.x`
  - Phase 1: `0.2.x`
  - Phase 2: `0.3.x`
  - Phase 3: `0.4.x`
- After Phase 3 passes milestone acceptance, publish `1.0.0`.
- Phase 4 through Phase 20 use `1.x.x`, where the minor version is `Phase - 3`.
- Reserve patch `0` as the Phase baseline. Number executable development batches from patch `1` in execution order.
- Each executable development batch must have one target version and one branch named `develop/x.x.x`. All commits in that batch share the target version.
- The Phase total implementation plan at `dev/imple/Phase-XX/Phase-XX-总实施方案.md` is the authoritative source for batch order, target versions, and branch allocation.
- If an unstarted batch split or order changes, update the total implementation plan and recalculate affected branches. Never silently rename or renumber a branch that has already been pushed.
- Before starting an independent development task:
  1. Fetch the configured primary remote.
  2. Determine the target version.
  3. Create `develop/x.x.x` from the remote `main` branch.
- Continue using an existing development branch only for follow-up work belonging to the same task or pull request.
- Do not automatically continue using a development branch after its task is complete or its pull request has been opened.
- Before pushing, verify that the branch is either `develop/x.x.x` or exactly `update`.
- The `update` branch is a long-lived planning branch. It may contain only planning, architecture adjustments, implementation plans, documentation organization, planning metadata, and repository-rule maintenance.
- Do not use `update` for feature implementation, application testing, or ordinary development pull requests.
- Pull requests from `update` to `main` use merge commits and must not delete `update`. Ordinary `develop/x.x.x` pull requests may use squash merge and delete their branches.
- The root `VERSION` file is the sole source of the current completed product version. If it does not exist before the first batch, create it with the Phase 0 baseline `0.1.0`.
- At development-batch completion, update `VERSION` to the batch target version and include it in the batch commit.
- Planning, documentation, tests-only, formatting, and repository-rule work performed on `update` do not change `VERSION`.

# Implementation and Validation Rule

- Every Phase total implementation plan must define:
  - Phase-level acceptance criteria.
  - Required end-to-end capabilities.
  - Cross-batch integration results.
  - Milestone completion conditions.
- Every split implementation plan must define:
  - Batch acceptance criteria.
  - Concrete validation items or commands where known.
  - Required regression scope.
  - An explicit completion condition.
- Treat the implementation plan as an acceptance contract, not as permission for a general code audit, dependency audit, coverage campaign, or speculative hardening pass.
- For Phase-02-03 and later implementation tasks, spend no more than 10 minutes on initial discovery before making the first in-scope implementation change.
- During initial discovery, inspect only directly affected project code, tests, and public interfaces.
- Inspect third-party source only when a concrete compiler error, runtime failure, or required failing test cannot be resolved from the local call site, public API, documentation, and reported error. Limit inspection to the smallest relevant symbol and record the reason.
- Add or change tests only when they:
  - Prove a new or changed acceptance criterion.
  - Reproduce an observed defect.
  - Protect a changed security boundary, persistent-data invariant, or public contract.
- Do not add tests solely to increase coverage, enumerate hypothetical combinations, verify unchanged library behavior, or duplicate one behavior across multiple test layers.
- For one changed state transition, prefer one representative success case and one representative failure case at the lowest effective test layer. Add cases only for distinct required business outcomes or an observed failure.
- Run validation in stages:
  1. Run the smallest affected-package check during implementation.
  2. Run the implementation plan's fixed completion gates once against the final candidate.
- Expand validation only for a recorded concrete risk, such as:
  - Shared infrastructure changes.
  - Security-boundary changes.
  - Persistent-data changes.
  - Public-contract changes.
  - Evidence of a regression.
- Do not repeat a successful check unless relevant code, configuration, dependencies, candidate metadata, or the execution environment changed.
- Conversation context compaction does not invalidate successful checks. Reconstruct progress from the plan, Git diff, implementation log, and captured command results.
- If optional investigation or optional test work consumes 15 consecutive minutes without resolving a required failure or advancing production implementation, stop it and return to the shortest in-scope path. Record optional improvements as follow-up items.
- Do not make standalone code reviews, architecture reviews, severity classifications, or separate review reports default development gates. Perform them only when explicitly requested.
- Stop when the documented acceptance criteria and fixed completion gates pass with no blocking issue. Do not continue with opportunistic refactors, extra edge cases, or unrelated cleanup.

# Acceptance Candidate Rule

- Before starting an expensive end-to-end acceptance matrix, run a deterministic preflight against the exact candidate manifest, Bundle, runtime contract, release receipt, and evidence verifier.
- The preflight must validate:
  - Manifest, Bundle, runtime-contract, revision, and receipt binding.
  - Evidence attachment paths and hashes.
  - Schema and redaction rules.
  - Runtime-contract environment templates.
  - Docker resource-isolation snapshots on the actual acceptance host.
- Treat acceptance runners, evidence aggregators, secret scanners, receipt copiers, and cleanup logic as testable infrastructure.
- Add focused unit or fixture coverage for known acceptance-infrastructure failure modes before launching the product matrix.
- Do not start the full matrix when deterministic preflight can detect the failure.
- Once a candidate is frozen, changes to application code, acceptance code, evidence verification, release metadata, or runtime-contract handling invalidate its receipts and require a new candidate revision.
- Do not repair a failed candidate by replacing tags, editing receipts, or reusing evidence from another revision.
- Separate product failures from acceptance-infrastructure failures. Record the category and fix the smallest directly affected layer before rerunning the affected gate.
- Resource-isolation checks must distinguish owned resources, foreign resources, stopped resources, and disposable anonymous test volumes.
- Never use broad cleanup or global prune to make an isolation check pass.
- After the final matrix passes, run the evidence verifier against the exact sanitized evidence selected for publication.

# Implementation Log Rule

- After completing an implementation plan under `dev/imple/Phase-XX/`, create or update its corresponding record under `dev/logs/Phase-XX/`.
- Mirror the plan's Phase directory and Markdown filename.
- Each record must contain:
  - Work actually completed.
  - Files actually changed.
  - Validation commands actually executed and their results.
  - Deviations from the plan.
  - Known limitations and follow-up items.
- Never record planned or unverified work as completed.
- Complete the implementation log before updating the final version and creating the completion commit.

# Active Platform Rule

- The maintained product implementation and acceptance environment is Linux `amd64` with Bash.
- Kubernetes is a deployment environment, not a prerequisite for proving that GoPulse's business and observability systems work.
- Phase 18 through Phase 20 use WSL2/Linux as the primary Kubernetes implementation, application-testing, and integration-acceptance environment.
- Keep the active Phase 18–20 checkout in the WSL Linux filesystem, such as `/home/<user>/src/GoPulse`, rather than under `/mnt/c`, `/mnt/d`, or another Windows-mounted filesystem.
- Use only one Docker daemon for the active workspace.
- Preserve the Linux `amd64` Compose product, lifecycle, release, backup, and upgrade contracts delivered by Phase 16 wherever later work directly affects them.
- Existing `scripts/*.ps1` files are frozen at the `0.2.1` capability baseline and are historical artifacts. Do not extend them with current Compose or Kubernetes behavior.
