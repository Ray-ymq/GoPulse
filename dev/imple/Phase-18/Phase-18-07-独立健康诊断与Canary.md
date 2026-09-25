# Phase-18-07：独立健康诊断与 Canary 实施方案

> 目标版本：`2.0.7`
> 开发分支：`develop/2.0.7`
> 批次边界：诊断与 canary，不改变容量和背压参数

## 1. 批次目标

提供不依赖 Kafka、VictoriaMetrics 或观测 Elasticsearch 回传的 lifecycle 健康与 canary receipt，
确保观测数据面正常、单点故障和组合故障时都能在短时限内给出结构化判定。

## 2. 实施范围

- `status --json`、`doctor --json` 和 `verify --canary --json --receipt <path>` 使用同一版本化 schema。
- 输出包含 candidate、generated_at、overall status 和每项 `id/status/reason/facts`。
- `facts` 只允许 schema 声明的有限值和 digest，禁止 DSN、token、payload 和私有路径。
- status 直接读取 Docker inventory 和组件 Probe；doctor 限时并发检查 MySQL、Redis、RabbitMQ、
  Kafka、双 ES、VictoriaMetrics、Outbox 和 Monitor owner。
- canary 通过唯一 edge 写代表业务事实，并通过正式 Router 注入 Metrics/Logs/Events；
  lifecycle 直接查询目标存储，不通过被测可观测链路回报自己。
- receipt 同目录临时写、fsync、rename；拒绝 symlink 和非归属路径。

## 3. 固定 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| preflight/schema | 5 |
| 全部正常 | 8 |
| Kafka 不可用 | 12 |
| VictoriaMetrics 不可用 | 12 |
| 观测 Elasticsearch 不可用 | 12 |
| 三者同时不可用 | 12 |
| 恢复后 canary 与 cleanup | 10 |
| 聚合、secret scan 与余量 | 4 |
| 主运行器总计 | 75 |

每个诊断调用自身默认 deadline 不超过 120 秒；整个 shard 不超过 20 分钟。
静态门禁最多 20 分钟，批次串行总预算不超过 95 分钟。

## 4. 不在本批范围

- 新业务 API、新 dashboard、新插件或生产监控集群。
- 将诊断 receipt 发回 Kafka/VM/观测 ES。
- 修改已冻结的容量、扩容或背压参数。

## 5. 验收与完成条件

1. 五种数据面状态都在规定时间内输出完整 JSON 和原子 receipt。
2. 结果能区分进程故障、依赖故障、积压和数据不收敛。
3. 三个观测依赖同时不可用时 stdout/本地 receipt 仍可用。
4. canary 不污染真实用户数据，清理有强归属且无 Secret/路径泄漏。
5. 主运行器小于 75 分钟，全部门禁小于 120 分钟。
6. 完成提交更新 `VERSION=2.0.7`。

## 6. 固定验证命令

```bash
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-07 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 4500
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.7 --base-ref upstream/main
git diff --check
```

创建同名实施记录，记录每种故障状态的诊断耗时和恢复结果。
