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

业务进程保留本地结构化日志、健康探针和退出管理。指标埋点、指标接口、采集任务和链路追踪已删除。

## 开发与验证

在仓库根目录执行：

```sh
make dev
make verify MODULE=backend/business
make integration SCOPE=business
make stop
```

集成验证需要独立环境和 Docker。配置说明见根目录 `.env.example`，部署说明见 [deploy/README.md](../../deploy/README.md)。

历史迁移保留原编号。`000021_remove_observability` 删除三张告警表，执行前应备份告警历史；降级仅恢复表结构。业务表与用户角色数据继续保留。
