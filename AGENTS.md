# 仓库协作规则

## 项目结构

GoPulse 采用多模块结构。根 `Makefile` 的 `GO_MODULES` 和 `NPM_MODULES` 是模块清单的唯一来源。各 Go 模块独立维护 `go.mod`。

| 路径 | 职责 |
| --- | --- |
| `backend/business/` | 业务 API、异步通知、搜索索引及数据库迁移 |
| `frontend/web/` | Vue 3、TypeScript 和 Vite 用户前端，采用现代简洁主题 |
| `tools/ci/` | CI 模块选择、提交规范校验及工具测试 |
| `tools/make/` | 共享 Makefile 片段 |
| `docs/` | 技术说明和设计资料 |
| `.github/` | GitHub Actions 工作流及 PR 模板 |
| `.githooks/` | 本地 Git 钩子 |
| `VERSION` | 产品版本的唯一来源 |
| `.env.example` | 环境变量示例 |
| `.env`、`.run/` | 本地配置、运行数据和工具缓存，不作为源码提交 |

新增、移动或移除模块时，必须同步更新根 Makefile、路径引用和相关文档。业务 Go module 名称保持为 `github.com/Ray-ymq/GoPulse/backend`。

## 实现与配置

- 业务 Go 代码放在 `backend/business/`。日志使用 `internal/logging`，健康检查使用 `internal/platform`，各命令入口通过标准库处理信号和退出。
- 前端开发与验证按 [前端说明](frontend/web/README.md) 执行。接口契约以 [OpenAPI](backend/business/api/openapi.yaml) 为准。
- 配置示例统一维护在根 `.env.example`。调整配置时，同步核对代码中的读取方式、默认值和校验条件。`.env` 中的敏感值不得提交或写入日志。
- 单机容器部署入口为根 `compose.yml` 和 `deploy/README.md`。宿主机源码运行前需自行准备依赖。仓库不提供压测工具，历史压测资料不能作为可执行入口。新增运行或压测工具时，必须补充操作说明。
- runtimekit 及可观测平台已移除。不得将旧模块目录、旧 CI 标签或旧平台配置作为当前入口。

## 写作规则

撰写或改写自然语言前，必须读取并使用 `${CODEX_HOME:-$HOME/.codex}/skills/asd-ste100-zh/SKILL.md`。当前上下文已完整包含规则时可直接应用；上下文压缩后缺少规则时必须重新读取。技能无法读取时如实说明。

遵循用户指定的语言、格式和原文保留要求。中文及中英混合内容采用技能的中文规则；只有用户明确要求纯英文改写时才使用英文分支。创意和营销内容遵循技能的适用边界。

明确主体与动作，保留事实、条件、否定、范围、例外、顺序、数值、单位、术语、要求强度和置信度。代码、命令、路径、标识符和指定引用按要求保留，不补造原因、机制或指标。同一概念使用一致名称，多步骤操作按执行顺序列出，必要条件和风险放在对应动作前。

修改文档后必须回读实际文件，并逐项核对原意和事实逻辑。修正问题后再交付；回复、代码注释、错误信息、工具描述及提交说明同样适用。用户未要求时，不附加检查过程或合规声明。

## 验证入口

在仓库根目录按改动范围运行必要检查：

```sh
make ci-tools
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
make check MODULE=frontend/web
make test MODULE=frontend/web
make build MODULE=frontend/web
```

前端首次验证前执行 `make -C frontend/web install`。Go CI 命令使用 `.run/ci/go.work`，不得覆盖开发者的根 `go.work`。真实依赖联调需另行执行，不能用单元测试结果代替。

### 本地镜像重建

- 每次完成涉及系统运行的改动后，必须从仓库根目录主动执行 `make container-build`。触发范围包括前后端代码、应用依赖、数据库迁移、镜像构建文件和容器部署配置。修改 Compose 配置时，同时执行 `make container-config`。
- `make container-build` 重建两个项目镜像。`gopulse/web` 供 `web` 使用。`gopulse/backend` 供 `api`、`worker`、`indexer`、`migrate` 和 `search-init` 共用。MySQL、Redis、RabbitMQ 和 Elasticsearch 使用 Compose 指定的上游镜像。
- 构建后，使用 `docker image inspect` 按本次构建所用标签核对两个镜像。任务完成时记录构建结果和镜像 ID。构建失败时，说明受影响的镜像和阻塞原因。

## Git 与协作约定

- 修改代码、配置、文档或其他文件后，必须完成必要验证并创建 Git commit，不能只将修改留在工作区。
- 每个 commit 对应一项完整改动。只提交本次任务涉及的内容，不混入用户或其他任务已有的未提交修改。
- 新建开发分支统一使用 `develop/x.x.x`，版本号表示本轮计划发布的正式版本。该分支持续承载本轮全部开发工作，不因单个任务或提交完成而切换版本分支。本轮完成并合入主线后，再从最新主线创建下一轮版本分支。
- 提交标题采用 `<type>(<scope>): <subject>`，例如 `fix(config): 修正环境变量示例`。允许的提交类型以 `.conventional-commits.json` 和提交规范为准，不限于 `feat`、`fix`。
- 不得单独创建 `docs` 类型的 commit。文档修改随对应改动提交，类型使用对应改动的类型。
- 创建提交前阅读 [提交规范](tools/ci/CI.md#提交规范)。提交正文必须包含非空的“背景”“改动”和“验证”；PR 正文还必须包含“影响与风险”。验证结果必须如实填写。
- 完成任务时，在最终回复中说明 commit hash 和验证结果。提交失败时，必须说明原因和仍未提交的内容。

## 版本管理

- 完整规则统一维护在 [版本管理](docs/版本管理.md)。产品版本只使用 `x.y.z`，不使用 `rc` 等预发布后缀。
- 每项完整改动在创建 commit 前，根据提交说明更新根 `VERSION`，同时同步 OpenAPI、前端包及锁文件中的产品版本字段，并与代码放在同一个 commit 中。历史版本记录和依赖版本不得统一替换。
- 标题带 `!` 或正文声明 `BREAKING CHANGE:` / `BREAKING-CHANGE:` 时升级主版本；否则 `feat` 升级次版本，`fix`、`perf`、`revert` 升级补丁版本，其他类型保持版本。升级高位时将低位归零，每个 commit 只递增一次。
- 使用 `make version-plan` / `make version-update MESSAGE_FILE=<提交说明文件>` 预览和同步版本。计算以 HEAD 为基准，重复执行不会累加。使用 `make commit MESSAGE_FILE=<提交说明文件>` 自动同步暂存区版本并创建提交；提交钩子和 CI 检查版本递增及各版本字段的一致性。
- `develop/x.x.x` 保留本轮分支，不因逐次提交的版本变化而切换或重命名。后续新建分支仍按下一轮计划版本命名，名称冲突时不得覆盖已有分支。
- 实际发布时才创建对应版本标签，不得因更新版本文件而自动创建标签、推送或发布。已有历史分支、提交和标签保留。
