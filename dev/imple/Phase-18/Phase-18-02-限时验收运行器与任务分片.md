# Phase-18-02：限时验收运行器与任务分片实施方案

> 目标版本：`2.0.2`
> 开发分支：`develop/2.0.2`
> 批次边界：只建设有界验收基础设施，不修复或判定产品扩容能力

## 1. 批次目标

建立 Phase 18 后续批次共用的限时 runner、场景分片、进度心跳、取消传播、原子失败证据和强归属清理。
本批只证明验收任务能在预算内得到“通过、失败或超时”三种可信结果，不运行五组件扩容矩阵，
不生成 `scaling.json`，不修改产品组件的业务处理、并发模型或资源参数。

2026-09-26 前本地 `develop/2.0.2` 的探索实现和运行中 qualification 属于已废止方案。
重新实施前必须按总方案归档旧分支，从本方案合入后的最新 `upstream/main` 创建新分支。

## 2. 实施范围

### 2.1 统一任务协议

- 新增 `scripts/verify-phase18-task.sh` 和任务目录/schema；所有任务接受 `--batch`、`--manifest`、
  `--work`、`--deadline-seconds` 和可选 `--shard`。
- runner 使用 monotonic clock；主 deadline 固定不超过 5,400 秒，第 4,800 秒停止启动新 shard，
  将剩余时间保留给附件、清理和 receipt。
- 每个 shard 在清单中声明不超过 1,200 秒的预算；清单预算求和超过批次上限时，在启动 Docker 前失败。
- runner 创建独立进程组，TERM 后最多等待 20 秒再 KILL；取消传递给负载器、采样器和 Compose 子进程。
- 每 60 秒原子更新 `progress.json`，至少记录 candidate、batch、shard、elapsed、remaining、
  last_observation 和 managed project。

### 2.2 失败与清理

- 每个断言在抛错前写入 before/during/after 观测和附件 checksum；顶层异常只记录安全 reason code
  与错误摘要 digest，不把凭据、payload 或私有路径写入公开 evidence。
- 超时写 `time_budget_exceeded`，解析失败写 `acceptance_observer_failed`，产品断言失败写
  `product_assertion_failed`，三者不得互相转换。
- cleanup 只处理当前 token/project 标记的容器、网络、卷和临时端口；禁止 Docker 全局 prune。
- cleanup 未在预算内完成时 receipt 为 failed，并列出强归属资源；命令仍须按外部 watchdog 退出。

### 2.3 Observer 与 fixture

- MySQL 观测必须保留 SQL NULL、空字符串和微秒时间；禁止通用空白 `split()`。
- Kafka group 观测按受控表头或 Admin API 识别 member、partition、offset 与 rebalance。
- RabbitMQ、Kafka、逐实例 counter 和外部副作用共享相对时间轴，每个样本带解析状态。
- fixture 覆盖旧探索已发现的空字段、错误 member、任务提前耗尽、失败附件缺失和超时取消。
- live smoke 仅各运行一次 MySQL lease、Kafka 双 member/四 partition 和 Rabbit queue 观测，
  不执行正式 replacement、容量对比或 100,000/500,000 消息负载。

### 2.4 Receipt 与验证器

`task.json` 至少绑定候选 revision/manifest/Bundle/image digest、任务清单 digest、runner/observer digest、
宿主事实、各 shard 预算与实耗、heartbeat、附件、清理和 secret scan。

聚合器拒绝：

- `duration_seconds >= 7200`、主 deadline 大于 5,400 秒或 shard 预算大于 1,200 秒；
- 缺失 heartbeat、结束时间早于最后样本、跨候选附件或手工修改 receipt；
- 超时却标记 passed、清理失败却标记 complete、Secret/私有路径命中。

## 3. 固定时间预算

| 步骤 | 最大分钟 |
| --- | ---: |
| 宿主/候选预检 | 5 |
| parser 与 schema fixture | 8 |
| deadline/取消/进程组 self-test | 12 |
| 三类最小 live observer smoke | 20 |
| 失败附件、secret scan 与清理 | 10 |
| 聚合与余量 | 5 |
| 主运行器总计 | 60 |

静态门禁最多 20 分钟，因此本批串行完成门禁最多 80 分钟。

## 4. 不在本批范围

- 多副本产品拓扑、逐实例替换、扩容比率和 SLO 修复。
- Router concurrency staircase、Kafka producer ceiling 和 Marshaller 大批量校准。
- 双 Elasticsearch、背压、lifecycle canary、合同生成或最终恢复矩阵。
- 复用旧 candidate、旧 work directory 或旧 qualification receipt。

## 5. 验收与完成条件

1. fixture 能稳定证明正常、产品失败、observer 失败和超时四种结果。
2. 人为挂起的 shard 在自己的预算内被终止，顶层命令在 100 分钟 watchdog 前退出。
3. TERM/KILL、异常和正常路径都产生原子 receipt 与强归属清理结果。
4. 三类 live observer smoke 通过，未运行正式扩容场景且未生成 `scaling.json`。
5. `task.json` 验证、Secret/路径扫描、版本和分支门禁通过。
6. `VERSION` 只在全部条件通过的完成提交更新为 `2.0.2`。

## 6. 固定验证命令

```bash
PYTHONPATH=scripts/ci python3 -m unittest \
  scripts/ci/test_phase18_task.py \
  scripts/ci/test_phase18_observers.py \
  scripts/ci/test_phase18_evidence.py
scripts/verify-phase18-task.sh --self-test
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-02 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 3600
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

完成前创建同名实施记录，记录每个命令和 shard 的实际耗时。任何超时均为本批失败，不延长预算。
