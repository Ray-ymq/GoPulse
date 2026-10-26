# 仓库协作规则

## 项目结构

本仓库采用多模块结构，Go 服务和工具分别维护各自的 `go.mod`。业务系统和可观测系统统一放在 `backend/`，前端统一放在 `frontend/`，工程工具统一放在 `tools/`。

```text
GoPulse/
├── backend/                 # 后端系统与共享能力
│   ├── business/            # 业务 API、异步任务、搜索索引及数据库迁移
│   ├── observability/       # 可观测系统
│   │   ├── monitor/         # 采集调度、日志与事件接收、插件管理
│   │   ├── marshaller/      # 数据消费、清洗、转换及存储写入
│   │   └── exporters/       # 采集插件源码、原生插件元数据与插件模板
│   └── runtimekit/          # 后端共享运行时能力与指标契约
├── frontend/                # Vue 3、TypeScript 和 Vite 前端
│   ├── user/                # 用户前端
│   ├── admin/               # 管理前端
│   └── shared/              # 前端共用组件与工具
├── tools/                   # 工程与测试工具
│   ├── devtools/            # 开发环境、工具链、构建、验证及插件打包
│   ├── perflab/             # 压测工具、负载脚本、容量配置及报告处理
│   ├── ci/                  # CI 模块选择、提交规范校验及相关测试
│   └── make/                # 共享 Makefile 片段
├── deploy/                  # Dockerfile、Compose 配置及自监控部署资源
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
- 用户前端和管理前端分别维护各自业务，共用组件与工具放入 `frontend/shared/`。
- Go 业务逻辑放在所属模块，后端共享能力放入 `backend/runtimekit/`，避免跨模块复制实现。
- Go module 名称及 import 路径保持不变；本地模块目录由根 Makefile 和各模块的 `replace` 指令指定。
- 压测方案和报告统一放在 `docs/performance/`，压测工具与配置放在 `tools/perflab/`。
- Exporter 由 Monitor 作为二进制插件启动。Go 采集器分别维护独立模块；`node/` 维护官方 node_exporter 发布包的元数据，不包含 Go 模块；`std_exporter/` 是插件模板。
- 插件构建与打包统一通过根目录 `make package-plugin SOURCE=...`，正式压测使用 `make pt`，执行条件见 `tools/perflab/flow/README.md`。
- `.run/` 为本地运行和工具缓存目录，不作为源码维护；实际环境配置中的敏感值不得提交。

## Git 与协作约定
- 任何代码、配置、文档或其他文件修改完成后，都必须完成必要验证并创建 Git commit，不能只将修改留在工作区。
- 提交信息格式：`<type>(<scope>): <subject>`，如 `feat(auth): add SSO login`。分支命名：`feat/`、`fix/`、`chore/` 等，前缀 + 简短描述。
- 每个 commit 对应一项完整改动；只提交本次任务涉及的内容，不混入用户或其他任务已有的未提交修改。
- 不得单独创建 `docs` 类型的 commit；文档修改必须随对应改动一并提交，提交类型使用对应改动的类型。
- 提交说明必须遵循 [提交规范](tools/ci/CI.md#提交规范)：使用合规标题，并填写非空的背景、改动和验证，验证结果必须如实记录。
- 完成任务时，在最终回复中说明 commit hash 和验证结果；若提交失败，明确说明原因和仍未提交的内容。
