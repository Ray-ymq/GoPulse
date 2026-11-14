# GoPulse 后端

GoPulse 后端提供认证、用户资料、关注、帖子、评论、点赞、收藏、搜索、异步通知和超级管理员角色管理。当前只有一个 Go 模块：`business/`，使用 Go 1.26，模块名称为 `github.com/Ray-ymq/GoPulse/backend`。

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
    │   ├── search-indexer/          # 消费消息并更新搜索索引
    │   ├── search-reindex/          # 索引初始化、重建与恢复准备
    │   ├── migrate/                 # 数据库迁移
    │   └── admin-role/              # 初始超级管理员设置
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
    │   │   │   └── admin/           # 账号角色查询与变更
    │   │   ├── middleware/          # 认证、授权与请求保护
    │   │   ├── request/             # 请求体解析
    │   │   ├── response/            # 响应与错误编码
    │   │   └── params/              # 路径参数解析
    │   ├── async/
    │   │   ├── outbox/              # 事务事件写入与可靠分发
    │   │   └── worker/              # 消费运行、确认与重试
    │   ├── platform/                # 外部连接、健康检查与技术适配
    │   │   ├── config/              # 配置读取与校验
    │   │   ├── redisclient/         # Redis 连接、操作与故障恢复
    │   │   ├── rabbitmq/            # RabbitMQ 探测、消息发布与拓扑
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
    └── Makefile                     # 模块检查、测试与构建
```

## 分层与依赖

`cmd` 构造依赖并管理进程生命周期。`transport/http` 处理路由、请求与响应，通过接口调用业务。`modules` 按功能组织业务，在各模块内部用 `model`、`validation`、`service` 和 `repository` 等文件区分职责。

业务模块不依赖 Gin、HTTP 适配或 Worker 运行实现。通知与搜索 Processor 负责具体投影，`async/worker` 负责消息确认、重试和死信。帖子模块定义缓存接口，`platform/postcache` 实现 Redis 适配，由入口注入。

`platform` 根包保留 MySQL、Elasticsearch、数据库版本检查和健康检查。Redis 与 RabbitMQ 分别由 `platform/redisclient` 和 `platform/rabbitmq` 提供；调用方直接导入对应子包。帖子缓存直接依赖 Redis 子包，避免通过根包引入其他适配器。

`platform/config` 保持一个包。`config.go` 提供 API 配置加载入口，其余文件按 MySQL、Redis、搜索、认证、请求保护、Outbox、运行环境和通用解析划分职责。Worker 和 Indexer 继续使用各自的配置入口。配置变量、默认值和校验规则保持不变。

数据库写入和对应 Outbox 记录使用同一事务。业务模块可以在事务内调用 `async/outbox`，不为目录分层拆开原子写入。存储接口与实现仍可保留在同一业务包中。

目录与导入约束由 [架构测试](business/internal/architecture_test.go) 检查。角色管理的 HTTP 入口位于 [handlers/admin](business/internal/transport/http/handlers/admin/)，角色变更和审计事务位于 [用户角色存储实现](business/internal/modules/user/repository_management.go)。

## 数据与异步流程

| 依赖 | 用途 |
| --- | --- |
| MySQL | 业务数据、事务 Outbox、命令和处理状态 |
| Redis | 帖子正文缓存与共享限流 |
| RabbitMQ | 传递通知和搜索索引事件 |
| Elasticsearch | 帖子搜索索引与查询 |

业务事务提交后，API 进程中的 Outbox 分发器将事件发布到 RabbitMQ。业务 Worker 生成通知，搜索 Indexer 更新 Elasticsearch。异步处理不改变业务事实与 Outbox 记录共同提交的要求。

## 配置与运行

宿主机源码运行前需自行准备外部依赖及应用账号。单机容器部署通过 [Docker Compose](../deploy/README.md) 创建依赖和业务进程。仓库不提供压测工具。配置示例统一维护在根 [.env.example](../.env.example)，本地实际配置存入根 `.env`，敏感值不得提交。

后端只读取进程环境变量，不会自动加载 `.env`。每个后端终端都需加载配置。从仓库根目录执行以下命令；配置值包含 shell 特殊字符时，需先按 shell 语法正确引用。

```sh
set -a
. ./.env
set +a
cd backend/business
```

后续迁移和运行命令均在已加载配置的 `backend/business/` 目录执行。命令中的尖括号表示占位内容，执行前需替换为实际值，并移除尖括号。完整准备步骤见 [根目录开发说明](../README.md#开发准备)。

`BACKEND_SERVICE_ROLE` 默认且仅支持 `business`。旧值 `combined` 和 `platform` 会被拒绝。`GOPULSE_RUNTIME_MODE=host` 要求 API 和依赖使用回环地址；容器模式的地址约束及其他校验见 [配置实现](business/internal/platform/config/) 和根 [.env.example](../.env.example)。

### 数据库迁移

升级已有数据库前，先备份需要保留的数据。迁移 `000021_remove_observability` 会删除 `alert_rule_states`、`alert_incidents` 和 `alert_rules` 三张表；降级只能恢复表结构，不能恢复已删除的数据。

```sh
go run ./cmd/migrate validate
go run ./cmd/migrate status
go run ./cmd/migrate up
```

`validate` 校验迁移文件，`status` 查看数据库状态，`up` 执行待应用迁移。`down` 仅用于本地开发，不能代替备份与恢复流程。

### 常驻进程

完成迁移后，在分别加载配置的终端中启动以下三个进程。首次使用搜索前还需按下文初始化搜索索引。使用 Ctrl+C 停止进程。

| 命令 | 职责 | 默认监听地址 |
| --- | --- | --- |
| `go run ./cmd/server` | HTTP API 与 Outbox 分发 | `127.0.0.1:8080` |
| `go run ./cmd/business-worker` | 消费 RabbitMQ 消息并处理通知 | 健康检查 `127.0.0.1:19102` |
| `go run ./cmd/search-indexer` | 消费 RabbitMQ 消息并更新 Elasticsearch 索引 | 健康检查 `127.0.0.1:19103` |

API 使用 `HTTP_HOST` 和 `HTTP_PORT` 配置监听地址。Worker 与 Indexer 分别使用 `BUSINESS_WORKER_HEALTH_ADDRESS` 和 `SEARCH_INDEXER_HEALTH_ADDRESS`。健康检查地址不会随运行模式自动切换；容器部署需按探测方式配置。

### 搜索索引维护

MySQL 和 Elasticsearch 可用后，首次使用搜索前执行：

```sh
go run ./cmd/search-reindex --if-missing
```

该命令从 MySQL 构建搜索索引；搜索别名已存在时跳过重建。需要主动重建时，执行 `go run ./cmd/search-reindex`。重建命令执行完成后退出，`search-indexer` 则持续消费消息并同步后续变更。

数据库备份恢复后的数据 epoch 切换使用 `--restore-operation`、`--expected-old-epoch` 和 `--new-epoch`。三个参数必须同时提供，旧、新 epoch 必须不同，且不能与 `--if-missing` 合用。该模式负责索引重建和业务 epoch 状态切换，不执行数据库备份恢复。参数校验与执行步骤见 [search-reindex 入口](business/cmd/search-reindex/main.go)。

## 管理员角色

### 初始超级管理员

先通过前端注册账号。系统维护者在已加载配置的 `backend/business/` 目录执行以下命令，参数使用该账号的用户 ID：

```sh
go run ./cmd/admin-role bootstrap --user-id <已注册用户的ID>
```

也可以按已有账号的用户名设置：

```sh
go run ./cmd/admin-role promote --username <已注册用户名>
```

这两个命令都调用初始超级管理员设置逻辑，不创建账号。初始超级管理员已经确定后，重复指定同一账号不会替换它；指定其他账号会被拒绝。`promote` 不能用于日常新增其他超级管理员。

### 网页角色管理

初始超级管理员重新登录或刷新前端页面后，可从“角色管理”进入 `/admin/roles`。页面支持按完整用户名查询账号，并在确认后授予或撤销超级管理员权限。

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/admin/users/by-username/{username}` | 返回账号角色和初始超级管理员标记 |
| `PATCH /api/v1/admin/users/{userId}/role` | 将角色设置为 `user` 或 `super_admin` |

接口使用登录会话 Cookie，后端在每次管理请求中读取数据库角色并检查权限。变更事务会再次检查操作者角色，并将角色变更与成功审计记录一同提交。请求 ID 写入审计记录；角色没有变化时返回 `changed=false`，不新增变更审计记录。

- 初始超级管理员不能降为普通用户，接口返回 `409 bootstrap_super_admin_protected`。
- 非初始管理员可以撤销自己的权限。成功后前端返回首页，后续管理请求会因权限不足被拒绝。
- 通过超级管理员角色校验后，如果尚未完成初始管理员设置或初始化状态不可用，接口返回 `503 management_setup_unavailable`。
- 请求结果不确定时，页面要求重新查询当前角色，再决定是否操作。

前端不会提供初始管理员设置入口。页面行为见 [前端说明](../frontend/web/README.md#管理员角色)，请求和响应字段以 [OpenAPI](business/api/openapi.yaml) 为准。

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
