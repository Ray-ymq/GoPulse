# Automatic Commit Rule

- After every task that adds, deletes, or modifies project files, automatically create a Git commit before reporting completion.
- Stage and commit only the files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write every commit message in English and make it clearly describe the completed change.
- Use the Conventional Commits format when practical, for example: `docs: add automatic commit instructions`.
- Run appropriate checks before committing. If checks cannot be run or fail, state that clearly in the final response.
- Do not create an empty commit when no project files were changed.

# Automatic Version Management Rule

- Use the three-part `major.minor.patch` format, with a project-specific mapping from product maturity, Phase, and execution batch to version numbers.
- Phase 0 through Phase 3 are pre-1.0 development and use these version lines:
  - Phase 0 uses `0.1.x`.
  - Phase 1 uses `0.2.x`.
  - Phase 2 uses `0.3.x`.
  - Phase 3 uses `0.4.x`.
- After Phase 3 is fully completed and its milestone acceptance passes, publish the usable-business-system milestone as `1.0.0`.
- Phase 4 through Phase 17 use the `1.x.x` series. Their minor version is `Phase - 3`, so Phase 4 uses `1.1.x`, Phase 5 uses `1.2.x`, Phase 13 uses `1.10.x`, and Phase 17 uses `1.14.x`.
- Within each Phase, reserve patch `0` as the Phase baseline. Number executable development batches from patch `1` in their planned execution order. For example, `Phase-00-01` maps to `0.1.1`, `Phase-01-01` maps to `0.2.1`, and `Phase-04-01` maps to `1.1.1`.
- Every executable development batch must have its own target version and its own `develop/x.x.x` branch. All commits belonging to that batch share the same target version.
- Do not require an individual implementation-plan file to declare its version or branch. After a Phase's batch count and execution order are known, its `dev/imple/Phase-XX/Phase-XX-总实施方案.md` must contain the authoritative batch-to-version and batch-to-branch allocation for that Phase.
- If a Phase's batch split or order changes before implementation, update its total implementation plan and recalculate all not-yet-created branches in that Phase. Never silently rename or renumber a branch that has already been pushed; coordinate the adjustment with the user.
- The root `VERSION` file is the sole source of the current completed product version when it exists. If it does not exist before the first development batch, use `0.1.0` as the Phase 0 baseline and create the file as part of the first development batch.
- At development-batch completion, update `VERSION` to the batch's target version and include it in that batch's commit. Planning, documentation, tests-only, formatting, and repository-rule work performed on `update` do not change `VERSION`.

# Development Branch Naming Rule

- Every development branch that is pushed to a remote must use the format `develop/x.x.x`, including branches used for pull requests and testing.
- The exact branch name `update` is the sole exception and may be pushed as the project's planning branch. It may contain only project planning, architecture adjustments, development plans, documentation organization, planning-workspace metadata, and repository-rule maintenance; it must not be used for feature implementation, application testing, or ordinary development pull requests.
- `update` is a long-lived planning branch. Automated pull requests from `update` to `main` must use a merge commit and must not delete `update`, so later planning work retains ancestry with `main`. Ordinary `develop/x.x.x` branches continue to use squash merge and may be deleted after merging.
- The `develop` prefix must always start with a lowercase `d`; uppercase or mixed-case variants are not allowed.
- Before pushing, verify that the branch name either matches `^develop/[0-9]+\.[0-9]+\.[0-9]+$` or is exactly `update`. When pushing `update`, also verify that the commits being pushed remain within the planning-branch scope defined above.

# Development Branch Lifecycle Rule

- Before starting each new independent development task, fetch the latest state from the repository's configured primary remote, determine the target version under the Automatic Version Management Rule, and create a new branch from that remote's `main` branch.
- The new branch must use the target version and follow the `develop/x.x.x` naming rule defined above.
- Continue using the current branch only for follow-up work that belongs to the same active task or pull request.
- Do not automatically continue working on a development branch after its work is complete or after a pull request has been opened for it.
- Work that remains entirely within the planning-only scope permitted for the `update` branch is exempt from creating a development branch and may be committed directly on `update`; it does not bump the product version.

# Implementation Plan Acceptance Rule

- Every Phase total implementation plan under `dev/imple/Phase-XX/Phase-XX-总实施方案.md` must define the Phase-level acceptance criteria, including the end-to-end capabilities, cross-batch integration results, and milestone conditions required to consider the Phase complete.
- Every split implementation plan under `dev/imple/Phase-XX/Phase-XX-XX-*.md` must define acceptance criteria for that batch, including concrete validation items or commands where they can be determined, the necessary regression scope, and an explicit completion condition.
- Acceptance and validation must remain proportional to the plan's implementation scope, risk, and direct impact. By default, validate the current batch, directly affected behavior, and necessary regressions only.
- Expand validation beyond the defined scope only when there is a specific risk basis, such as changes to shared infrastructure, security boundaries, persistent data, public contracts, or evidence of a regression. Record the reason for the expanded scope.
- Stop validation when the documented acceptance criteria have passed and no blocking issue remains. Record non-blocking improvements and unrelated findings as follow-up items instead of extending the current task indefinitely.
- Do not repeat an already successful validation unless relevant code, configuration, dependencies, or the execution environment changed in a way that could affect its result.
- Do not make a standalone code or architecture review, a severity classification, or a separate review report a default development gate. Perform such work only when the user explicitly requests it.

# Execution Efficiency Rule

- This rule applies to Phase-02-03 and every later implementation task. The implementation plan is an acceptance contract, not an invitation to perform a general code audit, dependency audit, coverage campaign, or speculative hardening pass.
- Spend no more than 10 minutes on initial discovery before making the first in-scope implementation change. Read the directly affected project code, tests, and public interfaces only. Do not read third-party dependency source by default.
- Inspect third-party dependency source only when a concrete compiler error, runtime failure, or required failing test cannot be resolved from the local call site, the dependency's public API/documentation, and the reported error. Limit inspection to the smallest relevant symbol and record the reason in the implementation log.
- Add or change a test only when it directly proves a new or changed acceptance criterion, reproduces an observed defect, or protects a changed security boundary, persistent-data invariant, or public contract. Do not add tests merely to improve coverage, enumerate hypothetical boundary combinations, test unchanged library behavior, or duplicate the same behavior across unit, integration, and end-to-end layers.
- For one changed state transition, prefer one representative success case and one representative failure case at the lowest effective test layer. Add more cases only when the implementation plan explicitly requires distinct business outcomes or a concrete failure demonstrates the need.
- Run validation in stages: the smallest affected-package check during implementation, then the batch plan's fixed completion gates once against the final diff. Expand beyond those gates only for a specific observed regression or documented cross-cutting risk. Record that reason before expanding.
- A successful check remains valid after conversation context compaction. Reconstruct progress from the implementation plan, Git diff, implementation log, and captured command results, then continue from the first unmet required item. Context compaction alone must never trigger source rereading, new tests, or rerunning successful checks.
- If optional investigation or optional test work consumes 15 consecutive minutes without resolving a required failure or advancing production implementation, stop it immediately. Return to the shortest in-scope implementation path or record the item as a non-blocking follow-up.
- As soon as the documented acceptance criteria and fixed completion gates pass with no blocking failure, update the implementation log and version, commit, and stop. Do not spend remaining time on opportunistic refactors, additional edge cases, or unrelated cleanup.

# Implementation Log Rule

- After completing each implementation plan under `dev/imple/Phase-XX/`, create or update its corresponding development record under `dev/logs/Phase-XX/` before reporting the plan complete.
- Mirror the plan's phase directory and Markdown filename. For example, `dev/imple/Phase-00/Phase-00-01-工程骨架与基础设施.md` must be recorded in `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`.
- Each record must describe the work actually completed, files changed, validation commands and results, deviations from the plan, and known limitations or follow-up items.
- Record only work and checks that were actually performed; never present planned or unverified work as completed.

# Platform Usage Rule

- Phase 0 and Phase-01-01 completed the original native Windows PowerShell and Unix Bash dual-platform baseline through product version `0.2.1`. Phase-01-02 through Phase 12 were implemented and accepted in WSL2/Linux with Bash as the maintained lifecycle path.
- Phase 13 is the explicit cross-platform productization stage. Its total implementation plan must define and actually run a support matrix containing at least Linux `amd64`, macOS `arm64`, and Windows `amd64`; cross-compilation or static script inspection alone cannot establish platform support.
- Phase 13 supports GoPulse on macOS and Windows through Linux containers and a shared product lifecycle implementation. It does not require MySQL, Kafka, Elasticsearch, GoPulse services, or Exporter processes to become native Windows services or macOS daemons.
- Phase 13 must let users invoke the supported lifecycle from macOS Terminal and Windows PowerShell/Terminal without duplicating the core orchestration logic into separate Bash and PowerShell implementations. Thin platform launchers are allowed when required by the chosen shared entry point.
- The existing `scripts/*.ps1` files remain frozen at the `0.2.1` capability baseline and are historical artifacts; do not extend them into a second implementation of current Compose behavior. A new thin launcher may use a different explicit path/name when the Phase 13 total plan requires it.
- For Phase 13 Windows acceptance, use the checkout and Docker Desktop arrangement defined by its support contract and validate Windows path, line-ending, file-sharing, port, signal, cleanup, and browser behavior. Do not claim native Windows support from a WSL-only run.
- Phase 14 through Phase 17 return to WSL2/Linux as the primary Kubernetes implementation, application-testing, and integration-acceptance environment. Keep the active repository checkout in the WSL Linux filesystem, such as `/home/<user>/src/GoPulse`, rather than under `/mnt/c`, `/mnt/d`, or another Windows-mounted filesystem, and use only one Docker daemon for that workspace.
- Phase 14 through Phase 17 must preserve the cross-platform product and multi-architecture image contracts delivered by Phase 13 where directly affected, but they do not require a Kubernetes cluster itself to run natively on macOS or Windows.
