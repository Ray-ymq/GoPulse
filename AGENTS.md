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
- Phase 4 through Phase 16 use the `1.x.x` series. Their minor version is `Phase - 3`, so Phase 4 uses `1.1.x`, Phase 5 uses `1.2.x`, and Phase 16 uses `1.13.x`.
- Within each Phase, reserve patch `0` as the Phase baseline. Number executable development batches from patch `1` in their planned execution order. For example, `Phase-00-01` maps to `0.1.1`, `Phase-01-01` maps to `0.2.1`, and `Phase-04-01` maps to `1.1.1`.
- Every executable development batch must have its own target version and its own `develop/x.x.x` branch. All commits belonging to that batch share the same target version.
- Do not require an individual implementation-plan file to declare its version or branch. After a Phase's batch count and execution order are known, its `dev/imple/Phase-XX/Phase-XX-总实施方案.md` must contain the authoritative batch-to-version and batch-to-branch allocation for that Phase.
- If a Phase's batch split or order changes before implementation, update its total implementation plan and recalculate all not-yet-created branches in that Phase. Never silently rename or renumber a branch that has already been pushed; coordinate the adjustment with the user.
- The root `VERSION` file is the sole source of the current completed product version when it exists. If it does not exist before the first development batch, use `0.1.0` as the Phase 0 baseline and create the file as part of the first development batch.
- At development-batch completion, update `VERSION` to the batch's target version and include it in that batch's commit. Planning, documentation, tests-only, formatting, and repository-rule work performed on `update` do not change `VERSION`.

# Development Branch Naming Rule

- Every development branch that is pushed to a remote must use the format `develop/x.x.x`, including branches used for pull requests and testing.
- The exact branch name `update` is the sole exception and may be pushed as the project's planning branch. It may contain only project planning, architecture adjustments, development plans, documentation organization, planning-workspace metadata, and repository-rule maintenance; it must not be used for feature implementation, application testing, or ordinary development pull requests.
- The `develop` prefix must always start with a lowercase `d`; uppercase or mixed-case variants are not allowed.
- Before pushing, verify that the branch name either matches `^develop/[0-9]+\.[0-9]+\.[0-9]+$` or is exactly `update`. When pushing `update`, also verify that the commits being pushed remain within the planning-branch scope defined above.

# Development Branch Lifecycle Rule

- Before starting each new independent development task, fetch the latest state from the repository's configured primary remote, determine the target version under the Automatic Version Management Rule, and create a new branch from that remote's `main` branch.
- The new branch must use the target version and follow the `develop/x.x.x` naming rule defined above.
- Continue using the current branch only for follow-up work that belongs to the same active task or pull request.
- Do not automatically continue working on a development branch after its work is complete or after a pull request has been opened for it.
- Work that remains entirely within the planning-only scope permitted for the `update` branch is exempt from creating a development branch and may be committed directly on `update`; it does not bump the product version.

# Implementation Log Rule

- After completing each implementation plan under `dev/imple/Phase-XX/`, create or update its corresponding development record under `dev/logs/Phase-XX/` before reporting the plan complete.
- Mirror the plan's phase directory and Markdown filename. For example, `dev/imple/Phase-00/Phase-00-01-工程骨架与基础设施.md` must be recorded in `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`.
- Each record must describe the work actually completed, files changed, validation commands and results, deviations from the plan, and known limitations or follow-up items.
- Record only work and checks that were actually performed; never present planned or unverified work as completed.

# Platform Usage Rule

- On macOS, use the project workspace primarily for project planning and subsequent architectural adjustments.
- On Windows, use the project workspace primarily for project development and implementation.
