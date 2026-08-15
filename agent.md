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
