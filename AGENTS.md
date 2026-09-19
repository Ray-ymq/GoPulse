# Task Classification, Version, and Branch Rule

- Read-only analysis, diagnosis, review, and status reporting do not require a new branch or commit.
- Planning, architecture, implementation-plan, documentation-organization, planning-metadata, and repository-rule work may be performed directly on `update`.
- Changes unique to `update` relative to the latest primary-remote `main` must remain within that planning scope. Merging `main` into `update` does not violate this rule.
- Do not use `update` for product implementation, test implementation, application validation, or ordinary development pull requests.
- Every executable product-development change must belong to a documented implementation batch.
- Before starting a new implementation batch:
  1. Fetch the configured primary remote.
  2. Read the Phase total implementation plan.
  3. Determine the batch target version.
  4. Create the allocated branch from the primary remote's `main`.
- Development branches must match `^develop/[0-9]+\.[0-9]+\.[0-9]+$`.
- Continue using the current development branch only for work belonging to the same implementation batch or pull request.
- Do not continue using a completed development branch after its pull request has been opened or the batch has been completed.
- The Phase total implementation plan at `dev/imple/Phase-XX/Phase-XX-总实施方案.md` is authoritative for batch order, target versions, and branch allocation.
- If an unstarted batch split or order changes, update the total implementation plan and recalculate affected uncreated branches.
- Never silently rename or renumber a branch that has already been pushed; coordinate the change with the user.
- Pull requests from `update` to `main` must use merge commits and must not delete `update`.
- Ordinary `develop/x.x.x` pull requests may use squash merge and delete their branches.

# Version Rule

- Use the three-part `major.minor.patch` format.
- Phase 0 through Phase 3 use `0.1.x` through `0.4.x`.
- After Phase 3 passes milestone acceptance, publish `1.0.0`.
- Phase 4 through Phase 20 use `1.(Phase - 3).x`.
- Reserve patch `0` as the Phase baseline. Executable batches start at patch `1` in planned execution order.
- Each executable batch has one target version, and all implementation commits in that batch share that target.
- The root `VERSION` file is the sole source of the current completed product version.
- At successful batch completion, update `VERSION` to the batch target and include it in the completion commit.
- Work performed within the permitted scope of `update` does not change `VERSION`.

# Implementation and Validation Rule

- Treat the current implementation plan as the acceptance contract.
- A Phase total implementation plan must define Phase-level capabilities, cross-batch integration results, acceptance criteria, and milestone completion conditions.
- A split implementation plan must define its scope, acceptance criteria, validation commands where known, required regression scope, and explicit completion condition.
- Initial discovery must stay limited to directly affected project code, tests, configuration, and public interfaces.
- Inspect third-party source only when a concrete build, runtime, or required-test failure cannot be resolved from the local call site, public API, documentation, and reported error.
- Do not turn implementation work into a general code audit, dependency audit, coverage campaign, architecture review, or speculative hardening pass unless the user explicitly requests it.
- Add or change tests only when they prove changed acceptance behavior, reproduce an observed defect, or protect a changed security boundary, persistent-data invariant, or public contract.
- Prefer the lowest effective test layer and the smallest representative set of success and failure cases.
- Run validation in stages:
  1. Run the smallest directly affected checks during implementation.
  2. Run the batch plan's fixed completion gates against the final candidate.
- Expand validation only for a recorded concrete risk or observed regression.
- A successful check remains valid while its relevant code, configuration, dependencies, candidate metadata, and execution environment remain unchanged.
- If the candidate changes after a check, rerun the affected check. Do not rerun unrelated successful checks.
- Stop when the documented acceptance criteria and fixed completion gates pass with no blocking issue. Record unrelated or non-blocking findings as follow-up items.

# Release Candidate Rule

- When a batch requires a frozen release candidate or an expensive product acceptance matrix, follow that batch's implementation plan and referenced matrix document.
- Run deterministic preflight before the full matrix.
- Bind the candidate revision, Bundle, manifest, runtime contract, receipts, and published evidence.
- Any relevant application, acceptance, verification, or release-metadata change invalidates the affected candidate evidence and requires a new candidate revision.
- Do not repair a failed candidate by editing receipts, replacing tags, or reusing evidence from another revision.
- Distinguish product failures from acceptance-infrastructure failures and fix the smallest affected layer.
- Never use broad Docker cleanup or global prune to make resource-isolation checks pass.
- Verify the exact sanitized evidence selected for publication before declaring the matrix complete.

# Completion, Log, and Commit Rule

- The implementation-log requirement applies when executing a plan under `dev/imple/Phase-XX/`.
- Before declaring that implementation plan complete, create or update the corresponding file under `dev/logs/Phase-XX/` using the same Markdown filename.
- The implementation log must record only:
  - Work actually completed.
  - Files actually changed.
  - Commands actually executed and their results.
  - Deviations from the plan.
  - Known limitations and follow-up items.
- Never record planned or unverified work as completed.
- After a task completes successfully and modifies project files, create a Git commit before reporting completion.
- Do not create a completion commit for failed, cancelled, or incomplete work unless the user explicitly asks to preserve that state.
- Stage and commit only files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write commit messages in English and use Conventional Commits when practical.
- Run proportionate checks before committing. If a required check cannot run or fails, report that clearly.
- Do not create an empty commit.

# Active Platform Rule

- The maintained product and acceptance environment is Linux `amd64` with Bash.
- Kubernetes is a deployment environment, not a prerequisite for proving that the Compose product works.
- Phase 18 through Phase 20 use WSL2/Linux as the primary Kubernetes implementation and integration environment.
- Keep the active Phase 18–20 checkout in the WSL Linux filesystem and use one Docker daemon for that workspace.
- Preserve the Linux `amd64` Compose lifecycle, release, backup, restore, and upgrade contracts wherever later work directly affects them.
- Existing `scripts/*.ps1` files are frozen at the `0.2.1` capability baseline and must not be extended with current Compose or Kubernetes behavior.

# Authority and Conflict Rule

- This file defines repository-wide execution constraints.
- The Phase total implementation plan defines batch allocation and Phase-level acceptance.
- The split implementation plan defines the current batch scope and completion gates.
- Matrix and release documents define detailed acceptance procedures only when referenced by the active batch.
- A lower-level document may add detail but must not silently weaken branch, version, safety, evidence, or commit constraints from this file.
- If authoritative documents conflict or the requested work cannot be assigned to a valid batch, stop and ask the user before changing project files.
