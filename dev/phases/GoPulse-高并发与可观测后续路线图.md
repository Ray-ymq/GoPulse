# GoPulse 高并发与可观测后续路线图

> 规划基线：2026-09-28，`upstream/main=9730c57`，根 `VERSION=2.0.5`。
>
> Phase 18 已完成并关闭。本文只为 Phase 19 分配版本和顺序；Phase 19 之后的方向不预留
> Phase、版本或分支。

## 1. 项目定位

GoPulse 不再把“社交业务”和“可观测系统”作为两条互不相干的功能线。后续统一定位为：

> 以社交业务作为真实负载，以自研可观测链路作为反馈系统，通过可重复的容量与故障实验，
> 证明系统在指定资源、负载和故障条件下的正确性、吞吐、延迟、降级与恢复边界。

因此，后续工作的判断标准不是新增多少组件，而是能否回答：

- 在什么数据规模、硬件和负载模型下达到什么吞吐与尾延迟；
- 压力超过边界时在哪里拒绝、积压或降级；
- 已接受的同步请求、Outbox、RabbitMQ、搜索投影和通知何时最终闭合；
- Metrics、Logs、Events 从产生到可查询需要多久，是否发生丢弃或高基数失控；
- 故障发生后多久恢复，恢复过程中是否重复、丢失或污染业务事实。

## 2. 当前事实

截至 `2.0.5`，GoPulse 已完成：

- 完整社交业务、搜索、通知、插件、Metrics、Logs、Events、内部告警与双 Frontend；
- Linux `amd64` Bundle、备份恢复、完整 Compose 生命周期与机器运行时合同；
- Backend、Business Worker、Search Indexer、Router、Marshaller 计算层多副本；
- 业务搜索 ES 与可观测 ES 隔离、有界队列/连接池/in-flight、背压和独立诊断；
- Phase-18-05 固定故障矩阵 `target_met`。

当前仍未证明：

- 指定资源规格下稳定承载 `150 RPS` 或其他公开容量目标；
- 运行时 P95/P99 尾延迟和异步端到端新鲜度能够由产品自身持续解释；
- Monitor 控制面高可用以及 MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、
  VictoriaMetrics 状态层高可用；
- Logs/Events/时序数据的完整保留、容量和删除合同；
- 分布式 Trace、生产 SLO、Kubernetes、跨地域或多活能力。

已验证、未验证和历史边界的当前清单见
[`docs/capability-status.md`](../../docs/capability-status.md)。

## 3. 已排期：Phase 19

Phase 19 使用 `2.1.x`，目标是先补齐容量诊断信号，再建立与 Phase 18 故障矩阵相互独立的
容量认证。它不借容量测试之名开放任意性能优化。

| 顺序 | 版本 | 批次结果 |
| --- | --- | --- |
| Phase-19-01 | `2.1.1` | 探针与业务准入隔离，固定低基数尾延迟和饱和度指标 |
| Phase-19-02 | `2.1.2` | 冻结容量 profile、统计规则、runner、采样和 evidence 合同 |
| Phase-19-03 | `2.1.3` | 执行冻结容量认证，发布容量曲线、第一瓶颈和能力结论 |

Phase 19 使用两个互不替代的状态：

- **执行状态**：`complete` 或 `incomplete`。候选漂移、验收程序错误、证据缺失、清理不安全均为
  `incomplete`，不能更新完成版本。
- **能力状态**：`target_met` 或 `boundary_found`。只有执行完整后才能产生；发现边界仍是有效事实，
  但不得描述为容量通过。

性能统计不沿用 Phase 18 的“恰好两轮”规则。Phase-19-02 必须在代码和证据中冻结重复次数、
阶梯负载、窗口、资源规格、停止条件和聚合算法；Phase-19-03 不得临时更改。

权威批次、版本、分支和完成条件见
[`Phase-19 总实施方案`](../imple/Phase-19/Phase-19-总实施方案.md)。

## 4. Phase 19 之后的发展顺序

以下方向只能根据 Phase 19 的容量与诊断事实逐项立项，目前不分配 Phase 或版本。

### 4.1 Trace、异步关联与观测数据生命周期

- 使用 W3C Trace Context / OpenTelemetry 语义贯穿 HTTP、事务 Outbox、RabbitMQ、
  Worker、Indexer 和 Elasticsearch；Trace ID 进入日志与消息上下文，不进入指标标签。
- 建立 accepted → published → consumed → projected/queryable 的端到端延迟和新鲜度指标。
- 为 Logs、Events 和 VictoriaMetrics 定义保留、rollover、磁盘水位、删除失败、容量预测和恢复合同。
- Router/Marshaller 保留自研学习价值，但入口与输出优先兼容公开标准，避免形成封闭协议孤岛。

### 4.2 Monitor 控制面高可用

- 将插件期望状态与本地进程状态分离；期望状态可持久化和恢复。
- 为目标调度引入 lease、fencing token 和接管语义，证明不会重复启动或双写。
- 验证 Monitor 故障只形成有界观测空窗，不阻断社交业务或破坏插件配置。

### 4.3 状态层高可用与数据恢复

只按容量、RPO/RTO 和故障证据逐项引入：MySQL、RabbitMQ、Kafka、两个 Elasticsearch、
VictoriaMetrics。Redis 始终保持可丢弃缓存角色。每种状态服务独立立项，不一次堆叠全部集群。

### 4.4 Kubernetes 与 SRE

只有多副本语义、容量认证和至少一个状态层恢复合同完成后，才迁移 Kubernetes：

- HPA 使用并发、队列深度、lag 或新鲜度等已验证饱和信号，不只使用 CPU；
- 保留 Compose 作为确定性开发与回归环境；
- 增加 SLI/SLO、error budget、告警风暴控制、变更/回滚和灾难恢复演练。

已有 Kubernetes 设计仍保存在 [`docs/future/kubernetes`](../../docs/future/kubernetes/README.md)，
不构成已实现能力。

## 5. 明确不做

- 不以新增中间件、微服务、分库分表、CQRS 或 Kubernetes YAML 代替容量证据。
- 不在 Phase 19 中顺手优化尚未被证据识别为第一瓶颈的 SQL、缓存或消息参数。
- 不把平均延迟当成 P95/P99，不把一次最好结果外推为稳定容量。
- 不让日志、事件、指标或告警失败回压核心业务事实写入。
- 不把版本号、测试执行完成或 evidence 完整描述为能力达标。

## 6. 最近的可执行入口

下一项产品开发是 Phase-19-01。开始前必须先将本规划和 Phase 19 总实施方案合入 primary remote
`main`；随后获取最新 `upstream/main`，读取 Phase 19 总方案与 Phase-19-01 拆分方案，从最新
`upstream/main` 创建 `develop/2.1.1`。在 Phase-19-01 完成并合入前，不创建后续开发分支。
