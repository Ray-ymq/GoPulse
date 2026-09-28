# GoPulse 当前能力状态

> 基线：产品版本 `2.1.1`，2026-09-28。版本完成、验收执行完成和能力达标是三个不同概念。

## 已验证

- 社交业务、搜索、通知、插件、Metrics、Logs、Events、内部告警与双 Frontend 端到端闭环。
- Linux `amd64` Compose 与 Bundle 生命周期、同 Bundle 备份恢复和私有网络边界。
- Backend、Business Worker、Search Indexer、Router、Marshaller 双副本计算层。
- Outbox lease/owner、RabbitMQ ack/retry、Kafka generation ownership/manual commit 和终态闭合。
- 业务搜索 Elasticsearch 与可观测 Elasticsearch 独立服务、卷和故障域。
- 有界 HTTP 并发、连接池、prefetch、Kafka buffer、Marshaller in-flight/retry 与关停预算。
- Backend 业务/管理 API 的有限准入与探针隔离；槽位耗尽时固定返回 `503 backend_busy`，四个运行时探针仍保留自身依赖和停止语义。
- Backend 固定低基数 HTTP 延迟分布（bucket/count/sum）以及 in-flight、并发上限、拒绝计数的 Monitor → Envelope → Marshaller → VictoriaMetrics → 查询链路。
- Phase-18-05 冻结候选的扩缩容、局部故障、SIGTERM、重建、独立诊断和清理矩阵。

## 已发现边界

- Phase-18-01/02 未证明稳定 `150 RPS`；历史运行出现重复性失败、burst 错误和 Outbox 积压。
- Phase-18-04 的原冻结候选结果为 `boundary_found`；其历史证据不因后续修复而改写。
- 固定延迟桶支持 P50/P95/P99 的查询表达，但尚未执行 Phase 19 的正式容量 profile，不能据此声明任何吞吐或尾延迟目标达标。
- Monitor 是插件生命周期唯一所有者；状态层仍可为单节点。

## 尚未验证

- 任意公开吞吐、尾延迟或单位资源容量声明。
- 端到端异步新鲜度 SLO、分布式 Trace 与跨消息 Trace 传播。
- Logs、Events、时序数据的长期保留、rollover、磁盘水位和成本合同。
- Monitor 控制面 HA、状态层 HA、生产 TLS/SASL、跨地域、多活和 Kubernetes。

## 声明规则

- `implementation_complete`：计划内文件和行为已实现。
- `validation_complete`：冻结候选和要求的 evidence 完整且可复核。
- `target_met`：固定能力阈值达到。
- `boundary_found`：执行完整但阈值未达到或发现明确边界。
- 验收程序错误、候选漂移、证据缺失或不安全清理属于 `validation_incomplete`，不能产生能力结论。
