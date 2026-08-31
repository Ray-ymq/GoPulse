# Automatic Commit Rule

- After every task that adds, deletes, or modifies project files, automatically create a Git commit before reporting completion.
- Stage and commit only the files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write every commit message in English and make it clearly describe the completed change.
- Use the Conventional Commits format when practical, for example: `docs: add automatic commit instructions`.
- Run appropriate checks before committing. If checks cannot be run or fail, state that clearly in the final response.
- Do not create an empty commit when no project files were changed.

# Automatic Version Management Rule

- Determine the version impact once for each new independent development task or pull request before creating its development branch. All commits in the same task or pull request must share that single target version; never bump the version separately for individual commits.
- Use Semantic Versioning in `major.minor.patch` format.
- Determine the version from the actual product and compatibility impact of the complete task, not solely from Conventional Commit types:
  - Changes limited to documentation, tests, formatting, planning, repository rules, or other work that does not affect product behavior do not bump the product version.
  - Bug fixes, compatibility improvements, dependency maintenance, and small internal adjustments bump the patch version, for example `0.1.0` to `0.1.1`.
  - Backward-compatible features and substantial backward-compatible enhancements bump the minor version, for example `0.1.1` to `0.2.0`.
  - Changes that break APIs, configuration, protocols, data formats, or other compatibility guarantees bump the major version, for example `0.9.0` to `1.0.0`.
- The root `VERSION` file is the sole source of the current product version when it exists. If it does not exist, determine the current version from valid SemVer Git tags. If neither a `VERSION` file nor a valid SemVer Git tag exists, ask the user for the initial version before creating a development branch; never guess it.
- Before creating a development branch, determine the target version from the current version and the task's actual impact, then use that target in the `develop/x.x.x` branch name.
- If the task scope changes during implementation, reassess the version impact from the original current version, keep a single final target version rather than applying incremental bumps, and rename the development branch to the corrected target version before its first push. After the branch has been pushed, do not silently change the target version; coordinate the required branch or pull request adjustment with the user.
- At task completion, update `VERSION` to the target version and include it in the task's commit. Do not modify `VERSION` for tasks that do not require a product version bump, including documentation, planning, and repository-rule-only work.

# Development Branch Naming Rule

- Every development branch that is pushed to a remote must use the format `develop/x.x.x`, including branches used for pull requests and testing.
- The exact branch name `update` is the sole exception and may be pushed as the project's planning branch. It may contain only project planning, architecture adjustments, development plans, documentation organization, planning-workspace metadata, and repository-rule maintenance; it must not be used for feature implementation, application testing, or ordinary development pull requests.
- The `develop` prefix must always start with a lowercase `d`; uppercase or mixed-case variants are not allowed.
- Before pushing, verify that the branch name either matches `^develop/[0-9]+\.[0-9]+\.[0-9]+$` or is exactly `update`. When pushing `update`, also verify that the commits being pushed remain within the planning-branch scope defined above.

# Development Branch Lifecycle Rule

- Before starting each new independent development task, fetch the latest remote state, determine the target version under the Automatic Version Management Rule, and create a new branch from `origin/main`.
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
