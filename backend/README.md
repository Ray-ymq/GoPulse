# GoPulse 后端

后端包含一个 Go 模块，使用 Go 1.26：

- [business](business/README.md)：社交业务 API、业务 Worker、搜索 Indexer 和 SQL 迁移。

日志与请求 ID 由 `business/internal/logging` 提供，健康检查由 `business/internal/platform` 提供。各命令入口使用标准库处理信号和退出。

业务写入 MySQL，并通过事务 Outbox 将事件发送到 RabbitMQ。Worker 处理通知，Indexer 更新 Elasticsearch 搜索索引。Redis 提供业务缓存。

API 仅支持 `business` 角色，也是默认角色。旧 `combined` 和 `platform` 配置会被拒绝。

运行前需自行准备业务依赖和进程配置。验证模块使用 `make ci-check MODULE=backend/business` 和 `make ci-test MODULE=backend/business`；完整模块清单见 `make list-go-modules`。
