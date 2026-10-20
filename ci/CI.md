# 模块 CI

旧工作流和旧 `ci/` 脚本已移除。现在只有 `.github/workflows/ci.yml`，不包含自动创建 PR、自动合并、删分支、缓存预热或发布。

## 什么时候运行

- PR 创建、更新、重新打开、修改标题或正文、添加或移除标签时运行；模块测试使用 GitHub 生成的合并版本，提交规范检查使用 PR 的实际提交。
- 手动运行必须输入模块标签，支持逗号或空格分隔，不再默认全量检查。
- 不再监听 `push` 或 `merge_group`。这些事件没有 PR 模块标签，不据此推断或扩大模块范围；直接推送 main 不会运行本工作流，也不支持合并队列检查。
- 同一个 PR 或分支有新任务时，取消旧任务；每个任务有独立超时。

## 只按标签选择模块

CI 只接受 PR 上人工选择的 `ci:<模块目录>` 标签。支持的模块来自根 Makefile 的 `GO_MODULES`；前端不进入 CI，`NPM_MODULES` 仅用于本地命令。

- `ci:backend`：只运行 backend 的检查、构建和单测。
- `ci:exporters/redis`：只运行 Redis exporter 的三个任务。
- 多个模块标签：只运行这些模块，不增加其他模块。
- `ci:runtimekit`：只检查共享库本身，不自动检查使用它的模块。
- 根 Makefile、CI、公共配置变化：仍只运行贴了标签的模块，不自动全量检查。
- `ci:none`：明确不运行模块任务，适合仅前端、文档等改动；CI 工具测试和提交规范检查仍然运行。
- 没有模块标签也没有 `ci:none`、存在未知 `ci:` 标签、或 `ci:none` 与模块标签混用：选择失败，最终 CI 失败。

标签是唯一的 CI 模块开关。不自动打标签，不根据文件变化核对或补齐标签；维护者负责选择正确范围。漏贴某个模块的标签，就不会检查该模块。普通的 `bug`、`documentation` 等标签不参与选择。

仓库维护者需先在 GitHub 创建对应标签。本次只提供工作流与本地配置，不创建或修改远端标签。全部合法名称可以查看：

```sh
make list-ci-labels
```

这里只选择和验证模块，不做真实环境集成、浏览器端到端或容器运行。这些仍通过显式 Makefile 命令执行。

## 每个模块检查什么

每个被选中的 Go 模块运行三个独立并行任务，同一套命令可以在本地执行：

```sh
make ci-check MODULE=backend  # gofmt、go vet、golangci-lint
make ci-test MODULE=backend   # go test -race -count=1 ./...
make ci-build MODULE=backend  # 模块构建
```

前端按 `frontend/user/`（用户端）、`frontend/admin/`（管理端）和 `frontend/shared/`（共用组件与工具）组织。两个应用分别维护 npm 依赖和构建配置，不进入 CI。本地命令如下：

```sh
make check MODULE=frontend/user   # TypeScript 检查
make lint MODULE=frontend/user    # ESLint
make test MODULE=frontend/user    # Vitest
make build MODULE=frontend/user   # 类型检查、Vite 构建
```

管理端使用相同命令，将 `MODULE` 替换为 `frontend/admin`。开发环境依赖准备也使用这两个模块路径，例如 `make bootstrap MODULE=frontend/user`。

根 Makefile 分发到各模块 Makefile。Go lint 工具继续由 `devtools/toolchains.json` 固定版本和校验和，但不再使用旧 CI 的 lint 基线豁免；发现问题会直接失败。

`ci-*` 命令使用 `.run/ci/go.work`，确保共享 Go 代码来自当前检出目录。不会覆盖开发者根目录的 `go.work`，也不会修改各模块的 `go.mod`。

查看指定标签的计划和复现完整模块检查：

```sh
make ci-tools
make ci-plan LABELS="ci:backend ci:marshaller"
make ci-plan LABELS=ci:none
make verify MODULE=backend
```

`ALL=1` 不再支持。`make verify BASE_REF=origin/main` 和 `make ci-plan BASE_REF=origin/main` 仅保留为本地差异工具：比较共同祖先到 HEAD，只选择直接变动的 Go 模块，不扩展依赖、不对公共文件全量兜底。GitHub 工作流不使用这个入口。尚未提交的修改可以直接用 `MODULE=...` 运行检查。

## 合并门禁

建议在 GitHub 分支保护中将固定的 `CI result` 设置为必需检查。它要求标签选择和提交规范校验成功，且标签选中的 Check、Test、Build 全部成功；失败、取消、意外跳过都会失败。只有显式使用 `ci:none` 时才允许模块任务正常跳过。

旧的必需检查名如果仍在仓库设置中，需要手动移除或替换。本次只修改本地仓库文件，不修改远端保护规则。

## 提交规范

采用 [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/)，项目类型固定在 `.conventional-commits.json`：`build`、`chore`、`ci`、`feat`、`fix`、`perf`、`refactor`、`revert`、`style`、`test`，统一小写。

不得单独创建 `docs` 类型的 commit。文档修改必须随对应改动一并提交，标题使用对应改动的类型，例如 `feat`、`fix` 或 `ci`。本地提交钩子和 CI 均拒绝新提交的 `docs:`、`docs(scope):` 及带 `!` 的形式；PR 标题也不得使用 `docs`。校验按标题中的类型判断，不根据文件路径识别纯文档改动。

标题格式为 `type(scope)!: description`，scope 和 `!` 可省略，描述可用中文或英文。破坏性改动使用 `!` 或 `BREAKING CHANGE: 描述`（也支持 `BREAKING-CHANGE`），说明兼容性变化和迁移要求。例如 `fix(frontend): 等待发帖后的路由跳转`、`ci: rebuild module-based checks`。

### Commit 正文

一个 commit 描述一项完整改动。标题与正文之间空一行，正文固定包含三个非空部分；小改动可以每部分各写一句，不省略章节。

- `背景：`：说明问题、触发条件或改动原因。
- `改动：`：说明具体改动和行为变化。
- `验证：`：记录实际执行的命令及结果，必要时补充人工检查。

也支持英文标签 `Background:`、`Changes:`、`Validation:`，以及 Markdown 标题形式（例如 `## 背景`）。以下是假设修复已完成、测试已通过时的写法，不代表本规范修改执行过这些业务测试：

```text
fix(backend): 避免重复点赞导致计数增加

背景：
同一用户重复点赞时，帖子点赞数可能重复增加。

改动：
- 已点赞时直接返回成功。
- 仅在新增点赞记录成功后更新计数。

验证：
- 新增重复点赞测试，确认点赞数只增加一次。
- 在 backend 目录执行 go test ./...，通过。
```

验证必须如实填写。没有运行测试时写“未执行：具体原因”；不适用时说明原因和已做的检查，例如“未执行产品测试：仅修改文档，已人工核对命令和链接”。不要预填“通过”，也不要把计划运行的命令写成已执行结果。

### PR 描述

PR 标题使用相同的标题格式。正文汇总 PR 的最终改动，使用以下四个非空章节，不必逐条重复 commit 历史：

```markdown
## 背景
说明问题、触发条件和预期行为。

## 改动
说明本次交付的最终行为和主要改动。

## 验证
填写实际执行的命令及结果；未执行或不适用时说明原因。

## 影响与风险
说明影响范围、兼容性，以及迁移、部署或回滚要求。
无额外要求时，也需要明确说明。
```

英文对应标题是 `Background`、`Changes`、`Validation`、`Impact and risks`。验证部分遵循与 commit 相同的真实性要求。

### 模板与校验

- [`.gitmessage`](../.gitmessage) 提供 commit 模板；[PR 模板](../.github/pull_request_template.md) 在 GitHub 创建 PR 时提供正文结构。模板提示均为注释，需填写实际内容。
- 运行 `make hooks-install` 安装提交钩子并设置当前仓库的 commit 模板；之后不带 `-m` 的 `git commit` 会打开模板。克隆到新目录后需重新安装。安装不会覆盖已有的其他 hook 路径或 commit 模板。
- `git commit -m` 不会自动带出模板，仍需填写完整正文；本地钩子始终检查新提交的三个部分。
- `make check-commits MESSAGE_FILE=/path/to/message.txt` 检查一份提交说明；`make check-commits BASE_REF=origin/main` 检查分支新增提交。
- CI 检查新增提交、PR 标题和正文；修改 PR 标题或正文会重新触发校验。缺少章节、空章节、仅含注释、`TODO`、`TBD`、`待补充` 等占位内容会失败。放在代码块或 HTML 注释中的章节示例不能代替实际章节。
- 校验检查结构和非空内容，不判断测试结果是否真实。模块测试结果仍由对应 CI 任务验证；人工检查和未执行原因由评审核对。

标题规则仍从配置中的 `legacy_ref`（`2c4d661`）之后生效。新增正文规则从 `body_required_after`（`5accf0b`）之后生效：该提交及其已有祖先保留原正文，之后的新提交必须填写三个部分。新增正文生效点不改变原有标题检查范围；两个生效点都不能移动来豁免后续提交。

禁止独立 `docs` 提交的规则从 `docs_forbidden_after`（`c5107e24`）之后生效。该提交及其祖先中的 `docs` 提交保留原类型，仍接受原有标题和正文检查；其他新增提交均受新规则约束，包括分叉分支上的提交。此生效点固定，不得移动来豁免后续提交。本地提交钩子和 PR 标题立即使用新规则。

真实 merge commit 豁免，但普通提交仅写成 `Merge ...` 不会被放行。`--no-verify` 只能绕过本地钩子，PR 和手动运行中的 CI 仍会拦截不合规的新提交。PR 正文使用当前规则，直接推送 main 不触发本工作流。
