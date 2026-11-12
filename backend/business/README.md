# GoPulse 社交业务模块

本模块提供用户认证、资料、关注、帖子、评论、点赞、收藏、搜索和通知。使用 Go 1.26，Go module 名称为 `github.com/Ray-ymq/GoPulse/backend`。HTTP 接口以 [OpenAPI](api/openapi.yaml) 为准。

## 目录与命令入口

| 路径 | 职责 |
| --- | --- |
| `cmd/server/` | 社交业务 API 与事务 Outbox 分发 |
| `cmd/business-worker/` | 消费异步业务任务并生成通知 |
| `cmd/search-indexer/` | 消费搜索任务并更新 Elasticsearch 索引 |
| `cmd/search-reindex/` | 初始化或重建搜索索引，执行数据 epoch 恢复准备 |
| `cmd/migrate/` | 校验迁移文件、查看数据库状态及执行迁移 |
| `cmd/admin-role/` | 声明初始超级管理员或提升已有用户角色 |
| [internal/](internal/README.md) | 按业务模块、HTTP 适配、异步执行、基础设施和公共能力组织 |
| `migrations/` | SQL 迁移及迁移文件加载 |
| `api/` | HTTP 接口契约 |

业务写入 MySQL，并通过同一事务记录 Outbox 事件。API 进程中的分发器将事件发送到 RabbitMQ；业务 Worker 处理通知，搜索 Indexer 更新 Elasticsearch。Redis 提供正文缓存和共享限流。

## 环境与配置

运行前需自行准备 MySQL、Redis、RabbitMQ 和 Elasticsearch，并创建应用使用的数据库与账号。仓库不提供部署配置或本地开发编排。

配置示例统一维护在根 [.env.example](../../.env.example)，本地实际配置存入根 `.env`。后端只读取进程环境变量，不会自动加载 `.env`。首次配置步骤见 [根目录说明](../../README.md#开发准备)。

每个后端终端均需加载配置。从仓库根目录执行以下命令；配置值包含空格、`$` 或其他 shell 特殊字符时，需先按 shell 语法正确引用。

```sh
set -a
. ./.env
set +a
cd backend/business
```

后续迁移、运行和维护命令均在已加载配置的 `backend/business/` 目录执行。

| 配置 | 约束与作用 |
| --- | --- |
| `BACKEND_SERVICE_ROLE` | 默认且仅支持 `business`；旧值 `combined` 和 `platform` 会被拒绝 |
| `GOPULSE_RUNTIME_MODE` | `host` 模式要求 API 和依赖使用回环地址；`container` 模式要求 API 使用未指定监听地址，依赖使用服务 DNS 名称 |
| `AUTH_JWT_SECRET` | 至少 32 字节，示例值仅用于本地开发 |
| `AUTH_COOKIE_SECURE` | 非本地开发和测试环境必须设置为 `true` |
| `BACKEND_REDIS_DISABLED` | 设置为 `true` 时不创建业务 Redis 客户端，禁用正文缓存，限流配额按进程独立计算 |
| `OUTBOX_RETRY_DELAY` | API 与业务 Worker 必须使用相同值 |
| `SEARCH_INDEXER_RETRY_DELAY` | API 与搜索 Indexer 必须使用相同值 |

配置加载还会校验连接地址、资源预算、连接池和业务实例名称。MySQL 连接池设置按进程生效；API 的连接预算校验计入 Outbox、探针连接池及副本数。完整配置项和示例值见根 `.env.example`，读取与校验实现见 [internal/platform/config](internal/platform/config)。

## 数据库迁移

升级已有数据库前，先备份需要保留的数据。迁移 `000021_remove_observability` 会删除 `alert_rule_states`、`alert_incidents` 和 `alert_rules` 三张表；降级只能恢复表结构，不能恢复已删除的告警历史。历史迁移编号保持不变，业务表和用户角色数据继续保留。

```sh
go run ./cmd/migrate validate
go run ./cmd/migrate status
go run ./cmd/migrate up
```

`validate` 只校验内嵌迁移文件，不连接数据库；`status` 查看数据库迁移状态；`up` 执行待应用的迁移。`down` 回退一个迁移，仅用于本地开发，不能代替产品数据的备份与恢复。

## 启动业务进程

完成迁移后，分别在三个已加载配置的终端中执行以下命令。使用 Ctrl+C 停止进程。

| 命令 | 职责 | 默认监听地址 |
| --- | --- | --- |
| `go run ./cmd/server` | API 与 Outbox 分发 | `127.0.0.1:8080` |
| `go run ./cmd/business-worker` | 异步通知处理 | 健康检查 `127.0.0.1:19102` |
| `go run ./cmd/search-indexer` | 搜索索引更新 | 健康检查 `127.0.0.1:19103` |

API 通过 `HTTP_HOST` 和 `HTTP_PORT` 配置监听地址。Worker 和 Indexer 分别使用 `BUSINESS_WORKER_HEALTH_ADDRESS` 和 `SEARCH_INDEXER_HEALTH_ADDRESS`。健康检查地址不会随 `GOPULSE_RUNTIME_MODE` 自动切换；容器需要外部探测时，须按部署方式配置监听地址。

前端开发步骤见 [用户前端说明](../../frontend/web/README.md)。登录使用后端下发的 HttpOnly 会话 Cookie；前端部署需配置 `/api` 同源代理。

## 搜索索引与用户角色维护

首次准备搜索索引时执行：

```sh
go run ./cmd/search-reindex --if-missing
```

`--if-missing` 会检查现有搜索别名、数据 epoch 和所需映射。当前索引满足条件时跳过重建；索引缺失或不满足条件时重建。需要主动重建时执行：

```sh
go run ./cmd/search-reindex
```

`SEARCH_REINDEX_BATCH` 控制批量大小。恢复准备使用 `--restore-operation`、`--expected-old-epoch` 和 `--new-epoch` 三个配套参数，不能与 `--if-missing` 同用。操作 ID 必须为 64 位小写十六进制字符串；两个 epoch 必须为不同的规范 UUID。该操作会修改业务数据 epoch 并重建搜索索引，不能作为普通初始化步骤使用。参数和执行逻辑见 [重建命令源码](cmd/search-reindex/main.go)。

角色维护命令作用于已有用户，不创建账号。声明初始超级管理员时，使用 `go run ./cmd/admin-role bootstrap --user-id <id>`，将 `<id>` 替换为已注册用户的正整数 ID。首次声明后，其他账号不能替换该初始账号。

将已有用户提升为超级管理员时，使用 `go run ./cmd/admin-role promote --username <username>`，将 `<username>` 替换为注册用户名。参数和行为见 [角色维护命令源码](cmd/admin-role/main.go)。

## 日志、健康检查与退出

业务进程使用 [internal/logging](internal/logging) 输出 `slog` JSON 日志。日志包含标准字段 `time`、`level`、`msg`，并附加 `service`、`module` 和业务字段。请求 ID 使用 32 位小写十六进制格式，通过 `X-Request-ID` 返回，并用于 Elasticsearch 请求关联。

[internal/platform](internal/platform) 提供以下健康探针：

| 路径 | 含义 |
| --- | --- |
| `/startup` | 初始化已完成 |
| `/live`、`/health` | 进程存活 |
| `/ready` | 已完成初始化、未进入退出阶段，且必要依赖和状态检查通过 |

探针响应只包含 `status`，不返回 `contract_version`。依赖检查不缓存结果，检查函数必须响应上下文取消。

各常驻进程通过 `os/signal` 接收退出信号，并使用自身的退出超时关闭服务和资源。业务配置保留校验，运行时层的全局环境校验和退出计时器已移除。

可观测平台路由、runtimekit、指标埋点与接口、采集任务和链路追踪已移除。日志不再包含运行时合同、版本和实例等固定元数据，不再按消息推导 `event`，也不再应用运行时层日志抑制。

## 验证与构建

在仓库根目录执行：

```sh
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
```

`ci-check` 检查格式并运行 `go vet` 和 lint；`ci-test` 执行 `go test -race -count=1 ./...`；`ci-build` 构建模块。Go CI 命令使用 `.run/ci/go.work`，不会覆盖开发者的根 `go.work`。

日常单元测试也可在模块目录执行 `go test ./...`。带 `integration` 构建标签的测试需单独准备环境，并按各测试入口的要求运行。单元测试和构建结果不能代替真实数据库、搜索及异步任务联调。

模块注册和构建入口见根 [Makefile](../../Makefile)，协作要求见 [AGENTS.md](../../AGENTS.md)，CI 与提交规范见 [tools/ci/CI.md](../../tools/ci/CI.md)。
