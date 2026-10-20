# 仓库协作规则

## 项目结构

本仓库采用多模块结构，Go 服务和工具分别维护各自的 `go.mod`，前端统一放在 `frontend/` 下。

```text
GoPulse/
├── backend/          # 业务 API、异步任务、搜索索引及数据库迁移
├── frontend/         # 前端代码，使用 Vue 3、TypeScript 和 Vite
│   ├── user/         # 用户前端
│   ├── admin/        # 管理前端
│   └── shared/       # 两个前端共用的组件与工具
├── monitor/          # 指标采集调度、日志与事件接收、插件管理
├── marshaller/       # 可观测数据消费、清洗、转换及存储写入
├── exporters/        # 各基础组件的指标采集插件，分别为独立 Go 模块
├── runtimekit/       # 多个 Go 模块共用的运行时能力与指标契约
├── devtools/         # 开发环境、工具链、构建、验证及插件打包工具
├── perflab/          # 压测工具、负载脚本、容量配置及报告处理
├── PT/               # 压测说明、方案和报告
├── deploy/           # Dockerfile、Compose 配置及自监控部署资源
├── ci/               # CI 模块选择、提交规范校验及相关测试
├── make/             # 共享 Makefile 片段
├── docs/             # 技术文档、CI 规范及通用资料
├── .github/          # GitHub Actions 工作流及 PR 模板
├── .githooks/        # 本地 Git 钩子
├── Makefile          # 根命令入口及模块注册
├── CONTRIBUTING.md   # 贡献说明与开发入口
├── VERSION           # 产品版本的唯一来源
└── .env.example      # 环境变量配置示例
```

- 模块清单以根 Makefile 的 `GO_MODULES` 和 `NPM_MODULES` 为准；新增、移动或移除模块时，同步更新构建入口、路径引用和相关文档。
- 用户前端和管理前端分别维护各自业务，共用组件与工具放入 `frontend/shared/`。
- Go 业务逻辑放在所属模块，共享能力放入已有公共模块，避免跨模块复制实现。
- Exporter 由 Monitor 作为二进制插件启动，构建与打包沿用现有工具链。
- `.run/` 为本地运行和工具缓存目录，不作为源码维护；实际环境配置中的敏感值不得提交。

## Git 与协作约定
- 任何代码、配置、文档或其他文件修改完成后，都必须完成必要验证并创建 Git commit，不能只将修改留在工作区。
- 提交信息格式：`<type>(<scope>): <subject>`，如 `feat(auth): add SSO login`。分支命名：`feat/`、`fix/`、`chore/` 等，前缀 + 简短描述。
- 每个 commit 对应一项完整改动；只提交本次任务涉及的内容，不混入用户或其他任务已有的未提交修改。
- 提交说明必须遵循 [提交规范](ci/CI.md#提交规范)：使用合规标题，并填写非空的背景、改动和验证，验证结果必须如实记录。
- 完成任务时，在最终回复中说明 commit hash 和验证结果；若提交失败，明确说明原因和仍未提交的内容。
