# 业务系统

本模块包含社交业务 API、异步任务、搜索索引，以及可观测平台的查询和管理接口。Go module 名称为 `github.com/Ray-ymq/GoPulse/backend`，要求 Go 1.26。

本文中的命令均在仓库根目录执行。后端整体说明见[后端 README](../README.md)。

## 目录结构

```text
business/
├── api/openapi.yaml       # HTTP API 定义
├── cmd/
│   ├── server/            # HTTP API 服务
│   ├── business-worker/   # 业务异步任务消费者
│   ├── search-indexer/    # 搜索索引更新消费者
│   ├── search-reindex/    # 搜索索引重建工具
│   ├── migrate/           # 数据库迁移工具
│   ├── admin-role/        # 超级管理员角色管理工具
│   └── selfmonitor-control/ # Compose 自监控控制服务
├── internal/              # 业务实现、配置和基础设施访问
├── migrations/            # SQL 迁移文件与嵌入逻辑
├── testdata/              # 测试数据
├── go.mod
└── Makefile
```

## 功能与进程职责

| 范围 | 主要实现 |
| --- | --- |
| 用户与内容 | `internal/user`、`auth`、`post`、`comment`、`like`、`bookmark` |
| 异步通知与搜索 | `internal/notification`、`search`、`outbox`、`worker`、`bus` |
| 业务容量与保护 | `internal/businessstate`、`resourcebudget`、`ratelimit` |
| 平台查询与管理 | `internal/metricquery`、`logquery`、`eventquery`、`exporterplugin`、`alert`、`selfmonitor` |
| HTTP 与配置 | `internal/http`、`config` |
| 基础设施与运行观测 | `internal/platform`、`instrumentation`、`observability` |

`cmd/server` 提供 HTTP API，并按角色装配后台任务。业务事务通过 Outbox 记录待分发事件，再通过 RabbitMQ 交给业务 Worker 和搜索 Indexer 消费。Worker 处理业务异步任务；Indexer 更新 Elasticsearch 搜索索引。

### API 角色

`BACKEND_SERVICE_ROLE` 控制同一服务程序启用的职责，定义见[角色配置](internal/config/service_role.go)。

| 值 | 启用的职责 |
| --- | --- |
| `combined` | 默认值，同时启用业务和平台职责 |
| `business` | 启用业务接口及业务后台任务 |
| `platform` | 启用平台查询与管理职责 |

具体接口以 [OpenAPI 定义](api/openapi.yaml)和[路由实现](internal/http/router.go)为准。平台独立部署的鉴权配置见[部署说明](../../deploy/README.md)。

### 管理工具

| 入口 | 用途 |
| --- | --- |
| [cmd/migrate](cmd/migrate/) | 支持 `validate`、`status`、`up`、`down`；`down` 仅用于本地开发，产品恢复须使用备份与恢复流程 |
| [cmd/search-reindex](cmd/search-reindex/) | 重建搜索索引；`--if-missing` 仅在搜索别名不存在时初始化 |
| [cmd/admin-role](cmd/admin-role/) | 支持按用户 ID 声明初始超级管理员，或按用户名提升超级管理员角色 |
| [cmd/selfmonitor-control](cmd/selfmonitor-control/) | 由 systemd 通过 Unix socket 激活，用于 Compose 自监控控制 |

## 依赖与配置

业务路径使用 MySQL 保存业务数据、Redis 提供缓存、RabbitMQ 传递异步任务、Elasticsearch 提供搜索。平台路径按功能访问 Monitor、Marshaller、VictoriaMetrics 和 Elasticsearch。实际依赖由服务角色及各程序的配置加载器决定。

环境变量示例见[.env.example](../../.env.example)，加载与校验逻辑见 [internal/config](internal/config/)。实际本地配置保存在根目录 `.env`，敏感值不得提交。共享的进程生命周期、健康探针和指标能力来自 [runtimekit](../runtimekit/README.md)。

## 本地开发

```sh
make doctor
make bootstrap
make dev
```

开发入口会准备依赖，执行数据库迁移和必要的搜索初始化，再启动 API、业务 Worker、搜索 Indexer 和用户前端。需要 Monitor、Marshaller 及管理前端时，执行 `make dev-observe`。结束开发时执行 `make stop`。

## 检查与构建

```sh
make check MODULE=backend/business
make lint MODULE=backend/business
make test MODULE=backend/business
make race MODULE=backend/business
make build MODULE=backend/business
```

`check` 检查 Go 格式并运行 `go vet`；`lint` 运行 golangci-lint；`race` 在测试时启用竞态检测。完整模块验证使用 `make verify MODULE=backend/business`。需要运行实际环境集成检查时，使用 `make integration SCOPE=business`。

PR 和手动运行通过 `ci:backend/business` 标签选择本模块。push 按文件变更选择模块，新分支或公共 CI 配置变更等情况会检查全部 Go 模块。详细规则见[CI 说明](../../tools/ci/CI.md)。

## 相关文档

- [贡献说明](../../CONTRIBUTING.md)
- [可观测系统](../observability/README.md)
- [部署说明](../../deploy/README.md)
- [自监控部署](../../deploy/selfmonitor/README.md)
