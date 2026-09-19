# Phase-18-02：多副本扩容与任务所有权闭环实施方案

> 目标版本：`2.0.2`  
> 开发分支：`develop/2.0.2`  
> 验收拓扑：Backend×3，Business Worker/Search Indexer/Router/Marshaller×2，Monitor×1

## 1. 批次目标

在 Phase-18-01 固定数据与负载上，使五类可扩展组件形成可运行的多副本 Compose 拓扑，
对 Outbox、RabbitMQ、Kafka、告警和 Monitor 插件逐项锁定所有权，并用扩容对比和逐实例替换证明结果。

## 2. 前置条件

- Phase-18-01 已合入主线，其 recipe、负载器、报告 schema 和三轮单副本基线可复核。
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
- 每类组件的多副本吞吐或积压排空速率至少达到单副本 1.3 倍，且 P95/P99 不超出总方案 SLO。
- 依次强制替换 Backend、Worker、Indexer、Router 和 Marshaller 的一个副本，在途数据要么完成，要么回到队列/未提交 offset。
- 替换窗口内无已接受业务丢失，通知、搜索文档、可观测文档和 active incident 不重复，最长 10 分钟收敛。

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
2. Worker、Indexer、Router、Marshaller 各两副本共同处理且满足 1.3 倍相对扩容线。
3. Outbox/alert lease、Rabbit ack、Kafka rebalance/fencing 和 Monitor 单所有者的正反场景通过。
4. 逐实例替换不导致越权提交、重复副作用或已接受数据丢失，恢复时限不超过 10 分钟。
5. 资源、Secret、端口和 project 归属检查通过，无孤儿容器/网络/卷。

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
