# 社交业务系统

Go module 为 `github.com/Ray-ymq/GoPulse/backend`，使用 Go 1.26。

## 业务与入口

业务包括用户认证、资料、关注、帖子、评论、点赞、收藏、搜索和通知。HTTP 契约见 [OpenAPI](api/openapi.yaml)。

| 入口 | 职责 |
| --- | --- |
| `cmd/server` | 社交业务 API 与 Outbox 分发 |
| `cmd/business-worker` | 异步业务任务与通知 |
| `cmd/search-indexer` | 更新 Elasticsearch 搜索索引 |
| `cmd/search-reindex` | 重建搜索索引 |
| `cmd/migrate` | 校验、查看和执行数据库迁移 |
| `cmd/admin-role` | 维护已有用户角色数据的命令行工具 |

API 默认角色为 `business`；不再提供可观测平台路由。MySQL 保存业务数据，Redis 提供缓存，RabbitMQ 传递异步任务，Elasticsearch 提供搜索。

业务进程使用标准库 `slog` 输出 JSON 日志，通过 `os/signal` 接收退出信号，并通过带超时的上下文关闭 HTTP 服务。健康检查由 `internal/platform` 提供。指标埋点、指标接口、采集任务和链路追踪已删除。

## 开发与验证

在仓库根目录执行：

```sh
make ci-check MODULE=backend/business
make ci-test MODULE=backend/business
make ci-build MODULE=backend/business
```

部署配置和本地开发编排已移除。运行进程或执行集成验证前，需自行准备 MySQL、Redis、RabbitMQ 和 Elasticsearch，并按根目录 `.env.example` 配置环境变量。

历史迁移保留原编号。`000021_remove_observability` 删除三张告警表，执行前应备份告警历史；降级仅恢复表结构。业务表与用户角色数据继续保留。

## 运行行为

- 日志使用 `slog` 标准字段 `time`、`level`、`msg`，附加 `service`、`module` 和业务字段。日志不再包含运行时合同、版本、实例等固定元数据，也不再按消息推导 `event` 或进行运行时层日志抑制。
- 请求 ID 保持 32 位小写十六进制格式，继续用于响应及 Elasticsearch 请求关联。
- `/startup` 表示初始化完成，`/live` 和 `/health` 表示进程存活，`/ready` 检查必要依赖。响应仅包含 `status`，不再返回 `contract_version`；依赖检查不缓存结果，检查函数必须响应上下文取消。
- Worker 和 Indexer 的健康检查地址分别由 `BUSINESS_WORKER_HEALTH_ADDRESS`、`SEARCH_INDEXER_HEALTH_ADDRESS` 指定，默认监听 `127.0.0.1:19102` 和 `127.0.0.1:19103`。监听地址不再随 `GOPULSE_RUNTIME_MODE` 自动切换。
- 移除运行时层的全局退出计时器和环境校验。各进程保留自身的退出超时；业务配置仍校验连接地址、资源预算、JWT 密钥和业务实例名称。
