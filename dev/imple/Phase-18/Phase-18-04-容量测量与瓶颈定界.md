# Phase-18-04：容量测量与瓶颈定界实施方案

> 目标版本：`2.0.4`
> 开发分支：`develop/2.0.4`
> 前置：Phase-18-03 多副本与所有权正确性已通过

## 1. 批次目标

使用短而高样本量的固定窗口，取得三轮业务容量、四类消费者的单/多副本成对测量以及宿主、
组件和共享依赖的瓶颈证据。本批只接受一个正式候选的一次主运行器结果。

## 2. 测量合同

- 三轮业务负载各为 1 分钟预热、3 分钟 150 RPS、1 分钟 300 RPS、最长 3 分钟恢复。
- 每轮独立 project/快照，但候选、seed、corpus、负载器和资源合同完全相同。
- Worker、Indexer、Router、Marshaller 每组单/多副本采用同一固定输入与受控 release，
  每侧有效窗口 2～3 分钟，并至少取得 20 个资源/进度样本。
- Worker、Indexer、Router、Marshaller 默认要求多副本速率至少为单副本的 `1.3x`。
- Backend 三副本必须分担流量并通过 150/300 RPS；无 CPU 隔离宿主上的饱和比值仅记录。
- 资源采样覆盖宿主、负载器、逐容器 CPU/RSS/I/O，以及 MySQL、RabbitMQ、Kafka、两个当前存储
  和 VictoriaMetrics 的直接压力指标。
- 瓶颈只能分类为 `component`、`shared_dependency`、`load_generator` 或 `inconclusive`。

三分钟稳态约产生 27,000 请求；若结果不稳定或不能在预算内恢复，直接失败，不延长到旧 15 分钟窗口。

## 3. 固定 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| preflight 与输入绑定 | 5 |
| business-round-1 | 12 |
| business-round-2 | 12 |
| business-round-3 | 12 |
| worker single/multi | 8 |
| indexer single/multi | 8 |
| router single/multi 与 Kafka ceiling | 8 |
| marshaller single/multi 与 assignment | 8 |
| 瓶颈聚合、secret scan 与清理 | 8 |
| 主运行器总计 | 81 |

单个 shard 不超过 20 分钟。静态门禁最多 20 分钟，批次串行总预算不超过 110 分钟。

## 4. 失败与计划修订

- 任一轮超时、负载器先饱和、样本不足、输入不一致或恢复超时，产生失败 receipt，不替换该轮。
- 同一候选不得重跑以筛选更好分母或延迟轮。
- 若共享依赖 ceiling 证明 `1.3x` 无法衡量组件扩容，停止并在 `update` 修订总方案；不得在本批降标。
- Phase-18-01 和废止方案的旧轮次只用于问题对照，不参与本批比率。

## 5. 验收与完成条件

1. 三轮 SLO、重复性、错误分类、积压和恢复均有完整原始样本。
2. 四类组件的成对输入与计时一致，达到开工前冻结的扩容门槛。
3. Backend 分流及固定负载通过，饱和比值以非阻断特征记录。
4. 负载器不是首个瓶颈，瓶颈分类有直接资源/依赖证据。
5. 主运行器小于 90 分钟，全部完成门禁小于 120 分钟。
6. 完成提交更新 `VERSION=2.0.4`。

## 6. 固定验证命令

```bash
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-04 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.4 --base-ref upstream/main
git diff --check
```

创建同名实施记录，逐轮记录实耗和失败原因，不运行任何第二次正式候选筛选。
