# Phase-18-06：全链路有界背压实施方案

> 目标版本：`2.0.6`
> 开发分支：`develop/2.0.6`
> 前置：Phase-18-05 双 Elasticsearch 已合入

## 1. 批次目标

为 HTTP、MySQL pool、Outbox、RabbitMQ、Router/Kafka、Marshaller/VM 和
Marshaller/观测 ES 建立硬上限、显式拒绝、可观察积压和有界恢复。每层单独制造压力并在短窗口内判定，
不运行一个串联全部故障的长矩阵。

## 2. 实施范围

- Frontend/Backend 限制连接、body、在途请求和路由 timeout；饱和返回 `503`、
  `server_overloaded` 和 `Retry-After: 1`。
- MySQL max-open/max-idle/lifetime/wait timeout 强类型配置，多 Backend 总预算低于服务容量。
- Outbox 高/低水位阻止新增异步写入，超出量可由在途请求上限推导。
- Rabbit main/retry/dead 同时设置消息数/字节上限和 `reject-publish`；nack/return 回到 Outbox 或 requeue。
- Router 使用 records/bytes 双上限，只在 Kafka ack 后成功；Kafka retention/segment 和磁盘预检固定。
- Marshaller 每 partition 一条在途，sink client 的连接、body、timeout 和 retry backoff 有硬上限；
  store 成功后才 commit。
- Router 接受前的本地日志/事件丢弃必须有 counter；Router 接受后不得静默丢失。
- 全部上限进入 runtime contract、`.env.example` 和验证器，不能只存在于验收脚本。

## 3. 固定 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| preflight/runtime limits | 5 |
| HTTP 与 MySQL pool | 10 |
| Outbox watermarks | 10 |
| Rabbit main/retry/dead | 12 |
| Router 与 Kafka | 10 |
| Marshaller 与 VictoriaMetrics | 10 |
| Marshaller 与观测 ES | 10 |
| 解除压力与跨层不变量 | 7 |
| 聚合、secret scan 与清理 | 8 |
| 主运行器总计 | 82 |

每个 shard 不超过 20 分钟；静态/合同门禁最多 20 分钟，批次串行总预算不超过 102 分钟。

## 4. 判定规则

- 每层只允许规定的显式拒绝或积压，不允许 OOM、连接挂死、无界 goroutine/队列/重试/日志增长。
- 压力解除后，本 shard 注入的积压必须在 5 分钟内排空；搜索/通知和可观测新数据满足 30/60 秒收敛。
- 一个 shard 失败不触发同候选重跑；只保留失败附件并结束主任务或执行明确无依赖的清理。
- 如果无法在 20 分钟内证明某层，回到 `update` 拆分该层，不提高 timeout。

## 5. 验收与完成条件

1. 七层的正常、饱和、拒绝、指标和恢复场景分别通过。
2. 已接受业务无静默丢失，拒绝请求不产生部分副作用。
3. 无 OOM、无界增长、伪成功或超过预算的恢复等待。
4. runtime contract、示例配置和实际 Compose 零漂移。
5. 主运行器小于 90 分钟，全部完成门禁小于 120 分钟。
6. 完成提交更新 `VERSION=2.0.6`。

## 6. 固定验证命令

```bash
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-06 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.6 --base-ref upstream/main
git diff --check
```

创建同名实施记录，记录各层实际注入量、恢复时间和任务实耗。
