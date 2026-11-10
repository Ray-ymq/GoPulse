# GoPulse 后端

`backend/` 包含业务系统、可观测系统和共享运行时能力。业务服务、可观测服务及 Go 采集插件分别维护各自的 `go.mod` 和 `Makefile`。

本文中的命令均在仓库根目录执行。模块清单以[根 Makefile](../Makefile) 的 `GO_MODULES` 为准，可通过 `make list-go-modules` 查看。

## 目录结构

```text
backend/
├── business/                     # 业务 API、异步任务和管理接口
│   ├── api/openapi.yaml          # HTTP API 定义
│   ├── cmd/                      # 服务、Worker、Indexer 及管理工具入口
│   ├── internal/                 # 业务逻辑、存储访问和服务配置
│   └── migrations/               # 数据库迁移文件与嵌入逻辑
├── observability/
│   ├── monitor/                  # 插件管理、采集调度、日志与事件接收
│   │   ├── cmd/monitor/          # Monitor 服务入口
│   │   └── internal/             # 采集、插件管理、缓冲与消息发布
│   ├── marshaller/               # 数据消费、校验、转换及存储写入
│   │   ├── cmd/marshaller/       # Marshaller 服务入口
│   │   └── internal/             # Kafka 消费、数据转换和存储客户端
│   └── exporters/
│       ├── elasticsearch/        # Elasticsearch 采集插件
│       ├── kafka/                # Kafka 采集插件
│       ├── mysql/                # MySQL 采集插件
│       ├── node/                 # 官方 node_exporter 元数据与打包入口
│       ├── rabbitmq/             # RabbitMQ 采集插件
│       ├── redis/                # Redis 采集插件
│       ├── victoriametrics/      # VictoriaMetrics 采集插件
│       └── std_exporter/         # Go 采集插件开发模板
├── runtimekit/                   # 后端共享运行时能力
│   ├── cmd/container-exporter/   # 容器资源采集器入口
│   ├── containerstats/           # 容器资源指标采集
│   ├── exportercontract/         # 插件与进程管理器之间的配置契约
│   └── metriccontract/           # 指标传输校验与单位契约
└── README.md
```

## 模块职责

各一级子目录的说明见 [business](business/README.md)、[observability](observability/README.md) 和 [runtimekit](runtimekit/README.md)。

| 模块 | 职责 |
| --- | --- |
| [business](business/) | 用户、帖子、评论、点赞、收藏、搜索、通知，以及可观测数据查询和管理接口 |
| [monitor](observability/monitor/) | 管理插件进程，调度指标采集，接收日志，记录事件，并将数据发布到 Kafka |
| [marshaller](observability/marshaller/) | 消费 Kafka 中的指标、日志和事件，完成校验与转换；指标写入 VictoriaMetrics，日志和事件写入 Elasticsearch |
| [exporters](observability/exporters/) | 采集被监控组件的指标，由 Monitor 作为独立二进制插件启动 |
| [runtimekit](runtimekit/) | 提供配置校验、日志、健康探针、进程指标、信号处理和退出流程等共享能力 |

业务异步任务通过 RabbitMQ 交给 Worker 和搜索 Indexer 处理。可观测数据通过 Monitor、Kafka 和 Marshaller 进入存储。RabbitMQ 与 Kafka 分别承担这两条数据链路中的消息传递职责。

`business/cmd/server` 支持通过 `BACKEND_SERVICE_ROLE` 选择 API 职责：`combined` 为默认值，同时启用业务和平台职责；`business` 启用业务职责；`platform` 启用平台职责。部署方式见[部署说明](../deploy/README.md)。

### 主要程序入口

| 入口 | 用途 |
| --- | --- |
| `business/cmd/server` | 启动 HTTP API 服务 |
| `business/cmd/business-worker` | 消费业务异步任务 |
| `business/cmd/search-indexer` | 消费搜索索引更新任务 |
| `business/cmd/search-reindex` | 重建搜索索引 |
| `business/cmd/migrate` | 校验迁移文件、查询迁移状态及执行数据库迁移 |
| `business/cmd/admin-role` | 管理管理员角色 |
| `observability/monitor/cmd/monitor` | 启动 Monitor |
| `observability/marshaller/cmd/marshaller` | 启动 Marshaller |
| `runtimekit/cmd/container-exporter` | 启动容器资源采集器 |

## 本地开发

后端 Go 模块要求 Go 1.26。完整开发环境还需要 Node.js、npm、Python、Docker、Docker Compose v2、Buildx、Git 和 Make；版本要求见[项目说明](../README.md)和[工具链配置](../tools/devtools/toolchains.json)。

先检查并准备环境：

```sh
make doctor
make bootstrap
```

环境变量示例统一维护在[.env.example](../.env.example)。实际本地配置保存在 `.env`，敏感值不得提交。

启动业务开发环境：

```sh
make dev
```

该命令准备开发依赖，启动后端 API、业务 Worker、搜索 Indexer 和用户前端。

需要同时开发可观测功能时，执行：

```sh
make dev-observe
```

该命令在业务开发环境基础上增加 Monitor、Marshaller 和管理前端。

停止当前工作区的开发进程和依赖：

```sh
make stop
```

## 检查、测试与构建

使用 `MODULE` 指定完整模块路径。例如，检查业务模块：

```sh
make check MODULE=backend/business
make lint MODULE=backend/business
make test MODULE=backend/business
make race MODULE=backend/business
make build MODULE=backend/business
```

`check` 检查 Go 格式并运行 `go vet`；`lint` 运行 golangci-lint；`test` 运行模块测试；`race` 在测试时启用竞态检测；`build` 构建指定模块。

其他模块使用相同入口，例如：

```sh
make test MODULE=backend/observability/monitor
make build MODULE=backend/observability/marshaller
make test MODULE=backend/runtimekit
make test MODULE=backend/observability/exporters/mysql
```

完整验证单个模块：

```sh
make verify MODULE=backend/business
```

`verify` 执行与模块 CI 一致的检查、竞态测试和构建。PR 和手动运行按 `ci:<模块目录>` 标签选择模块；push 按文件变更选择模块，新分支或公共 CI 配置变更等情况会检查全部 Go 模块。修改 `runtimekit` 不会自动增加其使用方的检查，需要按影响范围通过 PR 标签或手动运行补充验证。详细规则见[CI 说明](../tools/ci/CI.md)。

## 采集插件

六个 Go 采集插件和 `std_exporter` 模板分别维护独立模块。`node/` 使用官方 node_exporter 发布包，维护上游元数据、配置 schema、版本和许可文件，不包含 Go 模块。

插件统一通过根目录入口打包：

```sh
make package-plugin SOURCE=mysql ARCH=amd64
make package-plugin SOURCE=node ARCH=amd64
```

`SOURCE` 指定插件目录名，`ARCH` 指定目标架构。开发新 Go 采集插件时参考 [std_exporter 模板](observability/exporters/std_exporter/)；指标单位见[指标单位参考文档](observability/exporters/std_exporter/docs/metric_units.md)。

## 维护约定与相关文档

业务逻辑放在所属模块，后端共享能力放在 `runtimekit/`。调整本地目录时，保留既有 Go module 名称和 import 路径，并同步更新根 Makefile、模块 `replace` 指令及相关路径引用。

部署配置位于 `deploy/`，工程工具位于 `tools/`，本地运行状态和缓存位于 `.run/`。正式压测使用根目录 `make pt`，执行前必须按压测操作说明准备专用环境、数据和观测资源。

历史压测方案与报告已移至本地 `~/performance/`，Outbox 容量运行契约已移至 `~/Outbox容量运行契约.md`。

- [贡献说明](../CONTRIBUTING.md)
- [部署说明](../deploy/README.md)
- [压测操作说明](../tools/perflab/flow/README.md)
