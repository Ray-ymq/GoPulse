# 仓库协作规则

## 项目结构

本仓库采用多模块结构，Go 服务和工具分别维护各自的 `go.mod`。业务系统和共享能力统一放在 `backend/`，前端统一放在 `frontend/`，工程工具统一放在 `tools/`。

```text
GoPulse/
├── backend/                 # 后端系统与共享能力
│   ├── business/            # 业务 API、异步任务、搜索索引及数据库迁移
│   └── runtimekit/          # 后端共享运行时能力
├── frontend/                # Vue 3、TypeScript 和 Vite 前端
│   ├── user/                # 用户前端
│   └── shared/              # 前端共用组件与工具
├── tools/                   # 工程与测试工具
│   ├── devtools/            # 开发环境、工具链、构建、验证
│   ├── perflab/             # 压测工具、负载脚本、容量配置及报告处理
│   ├── ci/                  # CI 模块选择、提交规范校验及相关测试
│   └── make/                # 共享 Makefile 片段
├── deploy/                  # Dockerfile、Compose 配置及部署资源
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
- 用户前端放入 `frontend/user/`；共用组件与工具放入 `frontend/shared/`。
- 正式压测使用 `make pt`，执行条件见 `tools/perflab/flow/README.md`。
- Go 业务逻辑放在所属模块，后端共享能力放入 `backend/runtimekit/`，避免跨模块复制实现。
- Go module 名称及 import 路径保持不变；本地模块目录由根 Makefile 和各模块的 `replace` 指令指定。
- 压测方案和报告统一放在 `docs/performance/`，压测工具与配置放在 `tools/perflab/`。
- `.run/` 为本地运行和工具缓存目录，不作为源码维护；实际环境配置中的敏感值不得提交。
- Compose 部署入口放在 `deploy/compose/`，本地开发、调试和追踪覆盖配置放在 `deploy/dev/`，Dockerfile 放在 `deploy/docker/`；环境变量示例统一维护在根目录 `.env.example`，使用说明见 `deploy/README.md`。

## Git 与协作约定
- 任何代码、配置、文档或其他文件修改完成后，都必须完成必要验证并创建 Git commit，不能只将修改留在工作区。
- 提交信息格式：`<type>(<scope>): <subject>`，如 `feat(auth): add SSO login`。分支命名：`feat/`、`fix/`、`chore/` 等，前缀 + 简短描述。
- 每个 commit 对应一项完整改动；只提交本次任务涉及的内容，不混入用户或其他任务已有的未提交修改。
- 不得单独创建 `docs` 类型的 commit；文档修改必须随对应改动一并提交，提交类型使用对应改动的类型。
- 提交说明必须遵循 [提交规范](tools/ci/CI.md#提交规范)：使用合规标题，并填写非空的背景、改动和验证，验证结果必须如实记录。
- 完成任务时，在最终回复中说明 commit hash 和验证结果；若提交失败，明确说明原因和仍未提交的内容。
