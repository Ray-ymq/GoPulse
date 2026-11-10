# 可观测系统

本目录包含 Monitor、Marshaller 和采集插件。Monitor 负责采集与接收数据，Kafka 负责传输，Marshaller 负责消费、转换及存储写入。

`observability/` 本身不包含 `go.mod`。Monitor、Marshaller 和各 Go 采集插件分别维护独立模块。本文中的命令均在仓库根目录执行，整体开发要求见[后端 README](../README.md)。

## 目录结构

```text
observability/
├── monitor/
│   ├── cmd/monitor/          # 服务入口
│   └── internal/             # 插件管理、采集、日志接收、事件与缓冲
├── marshaller/
│   ├── cmd/marshaller/       # 服务入口
│   └── internal/             # Kafka 消费、数据转换、存储与保留策略
└── exporters/
    ├── elasticsearch/        # Elasticsearch 采集插件
    ├── kafka/                # Kafka 采集插件
    ├── mysql/                # MySQL 采集插件
    ├── node/                 # 官方 node_exporter 元数据与打包入口
    ├── rabbitmq/             # RabbitMQ 采集插件
    ├── redis/                # Redis 采集插件
    ├── victoriametrics/      # VictoriaMetrics 采集插件
    └── std_exporter/         # Go 采集插件开发模板
```

## 组件职责

| 组件 | 职责与实现入口 |
| --- | --- |
| [Monitor](monitor/cmd/monitor/main.go) | 管理插件进程，调度指标采集，接收应用日志，记录运行事件，并通过本地缓冲发布到 Kafka |
| [Marshaller](marshaller/cmd/marshaller/main.go) | 消费指标、日志和事件，校验消息，转换数据并写入存储；处理日志与事件保留策略 |
| [Exporters](exporters/) | 采集被监控组件指标，由 Monitor 作为独立二进制插件启动 |

查询、告警和插件管理等面向前端的 API 位于[业务模块](../business/README.md)。进程健康、内部指标和插件配置契约由 [runtimekit](../runtimekit/README.md) 提供。

### 数据流

Monitor 将三类数据发布到不同 Kafka topic，Marshaller 分别消费并写入对应存储。

| 数据 | Kafka topic | 最终存储 |
| --- | --- | --- |
| 指标 | `gopulse-observability-metrics` | VictoriaMetrics |
| 日志 | `gopulse-observability-logs` | Elasticsearch |
| 事件 | `gopulse-observability-events` | Elasticsearch |

消息发布定义见 [Monitor publisher](monitor/internal/metrics/publisher/publisher.go)，消费与写入装配见 [Marshaller 入口](marshaller/cmd/marshaller/main.go)。

## 配置与本地开发

环境变量统一参考根目录[.env.example](../../.env.example)。Monitor 和 Marshaller 分别使用 `MONITOR_*`、`MARSHALLER_*` 配置；运行模式与实例身份使用 `GOPULSE_RUNTIME_MODE`、`GOPULSE_INSTANCE_ID` 等共享配置。

| 服务 | 主要配置范围 | 校验实现 |
| --- | --- | --- |
| Monitor | HTTP、管理与日志接收凭据、插件目录、采集周期、本地缓冲和 Kafka 发布 | [monitor/internal/config](monitor/internal/config/) |
| Marshaller | HTTP、API 凭据、Kafka 消费、VictoriaMetrics、Elasticsearch、重试及保留策略 | [marshaller/internal/config](marshaller/internal/config/) |

实际本地配置保存在根目录 `.env`，插件运行状态和工具缓存由开发或部署入口管理，不提交敏感配置。

准备并启动源码开发环境：

```sh
make doctor
make bootstrap
make dev-observe
```

该入口启动业务开发环境，并增加 Monitor、Marshaller 和管理前端。停止当前工作区的开发环境使用 `make stop`。Compose 与 Kubernetes 产品部署见[部署说明](../../deploy/README.md)。

## 插件开发与打包

六个 Go 采集插件及 `std_exporter` 模板分别维护自己的 `go.mod`。`node/` 不包含 Go 模块，使用官方 node_exporter 发布包，并维护版本、上游元数据、配置 schema 和许可文件。

在仓库根目录打包插件：

```sh
make package-plugin SOURCE=mysql ARCH=amd64
make package-plugin SOURCE=node ARCH=amd64
```

`SOURCE` 指定插件目录名，`ARCH` 指定目标架构。新 Go 采集插件参考 [std_exporter](exporters/std_exporter/)，指标单位参考[单位说明](exporters/std_exporter/docs/metric_units.md)。插件与管理器的配置协议见 [exportercontract](../runtimekit/exportercontract/)，指标传输校验见 [metriccontract](../runtimekit/metriccontract/)。

## 检查与构建

为每个独立模块指定完整路径：

```sh
make check MODULE=backend/observability/monitor
make test MODULE=backend/observability/monitor
make build MODULE=backend/observability/monitor
make check MODULE=backend/observability/marshaller
make test MODULE=backend/observability/marshaller
make build MODULE=backend/observability/marshaller
make test MODULE=backend/observability/exporters/mysql
```

上述 Go 模块也支持 `make lint MODULE=...` 和 `make race MODULE=...`。完整验证使用 `make verify MODULE=backend/observability/monitor` 或 `make verify MODULE=backend/observability/marshaller`。模块路径以根 Makefile 的 `GO_MODULES` 为准。

PR 按完整文件差异选择模块，并在检查通过后同步 `ci:*` 标签；手动运行只检查输入标签选中的模块。push 按本次推送的文件变更选择模块，新分支或公共 CI 配置变更等情况会检查全部 Go 模块。调整共享契约或跨组件数据格式时，需要按影响范围手动运行补充验证。详细规则见[CI 说明](../../tools/ci/CI.md)。

## 相关文档

- [贡献说明](../../CONTRIBUTING.md)
- [公共采集与指标责任](../../tools/perflab/diagnostics/README.md)
- [压测操作说明](../../tools/perflab/flow/README.md)
- [自监控部署](../../deploy/selfmonitor/README.md)
