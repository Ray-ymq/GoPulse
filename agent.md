# Automatic Commit Rule

- After every task that adds, deletes, or modifies project files, automatically create a Git commit before reporting completion.
- Stage and commit only the files changed for the current task. Never include unrelated, pre-existing, or user-owned changes.
- Write every commit message in English and make it clearly describe the completed change.
- Use the Conventional Commits format when practical, for example: `docs: add automatic commit instructions`.
- Run appropriate checks before committing. If checks cannot be run or fail, state that clearly in the final response.
- Do not create an empty commit when no project files were changed.
