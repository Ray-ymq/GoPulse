# Phase-22-02 原生验证入口登记

本文件登记本批执行过的日常原生入口及仍保留的专项入口，避免把模块单测误记为跨进程验收。

## 已执行的原生入口

| 能力 | 原生承接 | 实际门禁 |
| --- | --- | --- |
| 业务注册、登录、发帖、评论、点赞 | Backend HTTP 集成用例及 `business_flow_integration_test.go` | `make integration SCOPE=business` |
| Outbox、RabbitMQ、通知、搜索 | 真实 test MySQL/RabbitMQ/Elasticsearch 与 Backend worker/search-indexer | `make integration SCOPE=business` |
| 告警状态、租约、恢复、审计 | `repository_integration_test.go` 与共享 MySQL 断言 | `make integration SCOPE=business` |
| 指标、日志、运行事件 | Redis exporter → Monitor → Router → Kafka → Marshaller → 查询 API | `make integration SCOPE=observe` |
| 观测权限失败 | `observability_flow_integration_test.go` 的管理员、普通用户、401/403 断言 | `make integration SCOPE=observe` |
| 依赖隔离与生命周期 | `local_development.py` 的 test scope、锁、端口预检、候选版本项目和归属清理 | Python 单测、Compose config、两条 integration scope |

## 模块与工具门禁

以下模块均实际执行过一次 `make test MODULE=...` 并通过：`backend`、`monitor`、`router`、`marshaller`、`componentmetrics`、`exporters/redis`、`exporters/mysql`、`exporters/rabbitmq`、`exporters/elasticsearch`、`exporters/kafka`、`exporters/victoriametrics`。

工具门禁为：`python3 -m unittest discover -s scripts/ci -p test_local_development.py`、`bash -n scripts/verify-business.sh`、`bash scripts/verify-business.sh --self-test`。`make integration` 默认委托 business，`scripts/verify-business.sh --native` 也只委托该入口。

CI Integration 门禁在 migration 后执行 `go run ./cmd/search-reindex --if-missing`，再使用 `go test -p 1 -count=1 -tags=integration ./...`；这样与本批原生入口一致，并避免共享 Elasticsearch alias 与 MySQL named lock 的并行竞态。

## 保留的专项入口与边界

`verify-monitor.sh`、`verify-router.sh`、`verify-marshaller.sh`、`verify-exporter.sh`、`verify-component-metrics.sh`、`verify-logs.sh`、`verify-events.sh`、`verify-alerts.sh`、`verify-plugin-state.sh` 及 Phase 20 链路/证据工具仍保留。它们继续承担容器安全、插件制品、重启持久化、故障恢复、长期评估、容量、完整 Trace 和发布证据等本批没有替代的专项检查；本批原生入口不宣称覆盖这些能力。
