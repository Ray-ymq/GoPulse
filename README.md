# GoPulse

GoPulse 包含业务系统与可观测系统，提供社交业务功能、指标采集、日志与事件处理，以及配套的开发和压测工具。

## 功能

| 系统 | 主要功能 |
| --- | --- |
| 业务系统 | 用户、帖子、评论、点赞、收藏、搜索和异步通知 |
| 可观测系统 | 插件管理、指标采集、日志与事件接收、数据处理及存储 |
| 工程工具 | 本地开发环境、构建、检查、插件打包和压测 |

可观测系统由三个部分组成：

- **Exporters**：采集 MySQL、Redis、Kafka 等组件的指标；Node 指标使用官方 node_exporter。
- **Monitor**：管理采集插件、调度指标采集，并接收日志和事件。
- **Marshaller**：消费可观测数据，完成校验、转换及存储写入。

## 技术栈

| 分类 | 技术 |
| --- | --- |
| 后端 | Go、Gin |
| 前端 | Vue 3、TypeScript、Vite |
| 数据与缓存 | MySQL、Redis |
| 消息 | RabbitMQ、Kafka |
| 搜索与日志 | Elasticsearch |
| 指标存储 | VictoriaMetrics |
| 链路追踪 | OpenTelemetry |
| 部署与 CI | Docker、Docker Compose、GitHub Actions |

详细版本见[技术栈说明](docs/GoPulse%20技术栈.md)。

## 目录结构

```text
GoPulse/
├── backend/                  # 后端系统与共享模块
│   ├── business/             # 业务 API、异步任务、搜索索引及数据库迁移
│   ├── observability/        # 可观测系统
│   │   ├── monitor/          # 采集调度、日志与事件接收、插件管理
│   │   ├── marshaller/       # 数据消费、处理与存储写入
│   │   └── exporters/        # 采集插件、原生插件元数据及开发模板
│   └── runtimekit/          # 后端共享运行时能力与指标契约
├── frontend/
│   ├── user/                # 用户前端
│   ├── admin/               # 管理前端
│   └── shared/              # 前端共用组件与工具
├── tools/
│   ├── devtools/            # 开发环境、工具链、构建及插件打包
│   ├── perflab/             # 压测工具、配置与分析
│   ├── ci/                  # CI 模块选择和提交规范校验
│   └── make/                # 共享 Makefile 规则
├── deploy/                  # Docker、Compose 及观测部署配置
├── docs/                    # 技术文档
│   ├── CONTRIBUTING.md      # 贡献指南
│   └── performance/         # 压测方案与报告
├── .github/                 # GitHub Actions 工作流和 PR 模板
├── .githooks/               # 本地 Git 提交检查
├── .run/                    # 本地运行数据与缓存，不作为源码提交
├── AGENTS.md                # 仓库协作规则
├── README.md                # 项目介绍与开发入口
├── Makefile                 # 开发、检查、构建和运行命令入口
├── VERSION                  # 产品版本号
└── LICENSE                  # MIT 许可证
```

Go 模块分别维护自己的 `go.mod`。模块清单以根 `Makefile` 中的 `GO_MODULES` 和 `NPM_MODULES` 为准。

`README.md` 留在所属目录，其他说明文档放在所属目录的 `docs/`。跨模块文档放在根目录 `docs/`。仅在有补充文档时创建 `docs/`，已有 `docs/` 内不再重复嵌套。`AGENTS.md` 和 `.github/pull_request_template.md` 保留工具约定的位置。

## 本地开发

### 环境要求

- Go 1.26
- Node.js 24，最低 24.20.0
- npm 11，最低 11.19.0
- Python 3.12 或更新版本
- Docker、Docker Compose v2 和 Buildx
- Git、Make

工具链要求见 [toolchains.json](tools/devtools/toolchains.json)。

### 准备环境

在仓库根目录执行：

```sh
make doctor
make bootstrap
```

`.env.example` 提供环境配置示例；`.env` 用于保存本地实际配置。示例中的凭据仅用于开发环境。

### 启动业务系统

```sh
make dev
```

该命令启动业务开发环境，包括后端 API、业务 Worker、搜索 Indexer 和用户前端。

### 启动可观测开发环境

```sh
make dev-observe
```

该命令在业务开发环境基础上增加 Monitor、Marshaller 和管理前端。

### 停止开发环境

```sh
make stop
```

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `make help` | 查看可用命令 |
| `make doctor` | 检查本地环境 |
| `make bootstrap` | 准备开发依赖 |
| `make check-all` | 执行所有模块的格式与静态检查 |
| `make lint-all` | 执行所有模块的 lint |
| `make build-programs` | 构建程序与前端 |
| `make check-compose` | 检查 Compose 配置 |
| `make ci-tools` | 测试 CI 选择与提交校验工具 |

检查或构建单个模块时，使用完整模块路径：

```sh
make test MODULE=backend/business
make race MODULE=backend/observability/monitor
make lint MODULE=frontend/user
make build MODULE=frontend/admin
make verify MODULE=tools/devtools
```

## 插件开发

插件通过 Monitor 作为独立进程启动。现有插件及开发模板位于 `backend/observability/exporters/`。

在仓库根目录打包插件：

```sh
make package-plugin SOURCE=mysql ARCH=amd64
```

开发协议、目录约定与验证方法见[Exporter 开发说明](backend/observability/exporters/README.md)。官方 node_exporter 的使用方式见[Node Exporter 说明](backend/observability/exporters/node/README.md)。

## 压测

压测工具位于 `tools/perflab/`，方案与报告位于 `docs/performance/`。

执行正式压测前，先按[压测操作说明](tools/perflab/flow/README.md)准备专用环境、数据和观测资源，再运行：

```sh
make pt
```

## 配置文件

| 文件 | 用途 |
| --- | --- |
| `.env` | 本地实际环境配置，不提交 Git |
| `.env.example` | 环境配置示例 |
| `.gitignore` | Git 忽略规则 |
| `.gitattributes` | 文件换行等 Git 属性 |
| `.dockerignore` | Docker 构建上下文排除规则 |
| `.editorconfig` | 编辑器格式约定 |
| `.gitmessage` | Git 提交说明模板 |
| `.conventional-commits.json` | 提交类型与历史校验规则 |
| `.golangci.yml` | Go lint 配置 |

## 文档与贡献

- [贡献指南](docs/CONTRIBUTING.md)
- [仓库协作规则](AGENTS.md)
- [CI 与提交规范](tools/ci/docs/CI.md)
- [HTTP API 定义](backend/business/api/openapi.yaml)
- [压测方案与报告](docs/performance/README.md)

提交信息使用 Conventional Commits，正文填写非空的“背景、改动、验证”。验证结果必须与实际执行情况一致。

## 许可证

本项目采用 [MIT License](LICENSE)。
