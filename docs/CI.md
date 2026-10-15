# 模块 CI

旧工作流和旧 `ci/` 脚本已移除。现在只有 `.github/workflows/ci.yml`，不包含自动创建 PR、自动合并、删分支、缓存预热或发布。

## 什么时候运行

- PR 创建、更新、重新打开、修改标题、添加或移除标签时运行；模块测试使用 GitHub 生成的合并版本，提交规范检查使用 PR 的实际提交。
- 手动运行必须输入模块标签，支持逗号或空格分隔，不再默认全量检查。
- 不再监听 `push` 或 `merge_group`。这些事件没有 PR 模块标签，不据此推断或扩大模块范围；直接推送 main 不会运行本工作流，也不支持合并队列检查。
- 同一个 PR 或分支有新任务时，取消旧任务；每个任务有独立超时。

## 只按标签选择模块

CI 只接受 PR 上人工选择的 `ci:<模块目录>` 标签。支持的模块来自根 Makefile 的 `GO_MODULES`；前端不进入 CI，`NPM_MODULES` 仅用于本地命令。

- `ci:backend`：只运行 backend 的检查、构建和单测。
- `ci:exporters/redis`：只运行 Redis exporter 的三个任务。
- 多个模块标签：只运行这些模块，不增加其他模块。
- `ci:componentmetrics`：只检查共享库本身，不自动检查使用它的模块。
- 根 Makefile、CI、公共配置变化：仍只运行贴了标签的模块，不自动全量检查。
- `ci:none`：明确不运行模块任务，适合仅前端、文档等改动；CI 工具测试和提交规范检查仍然运行。
- 没有模块标签也没有 `ci:none`、存在未知 `ci:` 标签、或 `ci:none` 与模块标签混用：选择失败，最终 CI 失败。

标签是唯一的 CI 模块开关。不自动打标签，不根据文件变化核对或补齐标签；维护者负责选择正确范围。漏贴某个模块的标签，就不会检查该模块。普通的 `bug`、`documentation` 等标签不参与选择。

仓库维护者需先在 GitHub 创建对应标签。本次只提供工作流与本地配置，不创建或修改远端标签。全部合法名称可以查看：

```sh
make list-ci-labels
```

这里只选择和验证模块，不做真实环境集成、浏览器端到端、容器运行或发布验收。这些仍通过原来的显式 Makefile 命令执行。

## 每个模块检查什么

每个被选中的 Go 模块运行三个独立并行任务，同一套命令可以在本地执行：

```sh
make ci-check MODULE=backend  # gofmt、go vet、golangci-lint
make ci-test MODULE=backend   # go test -race -count=1 ./...
make ci-build MODULE=backend  # 模块构建
```

前端不进入 CI，现有本地命令保留：

```sh
make check MODULE=frontend   # TypeScript 检查
make lint MODULE=frontend    # ESLint
make test MODULE=frontend    # Vitest
make build MODULE=frontend   # 类型检查、Vite 构建
```

根 Makefile 分发到各模块 Makefile。Go lint 工具继续由 `devtools/toolchains.json` 固定版本和校验和，但不再使用旧 CI 的 lint 基线豁免；发现问题会直接失败。

`ci-*` 命令使用 `.run/ci/go.work`，确保共享 Go 代码来自当前检出目录。不会覆盖开发者根目录的 `go.work`，也不会修改各模块的 `go.mod`。

查看指定标签的计划和复现完整模块检查：

```sh
make ci-tools
make ci-plan LABELS="ci:backend ci:router"
make ci-plan LABELS=ci:none
make verify MODULE=backend
```

`ALL=1` 不再支持。`make verify BASE_REF=origin/main` 和 `make ci-plan BASE_REF=origin/main` 仅保留为本地差异工具：比较共同祖先到 HEAD，只选择直接变动的 Go 模块，不扩展依赖、不对公共文件全量兜底。GitHub 工作流不使用这个入口。尚未提交的修改可以直接用 `MODULE=...` 运行检查。

## 合并门禁

建议在 GitHub 分支保护中将固定的 `CI result` 设置为必需检查。它要求标签选择和提交规范校验成功，且标签选中的 Check、Test、Build 全部成功；失败、取消、意外跳过都会失败。只有显式使用 `ci:none` 时才允许模块任务正常跳过。

旧的必需检查名如果仍在仓库设置中，需要手动移除或替换。本次只修改本地仓库文件，不修改远端保护规则。

## 提交规范

采用 [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/)，项目类型固定在 `.conventional-commits.json`：`build`、`chore`、`ci`、`docs`、`feat`、`fix`、`perf`、`refactor`、`revert`、`style`、`test`，统一小写。

格式为 `type(scope)!: description`，scope 和 `!` 可省略，描述可用中文或英文；正文和标题之间空一行。破坏性改动使用 `!` 或 `BREAKING CHANGE: 描述`（也支持 `BREAKING-CHANGE`）。例如 `fix(frontend): 等待发帖后的路由跳转`、`ci: rebuild module-based checks`。

运行 `make hooks-install` 启用本仓库的提交钩子；克隆到新目录后需要重新运行。`make check-commits BASE_REF=origin/main` 可检查分支新增提交。PR 标题同样需要合规，修改标题会重新触发 CI。

规则从配置中的 `legacy_ref`（本轮重构前的 `506a12f`）之后生效，不改写或追溯旧提交；不要移动此起点来豁免后续提交。真实 merge commit 豁免，但普通提交仅写成 `Merge ...` 不会被放行。`--no-verify` 只能绕过本地钩子，PR 和手动运行中的 CI 仍会拦截不合规的新提交；直接推送 main 不触发本工作流。
