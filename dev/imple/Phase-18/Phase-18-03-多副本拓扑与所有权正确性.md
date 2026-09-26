# Phase-18-03：多副本拓扑与所有权正确性实施方案

> 目标版本：`2.0.3`
> 开发分支：`develop/2.0.3`
> 前置：Phase-18-02 限时 runner 已合入并通过

## 1. 批次目标

在唯一 Frontend edge 后建立 Backend×3、Business Worker/Search Indexer/Router/Marshaller×2、
Monitor×1 的受管拓扑，分别证明所有权、ack/commit、fencing 和逐实例替换正确性。

本批不测单/多副本容量比率，不运行三轮容量负载；正确性失败不能用吞吐结果抵消。

## 2. 实施范围

- 基础 Compose 不发布 Backend 宿主端口；临时直达端口必须 loopback-only、强归属且任务后清理。
- Kafka topic 确定性扩为 4 partitions；不可降低已有 partition。
- Backend Outbox/alert 使用 owner、lease expiry 和 fencing，旧 owner 不得晚提交。
- Rabbit consumer 使用 competing consumers、有限 prefetch、手动 ack 和幂等业务键。
- Router 只在 broker ack 后成功；Marshaller revoke/lease 丢失后立即取消写入且不得晚 commit。
- Monitor 第二所有者在启动插件前非零退出，不改变 registry/process 状态。
- 五类 replacement 使用固定持续工作或 release barrier；移除前必须观察目标与存活副本均有进展且
  外部工作仍存在。任务耗尽属于 fixture failure，不是产品失败。
- 每个组件使用独立 shard、project、初始快照和原子附件；某 shard 失败后只允许执行无需其产物的
  诊断 shard，不能把所有场景串成无界矩阵。

## 3. 固定 shard 与预算

| Shard | 最大分钟 | 核心判定 |
| --- | ---: | --- |
| preflight/topology | 10 | 唯一 edge、目标副本数、4 partitions |
| backend-replacement | 10 | 剩余副本持续服务，lease/fencing 正确 |
| worker-replacement | 10 | survivor/replacement 推进，零重复通知 |
| indexer-replacement | 10 | 新 event-id 被替换实例处理，索引收敛 |
| router-replacement | 8 | 已接受消息与 Kafka offset 闭合 |
| marshaller-replacement | 12 | 双 member、rebalance、store-before-commit |
| runtime-ownership | 12 | Outbox、alert、Rabbit、Kafka 所有权 |
| monitor-single-owner | 7 | 第二实例安全退出 |
| aggregate/cleanup | 8 | receipt、secret scan、强归属清理 |
| 主运行器总计 | 87 | 不得增加 shard 或延长 |

每个 shard 超时立即失败；没有自动重试。静态门禁最多 20 分钟，批次串行总预算不超过 107 分钟。

## 4. 不在本批范围

- 1.3 倍容量门槛、Backend 饱和比值和三轮 SLO。
- 双 Elasticsearch、背压调优、诊断 canary、合同生成和最终恢复。
- 与所有权无关的性能优化、依赖升级或通用重构。

## 5. 验收与完成条件

1. 唯一 edge 与固定副本拓扑成立，所有实例有有限维度 identity/counter。
2. 五类逐实例替换均观察到 live work、survivor 和 replacement，零越权提交、重复持久副作用或静默丢失。
3. Outbox/alert/Rabbit/Kafka 正反所有权场景通过，Monitor 第二实例安全退出。
4. 每个 shard 有 before/during/after 附件，失败分类不混淆 observer 与产品。
5. 主 receipt 实耗小于 90 分钟，全部固定门禁串行小于 120 分钟。
6. 完成提交更新 `VERSION=2.0.3`。

## 6. 固定验证命令

```bash
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-03 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.3 --base-ref upstream/main
git diff --check
```

创建同名实施记录并记录各 shard 实耗。任一 shard 超过预算时停止，不进入容量批次。
