# Phase 19：并发扩展正确性与可观测故障隔离总实施方案

> 规划基线：2026-09-27，`upstream/main=ce08b1b`，根 `VERSION=2.0.2`。
>
> `2.1.0` 仅作为 Phase 基线保留，不发布。可执行批次从 `2.1.1` 开始。

## 1. 阶段目的

Phase 19 承接 Phase 18 固定的后续输入：多副本正确性、双 Elasticsearch 隔离与背压、
独立健康、机器合同单一来源，以及 Outbox/事件闭合未证明项。

本阶段把当前完整单副本 Compose 产品提升为代表性的计算层多副本产品，并证明扩容、缩容、
实例故障和存储故障下的所有权、幂等、提交、恢复与诊断边界。它不重新执行 Phase 18 的容量
资格判定，也不承诺生产级 HA。

## 2. 固定架构边界

- MySQL 继续是业务事实源，Redis 继续是可丢弃缓存。
- RabbitMQ 继续只承载业务异步任务；Kafka 继续只承载 Metrics、Logs、Events。
- Monitor 保持单实例和插件生命周期唯一所有者；本阶段不扩展 Monitor。
- Backend、Business Worker、Search Indexer、Router 和 Marshaller 必须支持至少两个副本。
- Kafka Topic 必须具有足以让至少两个 Marshaller group member 同时拥有 partition 的分区数；
  broker 仍可为单节点，不能据此宣称 Kafka HA。
- 业务搜索 Elasticsearch 与 Logs/Events Elasticsearch 使用独立服务、数据卷和网络/配置边界。
- Frontend 仍是唯一浏览器入口，浏览器不得直连任一内部副本、数据服务或可观测组件。
- 每个实例具有有界、非敏感的实例身份；身份进入日志、指标和验收证据，不进入授权决策。

## 3. 权威批次、版本与分支

| 批次 | 目标版本 | 分支 | 交付结果 |
| --- | --- | --- | --- |
| Phase-19-01 | `2.1.1` | `develop/2.1.1` | 业务计算层多副本与异步闭合 |
| Phase-19-02 | `2.1.2` | `develop/2.1.2` | 可观测计算层多副本、双 ES 与背压隔离 |
| Phase-19-03 | `2.1.3` | `develop/2.1.3` | 合同单一来源、独立诊断与完整矩阵收口 |

本表是 Phase 19 唯一有效分配。每个分支都从开始该批时最新的 primary remote `main`
创建，前一批必须已经合并并更新根 `VERSION`。不得提前创建后续分支。

## 4. Phase-19-01：业务计算层多副本与异步闭合

### 4.1 目标

让 Backend、Business Worker 和 Search Indexer 在同一候选中以至少两个副本共同工作，
并证明同步请求、Outbox、通知和搜索在实例退出与重建时保持正确。

### 4.2 实施范围

- 建立明确的内部负载均衡路径，使同一登录会话可以跨 Backend 副本工作，不依赖粘性会话。
- 识别 Backend 内全部后台职责。Outbox 继续使用 owner/lease/fencing 支持多 owner；告警调度等
  单写职责必须使用数据库租约/唯一约束或拆成唯一进程，不能依赖“通常只有一个 Backend”。
- 将 MySQL 连接池、HTTP 并发入口和相关等待上界纳入强类型配置；按整套候选的总连接预算分配，
  不通过每副本固定放大连接数制造数据库过载。
- Business Worker 与 Search Indexer 使用唯一 consumer identity、现有幂等/ack/requeue 语义和
  有界 prefetch；副本退出必须停止取新消息并在预算内完成或安全重投当前消息。
- 为 Backend、Worker、Indexer 增加稳定的实例级日志与组件指标身份，并保证标签基数有界。
- 补齐 Outbox/event-state 最终闭合证据：验收能够区分 accepted、pending、leased、published、
  dead/retry 和最终业务投影，而不只观察某一时刻 pending 数。
- 建立最小多副本业务矩阵：跨副本登录/请求、并发写、停一个 Backend、停一个 consumer、
  consumer 扩缩容、RabbitMQ 短故障、搜索存储短故障和最终收敛。

### 4.3 完成条件

1. 至少两个 Backend 实例都实际承接过请求，停掉任一实例后已登录会话继续完成代表性业务。
2. 至少两个 Worker 与两个 Indexer 实例都实际处理过消息；扩缩容和故障窗口后无重复通知、
   丢失通知、越权 ack 或搜索永久缺失。
3. Outbox owner/lease/fencing、确认后完成和满批连续 claim 在多 owner 下保持成立；最终闭合证据完整。
4. 单写后台职责在多 Backend 下不会重复执行或产生重复持久事实。
5. 连接、prefetch、队列和关闭预算有显式上界，相关最小测试及业务回归通过。
6. 创建同名实施记录，更新 `VERSION=2.1.1`，提交后停止；不提前实现 19-02。

## 5. Phase-19-02：可观测计算层多副本、双 ES 与背压隔离

### 5.1 目标

让 Router 和 Marshaller 能够水平扩展并安全 rebalance，同时把业务搜索与 Logs/Events 的
Elasticsearch 故障域彻底分开。可观测链路过载或存储故障只能按明确合同拒绝、排队或降级，
不能无界阻塞核心业务。

### 5.2 实施范围

- 为 Monitor 汇聚的 Metrics/Logs/Events 到 Router 建立可验证的多副本入口；至少两个 Router
  都实际生产过记录。
- 将 Kafka Topic 调整为多 partition，并让至少两个 Marshaller group member 同时活跃；保留
  generation ownership、手动 offset、永久坏记录继续和存储成功后提交语义。
- 对 Marshaller 的处理模型建立目标级故障隔离：Logs/Events 存储不可用时，Metrics 仍能写入
  VictoriaMetrics；任一阻塞不能无界占用所有 partition 或 goroutine。
- 建立独立的业务搜索 Elasticsearch 与可观测 Elasticsearch。Backend/search-init/Indexer 只能
  写业务搜索服务；Marshaller 和日志/Events 查询只能使用观测服务；Backend 查询层仅按所需网络
  桥接，不复用一个 URL 或数据卷。
- 明确 Router producer buffer、Monitor/日志源队列、Marshaller in-flight/retry 和存储请求的
  容量、超时和拒绝语义；队列满、远程故障和恢复都产生有界指标，不产生递归遥测风暴。
- 扩展组件采集，使每个 Router/Marshaller 副本的关键指标都可区分且不会被随机 DNS 结果遗漏。
- 建立最小故障矩阵：Router 扩缩容、Marshaller rebalance、Kafka 短故障、观测 ES 故障、
  搜索 ES 故障、VictoriaMetrics 故障和恢复后的 lag/offset/目标存储闭合。

### 5.3 完成条件

1. 至少两个 Router 与两个活跃 Marshaller group member 均有实例级处理证据。
2. rebalance、实例退出和重建期间，旧 generation 不提交 offset；重复写保持目标端幂等或可接受。
3. 业务搜索与观测数据的服务、卷、配置和网络边界分离，任一 Elasticsearch 故障不传播到另一侧。
4. 观测 ES 故障时 Metrics 保持前进；VictoriaMetrics 故障时 Logs/Events 不因全局串行而停止；
   恢复后未确认数据按合同收敛。
5. 所有背压边界均有低层测试、状态指标和集成故障证据，核心业务请求不因遥测队列耗尽而阻塞。
6. 创建同名实施记录，更新 `VERSION=2.1.2`，提交后停止；不提前实现 19-03。

## 6. Phase-19-03：合同单一来源、独立诊断与阶段收口

### 6.1 目标

把前两批形成的副本、依赖、探针、指标、所有权和背压语义固化为机器可校验合同，并用一次
冻结候选的完整矩阵证明扩缩容、局部故障、恢复和最终闭合。

### 6.2 实施范围

- 建立一个权威机器合同，覆盖进程 ID、可扩展/单写角色、监听器、启动/存活/就绪路径、硬/软依赖、
  实例身份、关键队列/连接预算和关停预算；Compose、代码目录、文档和验收脚本通过生成或漂移校验
  与其一致。该合同不是新的运行时配置服务。
- 提供独立诊断路径：验收容器可以逐实例直接检查探针、实例身份和关键容量信号，不经过
  Monitor → Router → Kafka → Marshaller → 存储查询链路，也不新增宿主公开端口。
- 固定候选 revision、镜像 digest、Compose 配置、数据配方、实例数、Kafka partition、总连接预算、
  故障顺序和 evidence schema；候选相关修改后必须重建受影响证据。
- 运行完整矩阵：正常并发流量、业务计算层扩缩容、可观测计算层扩缩容、两个 ES 独立故障、
  Kafka/RabbitMQ 短故障、单实例 SIGTERM、服务重建和最终收敛。
- 记录吞吐、延迟和资源事实用于下一阶段输入，但本批只以正确性、隔离、背压和闭合判定，
  不据此发布容量 SLO。

### 6.3 完成条件

1. 机器合同漂移检查覆盖全部长运行 Go 进程和本阶段新增的副本/存储拓扑。
2. 独立诊断在可观测数据面停止时仍能区分 live、ready、dependency down、queue saturation 和 stopping。
3. 完整矩阵使用同一冻结候选执行；所有已接受业务事件、Outbox、RabbitMQ、Kafka offset/lag、
   搜索投影和可观测写入达到合同定义的终态。
4. 无 OOM、无非预期容器重启、无跨故障域数据写入、无用户/管理员授权降级。
5. 失败时保留原始 evidence 并区分产品失败与验收基础设施失败，不通过改 receipt 或筛选轮次宣称通过。
6. 创建同名实施记录，更新 `VERSION=2.1.3`；Phase 19 与 Milestone 5 完成。

## 7. 阶段固定验收维度

每个拆分方案必须从下列维度选择直接相关检查，并在 Phase-19-03 全部覆盖：

| 维度 | 必须证明的事实 |
| --- | --- |
| 请求 | 跨 Backend 副本会话、授权、幂等和错误合同一致 |
| 业务消息 | Outbox、RabbitMQ、Worker/Indexer 所有权与最终闭合 |
| 可观测消息 | Router buffer、Kafka partition/group、Marshaller generation/offset 正确 |
| 存储 | MySQL 事实、搜索 ES、观测 ES、VictoriaMetrics 故障域清晰 |
| 背压 | 队列/连接/in-flight 有界，拒绝或降级可观测且不拖垮核心业务 |
| 生命周期 | scale out/in、SIGTERM、重启和依赖恢复满足预算 |
| 诊断 | 每个实例可脱离可观测数据面直接定位状态 |
| 安全 | 内部副本不对浏览器暴露，身份和错误不泄密，权限不降级 |

## 8. 非目标与停止条件

- 不实现 MySQL 主从、Redis Sentinel/Cluster、RabbitMQ quorum 集群、多 broker Kafka、
  Elasticsearch 集群或 VictoriaMetrics cluster。
- 不实现 Kubernetes、HPA、Service Mesh、跨地域、多活、生产 TLS/SASL 或外部告警。
- 不增加业务功能，不按微服务拆分现有 Backend，不开展一般代码审计或依赖升级活动。
- 不为得到更高 RPS 进行无证据的索引、缓存、连接池或中间件参数优化。
- Phase-19-03 完成后立即停止。下一步只能基于本阶段证据重新规划容量认证；未排期方向不得
  直接占用 `2.1.x` 的其他 patch。

## 9. 计划维护

- 每个批次开始前创建同名拆分实施方案，固定范围、验收命令、回归范围和完成条件。
- 若未开始批次需要拆分或调整顺序，先更新本文并重新计算尚未创建的分支。
- 已创建或已推送分支不得静默改号。
- 实际修改、命令、结果、偏差和限制只写入对应 `dev/logs/Phase-19/` 实施记录。
