# GoPulse 后端

GoPulse 后端提供用户认证、资料、关注、帖子、评论、点赞、收藏、搜索和异步通知。当前包含一个 Go 1.26 模块：`business/`。模块名称保持为 `github.com/Ray-ymq/GoPulse/backend`，模块清单由根 [Makefile](../Makefile) 的 `GO_MODULES` 维护。

## 目录与职责

| 路径 | 职责 |
| --- | --- |
| `business/cmd/` | API、后台任务和运维命令入口 |
| `business/internal/` | 业务逻辑、数据访问、配置及基础能力 |
| `business/api/openapi.yaml` | HTTP 接口契约 |
| `business/migrations/` | SQL 迁移及迁移文件加载 |
| `business/Makefile` | 模块检查、测试和构建入口 |

业务实现与运行行为见 [业务模块说明](business/README.md)，接口定义见 [OpenAPI](business/api/openapi.yaml)。

## 数据与任务流程

MySQL 保存业务数据。业务写入通过事务 Outbox 记录待发送事件，API 进程中的分发器将事件发布到 RabbitMQ。业务 Worker 消费通知任务，搜索 Indexer 消费索引任务并更新 Elasticsearch。Redis 提供业务缓存和共享限流。

API 仅支持 `business` 角色，也是默认角色。`BACKEND_SERVICE_ROLE` 使用旧值 `combined` 或 `platform` 时，配置加载会失败。

## 配置与运行

仓库不提供部署配置或本地开发编排。运行前需自行准备 MySQL、Redis、RabbitMQ 和 Elasticsearch，并创建应用使用的数据库与账号。环境变量示例统一维护在根 [.env.example](../.env.example)。本地实际配置存入根 `.env`，敏感值不得提交。

后端只读取进程环境变量，不会自动加载 `.env`。在每个后端终端中，从仓库根目录执行以下命令。配置值包含 shell 特殊字符时，需先按 shell 语法正确引用。

```sh
set -a
. ./.env
set +a
cd backend/business
```

完整环境准备步骤见 [根目录开发说明](../README.md#开发准备)。示例使用 `host` 模式，API 和依赖地址必须指向本机回环地址。

升级已有数据库前，先备份需要保留的数据。迁移 `000021_remove_observability` 会删除三张告警表；降级只能恢复表结构，不能恢复已删除的数据。历史迁移编号保持不变。

在已加载配置的 `backend/business/` 目录校验迁移文件、查看数据库状态并执行迁移：

```sh
go run ./cmd/migrate validate
go run ./cmd/migrate status
go run ./cmd/migrate up
```

迁移完成后，分别在已加载配置的终端中启动以下三个常驻进程。使用 Ctrl+C 停止进程。

| 命令 | 职责 | 默认监听地址 |
| --- | --- | --- |
| `go run ./cmd/server` | 社交业务 API 与 Outbox 分发 | `127.0.0.1:8080` |
| `go run ./cmd/business-worker` | 异步业务任务与通知 | 健康检查 `127.0.0.1:19102` |
| `go run ./cmd/search-indexer` | 更新 Elasticsearch 搜索索引 | 健康检查 `127.0.0.1:19103` |

`cmd/search-reindex` 用于重建搜索索引，`cmd/admin-role` 用于维护已有用户的角色数据。迁移 `down` 仅用于本地开发，产品数据恢复需要备份与恢复流程。

## 日志、健康检查与退出

日志和请求 ID 由 `business/internal/logging` 提供，业务进程使用标准库 `slog` 输出 JSON 日志。健康检查由 `business/internal/platform` 提供：`/startup` 表示初始化完成，`/live` 和 `/health` 表示进程存活，`/ready` 检查必要依赖。

Worker 和 Indexer 的健康检查地址分别由 `BUSINESS_WORKER_HEALTH_ADDRESS` 和 `SEARCH_INDEXER_HEALTH_ADDRESS` 指定。地址不会随 `GOPULSE_RUNTIME_MODE` 自动切换，容器部署需按探测方式配置监听地址。各常驻进程通过标准库处理信号，并使用各自的退出超时关闭资源。

可观测平台、runtimekit、指标接口、采集任务和链路追踪已移除。当前后端保留本地结构化日志、健康检查和退出管理。

## 验证与构建

在仓库根目录执行：

```sh
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
```

`ci-check` 执行格式检查、`go vet` 和 lint；`ci-test` 运行带竞态检测的 Go 测试；`ci-build` 构建模块。上述命令使用 `.run/ci/go.work`，不会覆盖开发者的根 `go.work`。真实依赖联调需单独执行。

使用 `make list-go-modules` 查看 Go 模块清单。更多协作和 CI 要求见 [AGENTS.md](../AGENTS.md) 与 [CI 与提交规范](../tools/ci/CI.md)。
