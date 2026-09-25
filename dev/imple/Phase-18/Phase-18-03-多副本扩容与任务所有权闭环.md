# Phase-18-03：多副本扩容与任务所有权闭环实施方案

> 目标版本：`2.0.3`
> 开发分支：`develop/2.0.3`
> 验收拓扑：Backend×3，Business Worker/Search Indexer/Router/Marshaller×2，Monitor×1

## 1. 批次目标

在 Phase-18-01 固定数据与业务请求构成以及 Phase-18-02 已取得资格的验收基础设施上，
使五类可扩展组件形成可运行的多副本 Compose 拓扑，对 Outbox、RabbitMQ、Kafka、告警和
Monitor 插件逐项锁定所有权，并在本批内先完成多副本正确性与逐实例替换，再执行同候选的
单副本/多副本容量测量。Phase-18-01 的失败基线和原 Phase-18-02 失败探索只作为已知问题、
第一瓶颈和固定输入，不作为扩容分母或通过 evidence。

## 2. 前置条件

- Phase-18-01/02 已合入主线；Phase-18-01 允许收口为“单副本失败基线已交付，重复性与恢复门禁未通过”，
  而不是要求 `capacity.json` 通过。
- Phase-18-02 的 parser、计时、替换工作保持、失败现场和瓶颈定界资格门禁全部通过；其
  `qualification.json` 与 `bottleneck-diagnostic.json` 可复核，且未把诊断结果冒充本批通过 evidence。
- 以下失败基线输入必须可复核：`capacity-failure.json`、三轮原始资源样本、逐窗口诊断、
  recipe/负载报告 schema、候选与宿主绑定、corpus SHA-256 和负载源码 commit。
- 扩容判断不直接复用 Phase-18-01 三轮或原 Phase-18-02 失败探索中的任何单轮，也不以低延迟轮作为分母。
- 记录第三轮 18 次 500 及 request-id 线索；本批默认不修复，只有复现、扩大或证明与扩容
  路径直接相关时才启动单独的最小修复，不借容许多副本结果掩盖。
- 从合入后的最新 `upstream/main` 创建 `develop/2.0.3`，不复用原本地 `develop/2.0.2` 或其候选。
- 使用总方案的 8 vCPU/12 GiB 宿主和固定 seed；扩容及替换窗口按 §3.4 执行。

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

### 3.4 正确性、替换与扩容矩阵

- 对 Backend 使用相同 1,024 用户池和混合请求构成，由 128 个并发执行者轮流使用全部用户，
  在 15 秒预热后做 90 秒无目标 RPS 的持续并发测量；每个执行者在前一请求完成后立即发起
  下一请求，按稳定窗口成功完成数/实测时长计算吞吐。原 150/300 RPS 固定目标只用于容量
  场景及 Backend 替换；替换窗口为 15 秒预热、120 秒 150 RPS 稳态和 30 秒 300 RPS 突发，
  并在稳态中途替换。Worker/Indexer/Marshaller 使用 Phase-18-02 已取得资格的固定积压释放与
  稳态计时；Router 使用宿主上的持久连接发布器、固定总并发 64 和 100,000 条消息，经受管
  临时 loopback 端口直达副本并核对 Kafka offset。各对比的依赖、初始数据和产品配置不变。
- 正式矩阵先执行 Backend、Worker、Indexer、Router、Marshaller 的逐实例替换和 runtime ownership，
  再执行五类组件的容量对比。各场景使用独立 project 和初始快照；一个场景失败后记录完整失败
  现场并继续执行与其无依赖的场景。只有全部阻断场景通过时才生成 `scaling.json`；存在失败时
  生成 `scaling-failure.json`，不得将其包装为通过 evidence。
- 每类组件的扩容结论必须来自 Phase-18-03 内新执行的单副本/多副本成对测量。两侧使用同一
  `2.0.3` 候选/manifest、同一 corpus 哈希、同一负载源码 commit 和配置、同一初始数据状态和宿主；
  记录执行顺序、两侧完整 report 和计算过程。不得从 Phase-18-01 旧三轮中挑选某个低延迟轮作为分母。
- Backend 三副本必须都收到请求，并在固定 150/300 RPS 替换窗口内保持零非预期错误、零超时、
  零已接受业务丢失和剩余副本持续服务。无 CPU 隔离的固定 8 vCPU 宿主上，单副本/三副本
  90 秒闭环饱和吞吐只作为容量特征记录，不设阻断比值，也不得据此宣称获得水平容量增益。
  Worker、Indexer、Router、Marshaller 的多副本吞吐或积压排空速率仍必须达到同批单副本的
  1.3 倍；若 Phase-18-02 已用瓶颈证据触发合同修订，本条以 Phase 总方案在本批开工前形成的
  新门禁为准，不允许本批看到结果后再降低门槛。单副本控制结果即使失败也按原值进入比值或
  标记为不可判定，不替换控制轮。
- 正式矩阵前执行确定性预检：候选与工作区绑定、宿主资源（Linux 工作区可用磁盘不少于 80 GiB）、
  拓扑、负载器、recipe 和 Phase-18-02 资格 receipt 全部通过。正式矩阵对每个冻结候选只运行一次，
  不在相同 revision 上重试或筛选通过结果。新候选必须对应已记录且完成定向验证的产品、配置、
  资源合同或验收脚本变化，并使用全新隔离工作区；所有通过 evidence 必须来自同一候选。
- 稳定 P95/P99、三轮重复性、端到端收敛和完整 SLO 的记录可作为后续输入，但不在本批收口；
  最终门禁由 Phase-18-06 判定。
- 依次强制替换 Backend、Worker、Indexer、Router 和 Marshaller 的一个副本，在途数据要么完成，要么回到队列/未提交 offset。
- Search Indexer 替换验收使用原始 5,000 条固定积压，并在替换副本健康就绪后再发布 `prefetch + 1`
  条确定性 `post.created` Outbox 事件；暂时暂停另一副本以保证替换副本亲自确认至少一条，随后
  恢复对等副本并核对全部新增确认、零重复索引副作用和最终空队列。替换副本无新任务可处理时
  不得将“未观察到计数增长”判作产品失败。
- 替换窗口内无已接受业务丢失，通知、搜索文档、可观测文档和 active incident 不重复；
  收敛情况如实记录，但完整收敛时限和 SLO 留给 Phase-18-06。

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
2. Backend 三副本完成固定负载替换和持续服务门禁，饱和吞吐比值作为非阻断容量特征如实记录；
   Worker、Indexer、Router、Marshaller 各两副本共同处理，并满足开工前已冻结的相对扩容门禁。
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
python3 scripts/ci/validate_branch.py --branch develop/2.0.3 --base-ref upstream/main
git diff --check
```

创建同名实施记录，更新 `VERSION=2.0.3`，只提交本批文件后停止。
