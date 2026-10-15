# 模块 CI

旧工作流和旧 `ci/` 脚本已移除。现在只有 `.github/workflows/ci.yml`，不包含自动创建 PR、自动合并、删分支、缓存预热或发布。

## 什么时候运行

- PR 创建、更新、重新打开、修改标题时运行；模块测试使用 GitHub 生成的合并版本，提交规范检查使用 PR 的实际提交。
- 推送到 `main` 时运行，检查本次推送的全部提交，而不只是最后一个提交。
- 合并队列运行对应合并组的检查。
- 手动运行时检查全部模块。
- 同一个 PR 或分支有新任务时，取消旧任务；每个任务有独立超时。

## 怎么选择模块

模块清单只维护在根 Makefile 的 `GO_MODULES`、`NPM_MODULES` 中。新增模块需要加入清单，并提供模块 Makefile。

- 模块目录变化：选择该模块。
- 共享 Go 模块变化：根据 `go.mod` 中的依赖和本地替换关系，递归选择使用它的模块。
- `frontend-shared` 变化：只检查两个使用它的前端。
- `devtools` 变化：同时检查两个前端，因为它们的依赖安装由这个工具执行。
- 根 Makefile、CI、共享配置、部署文件或未识别的代码目录变化：保守地检查全部模块。
- 仅 Markdown、文档目录或 LICENSE 变化：不启动产品模块任务，但仍测试 CI 工具、检查提交规范，并生成最终检查结果。
- 文件删除、跨模块重命名、多提交推送都包含在差异中。推送的旧提交无法取得时，全量检查，不静默跳过。

这里只选择和验证模块，不做真实环境集成、浏览器端到端、容器运行或发布验收。这些仍通过原来的显式 Makefile 命令执行。

## 每个模块检查什么

三个任务独立并行，同一套命令可以在本地执行：

```sh
make ci-check MODULE=backend  # gofmt、go vet、golangci-lint
make ci-test MODULE=backend   # go test -race -count=1 ./...
make ci-build MODULE=backend  # 模块构建

make ci-check MODULE=frontend # TypeScript 检查、ESLint
make ci-test MODULE=frontend  # Vitest
make ci-build MODULE=frontend # 类型检查、Vite 构建
```

根 Makefile 分发到各模块 Makefile。Go lint 工具继续由 `devtools/toolchains.json` 固定版本和校验和，但不再使用旧 CI 的 lint 基线豁免；发现问题会直接失败。

`ci-*` 命令使用 `.run/ci/go.work`，确保共享 Go 代码来自当前检出目录。不会覆盖开发者根目录的 `go.work`，也不会修改各模块的 `go.mod`。

查看计划和复现完整模块检查：

```sh
make ci-tools
make ci-plan BASE_REF=origin/main
make ci-plan ALL=1
make verify MODULE=backend
make verify BASE_REF=origin/main
```

本地差异计划比较共同祖先到 HEAD，只包含已提交内容。尚未提交的修改可以直接用 `MODULE=...` 运行检查。

## 合并门禁

建议在 GitHub 分支保护中将固定的 `CI result` 设置为必需检查。它要求提交规范校验、选择成功，且所有选中的 Check、Test、Build 全部成功；失败、取消、意外跳过都会失败。文档改动允许模块任务正常跳过，但仍检查提交规范。

旧的必需检查名如果仍在仓库设置中，需要手动移除或替换。本次只修改本地仓库文件，不修改远端保护规则。

## 提交规范

采用 [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/)，项目类型固定在 `.conventional-commits.json`：`build`、`chore`、`ci`、`docs`、`feat`、`fix`、`perf`、`refactor`、`revert`、`style`、`test`，统一小写。

格式为 `type(scope)!: description`，scope 和 `!` 可省略，描述可用中文或英文；正文和标题之间空一行。破坏性改动使用 `!` 或 `BREAKING CHANGE: 描述`（也支持 `BREAKING-CHANGE`）。例如 `fix(frontend): 等待发帖后的路由跳转`、`ci: rebuild module-based checks`。

运行 `make hooks-install` 启用本仓库的提交钩子；克隆到新目录后需要重新运行。`make check-commits BASE_REF=origin/main` 可检查分支新增提交。PR 标题同样需要合规，修改标题会重新触发 CI。

规则从配置中的 `legacy_ref`（本轮重构前的 `506a12f`）之后生效，不改写或追溯旧提交；不要移动此起点来豁免后续提交。真实 merge commit 豁免，但普通提交仅写成 `Merge ...` 不会被放行。`--no-verify` 只能绕过本地钩子，CI 仍会拦截不合规的新提交。
