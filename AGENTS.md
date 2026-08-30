# Automatic Commit Rule

- After every task that adds, deletes, or modifies project files, automatically create a Git commit before reporting completion.
- Stage and commit only the files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write every commit message in English and make it clearly describe the completed change.
- Use the Conventional Commits format when practical, for example: `docs: add automatic commit instructions`.
- Run appropriate checks before committing. If checks cannot be run or fail, state that clearly in the final response.
- Do not create an empty commit when no project files were changed.

# Development Branch Naming Rule

- Every development branch that is pushed to a remote must use the format `develop/x.x.x`, including branches used for pull requests and testing.
- The `develop` prefix must always start with a lowercase `d`; uppercase or mixed-case variants are not allowed.
- Before pushing, verify that the branch name matches `^develop/[0-9]+\.[0-9]+\.[0-9]+$`.

# Development Branch Lifecycle Rule

- Before starting each new independent development task, fetch the latest remote state and create a new branch from `origin/main`.
- The new branch must follow the `develop/x.x.x` naming rule defined above.
- If the user has not specified the target version, ask for the version before creating the branch; never invent a version number.
- Continue using the current branch only for follow-up work that belongs to the same active task or pull request.
- Do not automatically continue working on a development branch after its work is complete or after a pull request has been opened for it.
- Rule-only maintenance performed while introducing this lifecycle policy may be committed on the current branch; the policy applies to subsequent independent development tasks.

# Implementation Log Rule

- After completing each implementation plan under `dev/imple/Phase-XX/`, create or update its corresponding development record under `dev/logs/Phase-XX/` before reporting the plan complete.
- Mirror the plan's phase directory and Markdown filename. For example, `dev/imple/Phase-00/Phase-00-01-工程骨架与基础设施.md` must be recorded in `dev/logs/Phase-00/Phase-00-01-工程骨架与基础设施.md`.
- Each record must describe the work actually completed, files changed, validation commands and results, deviations from the plan, and known limitations or follow-up items.
- Record only work and checks that were actually performed; never present planned or unverified work as completed.
