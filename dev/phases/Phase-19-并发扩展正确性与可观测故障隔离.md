# Phase 19：并发扩展正确性与可观测故障隔离

## 阶段目的

把 Phase 18 已识别的单副本边界转化为可验证的计算层多副本能力，并拆开业务搜索与可观测
存储故障域。阶段结果是“正确扩展且可解释地退化”，不是吞吐 SLO 或生产级高可用认证。

## 最小端到端结果

```text
单一入口
  → Backend 多副本
  → MySQL / Redis / Outbox
  → Business Worker 与 Search Indexer 多副本

Monitor 与日志源
  → Router 多副本
  → 多分区 Kafka
  → Marshaller 多副本
  → VictoriaMetrics / 独立观测 Elasticsearch
```

业务搜索使用独立 Elasticsearch。任一计算实例退出、扩容或缩容时，系统不产生重复业务事实、
错误 offset 提交、租约越权或不可恢复积压。可观测存储故障不得阻断业务搜索和核心社交请求。

## 优先级

- **P0**：Backend、Worker、Indexer、Router、Marshaller 的多副本所有权与恢复正确性。
- **P0**：业务搜索与可观测 Elasticsearch 分离，代表性存储故障不跨故障域传播。
- **P1**：每层显式背压、实例身份、独立探针和候选绑定证据。
- **P1**：机器合同单一来源和跨代码、Compose、验收脚本的漂移检查。

## 验收方向

- 两个 Backend 实例承接同一会话流量，不需要粘性会话；停掉任一实例后请求继续。
- 至少两个 Worker 和两个 Indexer 实例共同消费，重投、重连和缩容后业务事实与搜索最终闭合。
- 至少两个 Router 与两个活跃 Marshaller group member 处理多个 Kafka partition，rebalance
  期间不越权提交旧 generation offset。
- 业务与观测 Elasticsearch 使用不同服务、卷、地址和网络边界；任一侧故障不拖垮另一侧。
- 队列、连接池、Kafka producer、消费者和远程写入均有可观测上界与明确拒绝/等待/降级语义。
- 每个副本可通过不依赖 Monitor、Kafka、Marshaller 或存储查询链路的通道检查探针和关键状态。
- 候选在固定扩缩容与故障矩阵后，Outbox、RabbitMQ、Kafka 和目标存储达到定义的最终闭合状态。

## 阶段边界

- Monitor 继续保持单写者，负责插件生命周期和采集；本阶段不设计 Monitor 主从或插件迁移。
- MySQL、Redis、RabbitMQ、Kafka、VictoriaMetrics 和两个 Elasticsearch 仍可使用单节点服务；
  本阶段证明计算并发和故障隔离，不宣称状态层 HA。
- 不新增业务功能、外部告警、微服务拆分、Schema Registry、SASL/TLS 或 Kubernetes 资源。
- 不复用 Phase 18 运行次数或 evidence，不在本阶段发布 `150/300 RPS` 容量达标结论。

精确批次、版本和分支见
[`Phase-19-总实施方案`](../imple/Phase-19/Phase-19-总实施方案.md)。
