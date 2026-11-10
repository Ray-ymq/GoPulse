# 模块 CI

旧工作流和旧 CI 脚本已移除。现在只有 `.github/workflows/ci.yml`，负责模块检查、提交规范校验和自动创建 PR。不包含自动合并、删分支、缓存预热或发布。

## 什么时候运行

- 所有分支的 `push` 都会运行，包括直接推送 `main`。不监听标签推送；分支删除事件跳过检查任务。
- PR 创建、更新、重新打开、修改标题或正文、添加或移除标签时运行；模块测试使用 GitHub 生成的合并版本，提交规范检查使用 PR 的实际提交。
- 手动运行必须输入模块标签，支持逗号或空格分隔，不再默认全量检查。
- 不监听 `merge_group`，不支持合并队列检查。
- 同一种事件下，同一个 PR 或分支有新任务时，取消旧任务；每个任务有独立超时。同一分支已有 PR 时，推送会分别产生 push 和 PR 检查。

## 自动创建 PR

非默认分支 push 的 `CI result` 通过后，工作流向仓库当前默认分支创建普通 PR。默认分支推送、标签推送、分支删除、PR 事件和手动运行均不创建 PR。push 检查失败或取消时，也不创建 PR。

- 同一来源分支和目标分支已有开放 PR 时，复用该 PR，不覆盖人工修改的标题、正文或标签。已关闭或已合并的 PR 不参与复用；后续 push 有新的文件改动时可以创建新 PR。
- 分支相对默认分支没有新增提交，或没有文件改动时，跳过创建。
- 创建前核对来源分支的最新 SHA。分支已删除或已有新提交时，跳过旧任务，由新 push 的检查结果决定是否创建 PR。
- PR 标题使用 `chore(repo): 合并 <分支名>`。正文包含背景、部分提交摘要、本次 push 的 CI 运行链接和待人工评估的影响与风险。维护者需按最终改动完善说明；自动生成的验证记录只代表该次 push 检查的模块范围。
- 不自动添加 `ci:*` 标签。维护者仍需按整个 PR 的影响范围选择模块标签，或用 `ci:none` 明确跳过模块任务；未选择标签时，PR 的模块选择检查会失败。

模块检查任务保持只读权限。只有创建 PR 的独立任务获得 `contents: read` 和 `pull-requests: write` 权限；该任务不检出分支或执行项目脚本，只通过工作流中的内联脚本调用 GitHub API 查询和创建 PR。创建被拒绝或 API 返回其他错误时，任务失败并记录原因，不把失败报告为成功。

该流程使用内置 `GITHUB_TOKEN`，不需要新增 secret。仓库需在 Settings → Actions → General → Workflow permissions 中启用 **Allow GitHub Actions to create and approve pull requests**；组织策略也必须允许此操作。本次未修改远端仓库设置。配置说明见 [GitHub Actions 仓库设置](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)。

按 GitHub 当前规则，由 `GITHUB_TOKEN` 创建 PR 所产生的 `opened` 工作流会等待人工批准。若 PR 页面显示 **Approve workflows to run**，具有仓库写权限的维护者需批准运行。后续人工修改 PR 或标签仍按上述事件规则触发检查。相关限制见 [GITHUB_TOKEN 事件规则](https://docs.github.com/en/actions/concepts/security/github_token#when-github_token-triggers-workflow-runs)。

## 按事件选择模块

支持的模块来自根 Makefile 的 `GO_MODULES`；前端不进入 CI，`NPM_MODULES` 仅用于本地命令。

### 分支推送

`push` 没有 PR 标签，按以下规则选择模块：

- 普通推送比较事件中的 `before` 与 `after` 两个版本，选择文件直接所属的 Go 模块。一次推送中的多个提交、文件删除及跨模块移动都会计入；强制推送也比较两个版本的完整文件树，不使用共同祖先缩小范围。
- 新分支的 `before` 为全零 SHA，或旧 SHA 在检出历史中不可用时，检查全部已注册 Go 模块。无法解析新 SHA 或事件字段无效时，计划失败。
- 根 `Makefile`、`.golangci.yml`、`.conventional-commits.json`、`tools/devtools/toolchains.json`，以及 `.github/workflows/`、`tools/make/`、`tools/ci/` 中的非文档文件变化时，检查全部已注册 Go 模块。
- 其他变化只选择文件直接所属的模块，不自动增加依赖或使用方。例如仅修改 `backend/runtimekit/` 的 Go 代码时，只检查该模块；需要验证其他模块时，通过 PR 标签或手动运行指定。
- 文档、前端和未归属 Go 模块的其他文件不选择模块任务。若没有选中任何模块，CI 工具测试和提交规范检查仍然运行。文档包括 `docs/` 下的文件、扩展名为 `.md`、`.rst`、`.adoc` 的文件和根目录 `LICENSE`。

### PR 与手动运行

PR 使用人工选择的 `ci:<模块目录>` 标签，手动运行使用输入的标签。两者都不按路径补齐或扩大模块范围。

- `ci:backend/business`：只运行业务系统的检查、构建和单测。
- `ci:backend/observability/exporters/redis`：只运行 Redis exporter 的三个任务。
- 多个模块标签：只运行这些模块，不增加其他模块。
- `ci:backend/runtimekit`：只检查共享库本身，不自动检查使用它的模块。
- 根 Makefile、CI、公共配置变化：仍只运行贴了标签的模块，不自动全量检查。
- `ci:none`：明确不运行模块任务，适合仅前端、文档等改动；CI 工具测试和提交规范检查仍然运行。
- 没有模块标签也没有 `ci:none`、存在未知 `ci:` 标签、或 `ci:none` 与模块标签混用：选择失败，最终 CI 失败。

标签是 PR 与手动运行的模块开关。不自动打标签，不根据文件变化核对或补齐标签；维护者负责选择正确范围。漏贴某个模块的标签，就不会检查该模块。普通的 `bug`、`documentation` 等标签不参与选择。

仓库维护者需先在 GitHub 创建对应标签。本次只提供工作流与本地配置，不创建或修改远端标签。全部合法名称可以查看：

```sh
make list-ci-labels
```

这里只选择和验证模块，不做真实环境集成、浏览器端到端或容器运行。这些仍通过显式 Makefile 命令执行。

## 每个模块检查什么

每个被选中的 Go 模块运行三个独立并行任务，同一套命令可以在本地执行：

```sh
make ci-check MODULE=backend/business  # gofmt、go vet、golangci-lint
make ci-test MODULE=backend/business   # go test -race -count=1 ./...
make ci-build MODULE=backend/business  # 模块构建
```

前端按 `frontend/user/`（用户端）、`frontend/admin/`（管理端）和 `frontend/shared/`（共用组件与工具）组织。两个应用分别维护 npm 依赖和构建配置，不进入 CI。本地命令如下：

```sh
make check MODULE=frontend/user   # TypeScript 检查
make lint MODULE=frontend/user    # ESLint
make test MODULE=frontend/user    # Vitest
make build MODULE=frontend/user   # 类型检查、Vite 构建
```

管理端使用相同命令，将 `MODULE` 替换为 `frontend/admin`。开发环境依赖准备也使用这两个模块路径，例如 `make bootstrap MODULE=frontend/user`。

根 Makefile 分发到各模块 Makefile。Go lint 工具继续由 `tools/devtools/toolchains.json` 固定版本和校验和，但不再使用旧 CI 的 lint 基线豁免；发现问题会直接失败。

`ci-*` 命令使用 `.run/ci/go.work`，确保共享 Go 代码来自当前检出目录。不会覆盖开发者根目录的 `go.work`，也不会修改各模块的 `go.mod`。

`make ci-tools` 需要 Python 3 和 Node.js；自动创建 PR 的回归测试通过模拟 API 执行实际工作流脚本，不会访问远端或创建 PR。

查看指定标签的计划和复现完整模块检查：

```sh
make ci-tools
make ci-plan LABELS="ci:backend/business ci:backend/observability/marshaller"
make ci-plan LABELS=ci:none
make verify MODULE=backend/business
```

`ALL=1` 不再支持。`make verify BASE_REF=origin/main` 和 `make ci-plan BASE_REF=origin/main` 保留为本地差异工具：比较共同祖先到 HEAD，只选择直接变动的 Go 模块，不扩展依赖、不对公共文件全量兜底。GitHub 工作流通过事件数据选择模块，不使用这个本地入口；push 的比较范围和公共配置规则见上文。尚未提交的修改可以直接用 `MODULE=...` 运行检查。

## 合并门禁

建议在 GitHub 分支保护中将固定的 `CI result` 设置为必需检查。它要求模块选择和提交规范校验成功，且选中的 Check、Test、Build 全部成功；失败、取消、意外跳过都会失败。PR 与手动运行显式使用 `ci:none`，或 push 未选中模块时，允许模块任务正常跳过。

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
- 在 backend/business 目录执行 go test ./...，通过。
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

- [`.gitmessage`](../../.gitmessage) 提供 commit 模板；[PR 模板](../../.github/pull_request_template.md) 在 GitHub 创建 PR 时提供正文结构。模板提示均为注释，需填写实际内容。
- 运行 `make hooks-install` 安装提交钩子并设置当前仓库的 commit 模板；之后不带 `-m` 的 `git commit` 会打开模板。克隆到新目录后需重新安装。安装不会覆盖已有的其他 hook 路径或 commit 模板。
- `git commit -m` 不会自动带出模板，仍需填写完整正文；本地钩子始终检查新提交的三个部分。
- `make check-commits MESSAGE_FILE=/path/to/message.txt` 检查一份提交说明；`make check-commits BASE_REF=origin/main` 检查分支新增提交。
- CI 检查新增提交；PR 事件还检查 PR 标题和正文，修改 PR 标题或正文会重新触发校验。push 检查 `before` 到 `after` 的全部新增非 merge 提交。新建功能分支或其旧 SHA 不可用时，优先排除 `origin/<默认分支>` 已接受的历史；默认分支不可用时，检查 `after` 可达历史中受提交规范约束的提交。默认分支自身的推送不会用自身的新版本排除提交。缺少章节、空章节、仅含注释、`TODO`、`TBD`、`待补充` 等占位内容会失败。放在代码块或 HTML 注释中的章节示例不能代替实际章节。
- 校验检查结构和非空内容，不判断测试结果是否真实。模块测试结果仍由对应 CI 任务验证；人工检查和未执行原因由评审核对。

标题规则仍从配置中的 `legacy_ref`（`2c4d661`）之后生效。新增正文规则从 `body_required_after`（`5accf0b`）之后生效：该提交及其已有祖先保留原正文，之后的新提交必须填写三个部分。新增正文生效点不改变原有标题检查范围；两个生效点都不能移动来豁免后续提交。

禁止独立 `docs` 提交的规则从 `docs_forbidden_after`（`c5107e24`）之后生效。该提交及其祖先中的 `docs` 提交保留原类型，仍接受原有标题和正文检查；其他新增提交均受新规则约束，包括分叉分支上的提交。此生效点固定，不得移动来豁免后续提交。本地提交钩子和 PR 标题立即使用新规则。

真实 merge commit 豁免，但普通提交仅写成 `Merge ...` 不会被放行。`--no-verify` 只能绕过本地钩子，push、PR 和手动运行中的 CI 仍会拦截不合规的新提交。PR 正文使用当前规则，直接推送 main 也会触发本工作流。

perflab 模块的 test/race 命令还运行公共流程的 Python、JavaScript、配置和归档回归。CI 在该模块的测试任务中创建独立 Python 虚拟环境，按 `tools/perflab/requirements.txt` 安装固定版本依赖，避免依赖 runner 预装包。本地缺少依赖时，先创建虚拟环境，再使用其中的 Python 运行模块测试：

```sh
python3 -m venv .run/ci/python
.run/ci/python/bin/python -m pip install -r tools/perflab/requirements.txt
PATH="$PWD/.run/ci/python/bin:$PATH" make ci-test MODULE=tools/perflab
```

默认 fixture 不访问业务环境。真实 k6 容器 fixture 通过 `GOPULSE_PT_K6_FIXTURE=1` 显式执行，真实专用业务环境的缩短全流程验收仍需单独完成，不由模块 CI 自动启动。
