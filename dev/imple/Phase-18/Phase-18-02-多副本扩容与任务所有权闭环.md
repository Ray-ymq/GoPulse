# Phase-18-02：多副本扩容与任务所有权闭环实施方案

> 目标版本：`2.0.2`  
> 开发分支：`develop/2.0.2`  
> 验收拓扑：Backend×3，Business Worker/Search Indexer/Router/Marshaller×2，Monitor×1

## 1. 批次目标

在 Phase-18-01 固定数据与负载上，使五类可扩展组件形成可运行的多副本 Compose 拓扑，
对 Outbox、RabbitMQ、Kafka、告警和 Monitor 插件逐项锁定所有权，并在本批内用同条件的
单副本/多副本成对测量和逐实例替换证明结果。Phase-18-01 的失败基线只用于提供已知问题、
第一瓶颈和固定输入，不作为扩容分母或通过 evidence。

## 2. 前置条件

- Phase-18-01 已合入主线；允许其收口为“单副本失败基线已交付，重复性与恢复门禁未通过”，
  而不是要求 `capacity.json` 通过。
- 以下失败基线输入必须可复核：`capacity-failure.json`、三轮原始资源样本、逐窗口诊断、
  recipe/负载报告 schema、候选与宿主绑定、corpus SHA-256 和负载源码 commit。
- Phase-18-01 的 1.3 倍扩容判断不直接复用三轮中的任何单轮，也不以低延迟轮作为分母。
- 记录第三轮 18 次 500 及 request-id 线索；本批默认不修复，只有复现、扩大或证明与扩容
  路径直接相关时才启动单独的最小修复，不借容许多副本结果掩盖。
- 从合入后的最新 `upstream/main` 创建 `develop/2.0.2`，不复用已完成分支。
- 使用总方案的 8 vCPU/12 GiB 宿主、固定 seed 和测量窗口。

## 3. 实施范围

### 3.1 Compose 拓扑与唯一入口

- 基础 Compose 取消 Backend 宿主端口发布，仅 Frontend edge 对外；现有 Nginx Docker DNS 解析继续在请求时发现 Backend 副本。
- 提供受管 scale overlay/runner，固定启动 Backend 3 个副本，Worker、Indexer、Router、Marshaller 各 2 个副本，Monitor 1 个副本。
- 副本使用 Compose 身份作为 instance label，不将容器 ID 或高基数 IP 写入公共指标。
- 调试直达端口仅在临时 loopback override 中分配，验收结束必须清理。

### 3.2 任务所有权

- Backend：每个副本都可处理 HTTP；Outbox claim 使用唯一 owner 和 lease expiry，只有当前 owner 可 mark/release；
  alert scheduler 使用 rule revision + lease owner/until fencing，过期实例不得提交。
- Business Worker/Search Indexer：使用同队列 competing consumers、有限 prefetch 和手动 ack；重投通过业务幂等键/确定性搜索 ID 收敛。
- Router：无本地持久所有权，两副本使用同 topic；仅 Kafka broker ack 后报成功。
- Kafka topic 从 1 partition 确定性扩为 4 partitions，不允许在已有 topic 上降低分区数。
- Marshaller：两副本使用同 group，按 partition lease 处理；丢失/revoke 后立即取消写入，旧 owner 不得晚 commit。
- Monitor：单副本保持插件卷所有权。对相同卷尝试启动第二实例必须在启动插件前非零退出，不修改 registry/process 事实。

### 3.3 多副本指标与状态

- 所有可扩展组件的私有指标提供当前在途、处理/拒绝计数、最后成功时间和有限所有权摘要。
- 公共指标 label 只允许 component/instance 等有限维度，不加 message ID、user ID、partition-offset 或容器 ID。
- 一个副本退出不使整组件伪报就绪；至少一个可服务副本和所需依赖存活时保持业务可用。

### 3.4 扩容对比与替换矩阵

- 对 Backend 使用相同混合负载，对 Worker/Indexer/Marshaller 使用固定积压，对 Router 使用固定并发发布；依赖、数据和单副本资源上限不变。
- 每类组件的 1.3 倍结论必须来自 Phase-18-02 内新执行的单副本/多副本成对测量。两侧使用同一
  `2.0.2` 候选/manifest、同一 corpus 哈希、同一负载源码 commit 和配置、同一初始数据状态和宿主；
  记录执行顺序、两侧完整 report 和计算过程。不得从 Phase-18-01 旧三轮中挑选某个低延迟轮作为分母。
- 多副本吞吐或积压排空速率至少达到同批单副本的 1.3 倍。单副本控制结果即使失败也按原值进入
  比值或标记为不可判定，不放宽门禁、不替换控制轮。
- 稳定 P95/P99、三轮重复性、端到端收敛和完整 SLO 的记录可作为后续输入，但不在本批收口；
  最终门禁由 Phase-18-05 判定。
- 依次强制替换 Backend、Worker、Indexer、Router 和 Marshaller 的一个副本，在途数据要么完成，要么回到队列/未提交 offset。
- 替换窗口内无已接受业务丢失，通知、搜索文档、可观测文档和 active incident 不重复；
  收敛情况如实记录，但完整收敛时限和 SLO 留给 Phase-18-05。

## 4. 不在本批范围

- 双 Elasticsearch、生产 HA、跨宿主调度或 Monitor 分布式插件所有权。
- 除多副本所必须的连接/所有权修正以外的通用性能优化。
- 端到端背压、独立 canary 或合同生成，分别由后续批次收口。

## 5. 预计直接影响

- Compose 基础/扩容/验收 overlay、Frontend Nginx 与内部网络边界。
- Kafka topic 初始化、多副本身份与组件指标。
- Outbox、alert、Rabbit consumer、Kafka ownership 及直接证明这些边界的最小测试。
- 扩容/替换 runner、evidence 和同名实施记录。

## 6. 验收与完成条件

1. 外部只能访问唯一 edge，Backend 三副本都收到请求，移除任一副本时另外两个继续服务。
2. Worker、Indexer、Router、Marshaller 各两副本共同处理，且由本批成对测量满足 1.3 倍相对扩容线。
3. Outbox 排空与 lease 所有权、alert lease、Rabbit ack、Kafka rebalance/fencing 和 Monitor
   单所有者的正反场景通过；约 38K pending 的已知失败不再被当作可忽略基线。
4. 逐实例替换不导致越权提交、重复副作用或已接受数据丢失；完整重复性、收敛与 SLO 不在此处收口。
5. 资源、Secret、端口和 project 归属检查通过，无孤儿容器/网络/卷。
6. 第三轮 18 次 500 未被本批扩大的结果掩盖；复现或扩大时已有独立追踪项和最小修复入口。

## 7. 固定验证命令

```bash
scripts/verify-phase18-scaling.sh --self-test
scripts/verify-phase18-scaling.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
python3 scripts/verify-phase18-evidence.py --scaling "$GOPULSE_PHASE18_WORK/evidence/scaling.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

创建同名实施记录，更新 `VERSION=2.0.2`，只提交本批文件后停止。
