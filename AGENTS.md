# 仓库协作规则

## 项目结构

本仓库采用多模块结构，Go 模块分别维护各自的 `go.mod`。业务系统和共享能力统一放在 `backend/`，前端统一放在 `frontend/`，工程工具统一放在 `tools/`。

```text
GoPulse/
├── backend/                 # 后端系统与共享能力
│   └── business/            # 业务 API、异步任务、搜索索引及数据库迁移
├── frontend/                # Vue 3、TypeScript 和 Vite 前端
│   └── web/                 # 纸墨风格用户前端
├── tools/                   # 工程与测试工具
│   ├── ci/                  # CI 模块选择、提交规范校验及相关测试
│   └── make/                # 共享 Makefile 片段
├── docs/                    # 技术文档及通用资料
│   └── performance/         # 压测方案、报告和摘要
├── .github/                 # GitHub Actions 工作流及 PR 模板
├── .githooks/               # 本地 Git 钩子
├── Makefile                 # 根命令入口及模块注册
├── CONTRIBUTING.md          # 贡献说明与开发入口
├── VERSION                  # 产品版本的唯一来源
└── .env.example             # 环境变量配置示例
```

- 模块清单以根 Makefile 的 `GO_MODULES` 和 `NPM_MODULES` 为准；新增、移动或移除模块时，同步更新构建入口、路径引用和相关文档。
- 用户前端位于 `frontend/web/`，通过 `NPM_MODULES` 注册；开发与验证说明见 `frontend/web/README.md`。部署配置、本地开发编排和压测工具已移除，重建前需自行准备运行环境。
- Go 业务逻辑放在所属模块，日志和健康检查分别由业务模块内的 `internal/logging`、`internal/platform` 提供，信号和退出由各命令入口通过标准库处理。
- 业务 Go module 名称保持不变；模块清单由根 Makefile 指定。runtimekit 模块及其依赖已删除。
- 历史压测资料不作为当前可执行入口；新增压测工具时同步补充操作说明。
- `.run/` 为本地运行和工具缓存目录，不作为源码维护；实际环境配置中的敏感值不得提交。
- 环境变量示例统一维护在根目录 `.env.example`。

## Git 与协作约定
- 任何代码、配置、文档或其他文件修改完成后，都必须完成必要验证并创建 Git commit，不能只将修改留在工作区。
- 提交信息格式：`<type>(<scope>): <subject>`，如 `feat(auth): add SSO login`。分支命名：`feat/`、`fix/`、`chore/` 等，前缀 + 简短描述。
- 每个 commit 对应一项完整改动；只提交本次任务涉及的内容，不混入用户或其他任务已有的未提交修改。
- 不得单独创建 `docs` 类型的 commit；文档修改必须随对应改动一并提交，提交类型使用对应改动的类型。
- 提交说明必须遵循 [提交规范](tools/ci/CI.md#提交规范)：使用合规标题，并填写非空的背景、改动和验证，验证结果必须如实记录。
- 完成任务时，在最终回复中说明 commit hash 和验证结果；若提交失败，明确说明原因和仍未提交的内容。
