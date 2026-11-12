# GoPulse 后端

GoPulse 后端提供认证、用户资料、关注、帖子、评论、点赞、收藏、搜索和异步通知。当前只有一个 Go 模块：`business/`，使用 Go 1.26，模块名称为 `github.com/Ray-ymq/GoPulse/backend`。

模块清单以根 [Makefile](../Makefile) 的 `GO_MODULES` 为准。HTTP 接口以 [OpenAPI](business/api/openapi.yaml) 为准。

## 目录树

以下目录树列出主要源码目录与入口，省略具体实现文件和测试文件。

```text
backend/
├── README.md
└── business/
    ├── cmd/                         # 进程与维护命令入口
    │   ├── server/                  # HTTP API 与 Outbox 分发
    │   ├── business-worker/         # 异步通知消费
    │   ├── search-indexer/          # 搜索索引消费
    │   ├── search-reindex/          # 索引初始化、重建与恢复准备
    │   ├── migrate/                 # 数据库迁移
    │   └── admin-role/              # 已有用户的角色维护
    ├── internal/
    │   ├── modules/                 # 业务功能与共享业务契约
    │   │   ├── auth/                # 认证、密码和令牌
    │   │   ├── user/                # 用户、资料、关注和角色
    │   │   ├── post/                # 帖子、命令与可见性
    │   │   ├── comment/             # 评论
    │   │   ├── like/                # 点赞关系
    │   │   ├── bookmark/            # 收藏关系
    │   │   ├── notification/        # 通知查询与事件投影
    │   │   ├── search/              # 搜索、索引投影与重建
    │   │   ├── bus/                 # 事件、准入和处理结果契约
    │   │   └── businessstate/       # 纪元、命令与持久化一致性状态
    │   ├── transport/http/          # HTTP 路由组装
    │   │   ├── handlers/            # 按业务功能划分的 Handler
    │   │   ├── middleware/          # 认证、授权与请求保护
    │   │   ├── request/             # 请求体解析
    │   │   ├── response/            # 响应与错误编码
    │   │   └── params/              # 路径参数解析
    │   ├── async/
    │   │   ├── outbox/              # 事务事件写入与可靠分发
    │   │   └── worker/              # 消费运行、确认与重试
    │   ├── platform/                # 外部连接、健康检查与技术适配
    │   │   ├── config/              # 配置读取与校验
    │   │   ├── postcache/           # Redis 帖子正文缓存
    │   │   ├── ratelimit/           # 请求限流
    │   │   └── resourcebudget/      # 进程资源预算
    │   ├── shared/
    │   │   ├── apperror/            # 应用错误类型
    │   │   └── correlation/         # 请求与异步处理关联信息
    │   ├── logging/                 # slog 日志与上下文
    │   ├── testkit/integrationtest/ # 集成测试支持
    │   └── architecture_test.go     # 目录与导入约束检查
    ├── api/openapi.yaml             # HTTP 接口契约
    ├── migrations/                  # SQL 迁移与迁移文件加载
    ├── testdata/                    # 测试辅助程序
    ├── go.mod
    ├── go.sum
    ├── Makefile                     # 模块检查、测试与构建
    └── README.md                    # 业务模块运行与维护说明
```

## 分层与依赖

`cmd` 构造依赖并管理进程生命周期。`transport/http` 处理路由、请求与响应，通过接口调用业务。`modules` 按功能组织业务，在各模块内部用 `model`、`validation`、`service` 和 `repository` 等文件区分职责。

业务模块不依赖 Gin、HTTP 适配或 Worker 运行实现。通知与搜索 Processor 负责具体投影，`async/worker` 负责消息确认、重试和死信。帖子模块定义缓存接口，`platform/postcache` 实现 Redis 适配，由入口注入。

数据库写入和对应 Outbox 记录使用同一事务。业务模块可以在事务内调用 `async/outbox`，不为目录分层拆开原子写入。存储接口与实现仍可保留在同一业务包中。

详细文件职责、依赖例外和自动检查范围见 [internal 目录说明](business/internal/README.md)。

## 数据与异步流程

| 依赖 | 用途 |
| --- | --- |
| MySQL | 业务数据、事务 Outbox、命令和处理状态 |
| Redis | 帖子正文缓存与共享限流 |
| RabbitMQ | 传递通知和搜索索引事件 |
| Elasticsearch | 帖子搜索索引与查询 |

业务事务提交后，API 进程中的 Outbox 分发器将事件发布到 RabbitMQ。业务 Worker 生成通知，搜索 Indexer 更新 Elasticsearch。异步处理不改变业务事实与 Outbox 记录共同提交的要求。

## 配置与运行

运行前需自行准备外部依赖及应用账号。仓库不提供部署配置、本地开发编排或压测工具。配置示例统一维护在根 [.env.example](../.env.example)，本地实际配置存入根 `.env`，敏感值不得提交。

后端只读取进程环境变量，不会自动加载 `.env`。每个后端终端都需加载配置。从仓库根目录执行以下命令；配置值包含 shell 特殊字符时，需先按 shell 语法正确引用。

```sh
set -a
. ./.env
set +a
cd backend/business
```

后续迁移和运行命令均在已加载配置的 `backend/business/` 目录执行。完整准备步骤见 [根目录开发说明](../README.md#开发准备)。

`BACKEND_SERVICE_ROLE` 默认且仅支持 `business`。旧值 `combined` 和 `platform` 会被拒绝。`GOPULSE_RUNTIME_MODE=host` 要求 API 和依赖使用回环地址；容器模式的地址约束及其他校验见 [配置实现](business/internal/platform/config/) 和 [业务模块说明](business/README.md#环境与配置)。

### 数据库迁移

升级已有数据库前，先备份需要保留的数据。迁移 `000021_remove_observability` 会删除 `alert_rule_states`、`alert_incidents` 和 `alert_rules` 三张表；降级只能恢复表结构，不能恢复已删除的数据。

```sh
go run ./cmd/migrate validate
go run ./cmd/migrate status
go run ./cmd/migrate up
```

`validate` 校验迁移文件，`status` 查看数据库状态，`up` 执行待应用迁移。`down` 仅用于本地开发，不能代替备份与恢复流程。

### 常驻进程

完成迁移后，在分别加载配置的终端中启动以下三个进程。使用 Ctrl+C 停止进程。

| 命令 | 职责 | 默认监听地址 |
| --- | --- | --- |
| `go run ./cmd/server` | HTTP API 与 Outbox 分发 | `127.0.0.1:8080` |
| `go run ./cmd/business-worker` | 异步通知处理 | 健康检查 `127.0.0.1:19102` |
| `go run ./cmd/search-indexer` | 搜索索引更新 | 健康检查 `127.0.0.1:19103` |

API 使用 `HTTP_HOST` 和 `HTTP_PORT` 配置监听地址。Worker 与 Indexer 分别使用 `BUSINESS_WORKER_HEALTH_ADDRESS` 和 `SEARCH_INDEXER_HEALTH_ADDRESS`。健康检查地址不会随运行模式自动切换；容器部署需按探测方式配置。

### 维护命令

`cmd/search-reindex` 用于初始化或重建索引，`cmd/admin-role` 用于维护已有用户的角色，不创建账号。索引重建、数据 epoch 恢复准备和角色维护的参数与限制见 [维护说明](business/README.md#搜索索引与用户角色维护)。

## 日志与健康检查

`internal/logging` 使用标准库 `slog` 输出 JSON 日志。HTTP 请求通过 `X-Request-ID` 返回请求 ID。各常驻进程通过标准库接收退出信号，并按自身的退出超时关闭资源。

`internal/platform` 提供以下探针：

| 路径 | 含义 |
| --- | --- |
| `/startup` | 初始化已完成 |
| `/live`、`/health` | 进程存活 |
| `/ready` | 初始化完成、未进入退出阶段，且必要依赖和状态检查通过 |

旧可观测平台、runtimekit、指标接口、采集任务和链路追踪已移除。当前入口保留结构化日志、健康检查和退出管理。

## 验证与构建

在仓库根目录执行：

```sh
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
```

| 命令 | 检查范围 |
| --- | --- |
| `ci-check` | 格式、`go vet` 和 lint |
| `ci-test` | `go test -race -count=1 ./...`，包含目录和依赖边界检查 |
| `ci-build` | 模块构建 |

这些命令使用 `.run/ci/go.work`，不会覆盖开发者的根 `go.work`。带 `integration` 标签的测试需要单独准备隔离环境。单元测试、构建或集成测试编译结果不能代替真实依赖联调。

使用 `make list-go-modules` 查看模块清单。协作与提交要求见 [AGENTS.md](../AGENTS.md) 和 [CI 与提交规范](../tools/ci/CI.md)。
