# GoPulse 当前能力状态

> 基线：产品完成版本 `2.2.4`，2026-10-01。版本完成、验收执行完成和能力达标是三个不同概念。

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
- Phase-19-02 的容量 profile、开环四阶梯/三重复 runner、独立资源采样、原值统计和严格 evidence verifier 已完成自测与 calibration；这只表示工具合同冻结。
- Phase-19-03 已完成正式入口、逐轮确定性配方、轮次 endpoint、进度/资源/恢复原始证据和严格 verifier 的工具修订；这只表示验收基础设施完成，尚无正式容量结论。
- Phase-19-04 已对冻结的 `2.1.3` candidate（revision `7251d32a20bc`）完成一次正式三重复认证；执行状态为 `complete`，能力状态为 `boundary_found`。固定同步请求门禁全部通过，但异步/观测恢复门禁未通过，正式结果已发布到 [`Phase-19-04 evidence`](../dev/logs/Phase-19/Phase-19-04-evidence/summary.json)。

- Phase-20-01 在冻结的 `2.1.4` 产品候选上完成独立空项目的十二个阶梯单元，新 profile 结果为 `complete / target_met`：50/100/150/200 RPS 各三次均通过固定同步门禁与四个独立恢复门禁，最长恢复观察上界 44.97 秒。接受/终态台账、真实插件 Event、精确业务事实/观测水位、开销对照和安全清理均由原始证据重算；[脱敏证据](../dev/logs/Phase-20/Phase-20-01-evidence/baseline.json) 已严格校验。完成版本为 `2.2.1`，本批没有产品优化。
- 新旧 profile 的隔离、恢复和采样语义不同；新结果不改写 Phase 19 历史，不构成产品优化收益或旧超时单一根因的证明。
- Phase-20-02 在 `2.2.2` 完成发帖→Outbox→RabbitMQ→Indexer→搜索可见的单链路 Trace/新鲜度与 C01～C07 真实案例，受控依赖延迟、Collector 故障、重试、异常上下文、重启和权限边界均有实际证据；范围见 [02 实施日志](../dev/logs/Phase-20/Phase-20-02-业务链路关联与新鲜度.md)。
- Phase-20-03 在冻结 `2.2.2` B0 制品上完成四阶梯三重复：`complete / target_met / not_needed`。90,000 个测量请求的固定错误项为零，最高 P95/P99 为 46.67/148.58 ms，四维独立恢复最长 40.74 秒，12 个项目均完成归属清理。完成版本 `2.2.3` 只同步登记的版本元数据，产品源码/行为配置与 B0 一致；无 B1、无改善率。[原值与聚合](../dev/validation/Phase-20/phase20-optimization.md) 已从严格校验的私有证据生成并核对。
- Phase-20-04 在 `2.2.4` 完成 R01～R08 生命周期验收：Logs/Events 按 UTC 日历边界保留 7 日，真实 Elasticsearch 清理验证了固定集群身份、strict mapping/归属标记、alias、删除前阻断、重试、权限失败、双副本幂等和在途写入竞态；迟到数据永久处理且不复活旧索引。VictoriaMetrics 使用原生 `30d` retentionPeriod，当前查询闭合；短时窗口未加速物理回收。Collector Trace 工件在固定归属路径内观察到轮转，文件/总量预算未越界；本结果不声明长期 Trace 存储。

## 已发现边界

- Phase-18-01/02 未证明稳定 `150 RPS`；历史运行出现重复性失败、burst 错误和 Outbox 积压。
- Phase-18-04 的原冻结候选结果为 `boundary_found`；其历史证据不因后续修复而改写。
- 固定延迟桶支持 P50/P95/P99 的查询表达；Phase-19-04 正式 profile 已执行，但未达到全部容量目标，不能据此声明目标阶梯 `target_met`。
- Phase-19-04 首个观察到的边界为 `50 RPS` 阶梯的异步/观测恢复：三次重复的恢复时间均超过 `120s` 门禁；同窗可见 Kafka lag、Outbox pending 与 Kafka 高 CPU，但这些是相关窗口证据，不构成单一根因证明。
- 原 Phase-19-03 deterministic preflight 发现正式 runner 的配方物化、轮次 endpoint、宿主版本合同和真实恢复证据不闭合；正式容量入口未调用，没有容量结论。修订由新的 Phase-19-03 承担，正式认证已由 Phase-19-04 完成并收口为 `boundary_found`。
- Monitor 是插件生命周期唯一所有者；状态层仍可为单节点。

## 尚未验证

- 本批冻结环境/配方之外的吞吐、尾延迟、持续运行或单位资源容量声明。
- 生产端到端异步新鲜度 SLO、全站分布式 Trace；已完成的有界单链路传播不能外推为这些能力。
- Logs、Events 超过本批 7 日默认策略的长期容量、rollover、磁盘水位和成本合同；VictoriaMetrics 的实际 30 日物理回收时序；Trace 长期存储。
- Monitor 控制面 HA、状态层 HA、生产 TLS/SASL、跨地域、多活和 Kubernetes。

## 声明规则

- `implementation_complete`：计划内文件和行为已实现。
- `validation_complete`：冻结候选和要求的 evidence 完整且可复核。
- `target_met`：固定能力阈值达到。
- `boundary_found`：执行完整但阈值未达到或发现明确边界。
- 验收程序错误、候选漂移、证据缺失或不安全清理属于 `validation_incomplete`，不能产生能力结论。
