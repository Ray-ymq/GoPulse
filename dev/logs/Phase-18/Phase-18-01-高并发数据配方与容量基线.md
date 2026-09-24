# Phase-18-01 高并发数据配方与容量基线实施记录

## 1. 交付结论

本记录记录 Phase-18-01 实际完成的数据配方、负载工具、容量 runner 和
三次失败基线尝试。工具链已实现并完成脚本/单元测试；三次正式尝试都执行完
三轮负载，但都在生成最终 `capacity.json` 之前被三轮重复性门禁拒绝。

因此本批当前状态是：**失败基线已保留，Phase-18-01 验收尚未完成**。
没有把失败轮次改写为通过，没有选择性复用轮次，也没有启动 Phase-18-02。
Phase-18-01 的最终 `capacity.json` 当前不存在。

## 2. 候选与执行环境

v8、有效 v9 和 v10 使用同一份运行绑定：

| 项目 | 实际值 |
| --- | --- |
| 分支 | `develop/2.0.1` |
| 版本 | `2.0.1` |
| revision | `5aca8753587a3e23730ecc5f90e45dd6ae8e5470` |
| manifest | `sha256:56c67a79445b3e3175a92b08e444b10eda746ac89a6aee76e8bad1eb0782c84e` |
| Bundle | `sha256:e0b59e814a1904d4d1a7523286c6d72465545163a94932b90326769faaab79b2` |
| 宿主 | WSL2 Linux `amd64`，8 vCPU，12.67 GiB RAM，16 GiB swap |
| Docker / Compose | Docker Server `29.7.2` / Compose `v5.5.0` |
| 可用空间 | v10 预检 `112.36 GiB`，满足 `>= 100 GiB` 合同 |

实际工作目录：

- v8：`.run/phase18-01-final-v8`
- 有效 v9：`.run/phase18-01-final-v9-test`
- v10：`.run/phase18-01-final-v10`

`.run/phase18-01-final-v9` 不是有效的三轮运行目录，没有 `binding.json`、
轮次报告、收敛结果或最终 evidence。

## 3. v8 / v9 / v10 实际结果

每次运行都完成三轮：

- 5 分钟 warmup；
- 15 分钟 steady，目标 `150 RPS`；
- 2 分钟 burst，目标 `300 RPS`；
- 最长 10 分钟恢复。

以下为每轮 steady 最差类别 P95/P99、burst 最差类别 P95/P99：

| 运行 | 轮次 | Steady P95 / P99 | Burst P95 / P99 | Steady 错误率 | Burst 错误率 |
| --- | ---: | ---: | ---: | ---: | ---: |
| v8 | 1 | 53.98 / 64.64 ms | 69.14 / 94.14 ms | 0.690% | 2.850% |
| v8 | 2 | 250.90 / 327.13 ms | 236.97 / 379.14 ms | 0.763% | 3.450% |
| v8 | 3 | 248.96 / 334.30 ms | 248.92 / 1010.56 ms | 0.785% | 3.525% |
| v9 | 1 | 52.51 / 60.42 ms | 180.94 / 258.76 ms | 0.766% | 3.008% |
| v9 | 2 | 178.74 / 242.16 ms | 210.74 / 282.10 ms | 0.772% | 3.008% |
| v9 | 3 | 53.64 / 72.21 ms | 67.74 / 888.41 ms | 0.773% | 3.017% |
| v10 | 1 | 53.14 / 63.19 ms | 60.16 / 80.81 ms | 0.766% | 3.008% |
| v10 | 2 | 185.16 / 264.15 ms | 208.50 / 267.43 ms | 0.773% | 3.008% |
| v10 | 3 | 185.44 / 244.83 ms | 221.74 / 290.24 ms | 0.766% | 3.008% |

三次运行的所有 steady 轮次都达到 `150 RPS`，偏差为 `0%`；三轮调度均没有
drops slots。负载器峰值 RSS 小于 47 MB，调度滞后最大为 v10 的 `11.18 ms`，
没有成为第一瓶颈。

按三次运行的同一轮次综合：

| 轮次 | Steady P95 平均 | Steady P99 平均 | Burst P95 平均 | Burst P99 平均 |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 53.21 ms | 62.75 ms | 103.41 ms | 144.57 ms |
| 2 | 204.94 ms | 277.81 ms | 218.74 ms | 309.55 ms |
| 3 | 162.68 ms | 217.11 ms | 179.47 ms | 729.74 ms |

第 2 轮在 v8/v9/v10 中都出现稳态 P95/P99 明显抬高；第 3 轮则在不同运行间
摇摆。该模式不支持把异常简单归因于某一次固定轮次，也没有足够的证据把任一
异常轮次排除出基线。

## 4. 门禁结果

### 4.1 三轮重复性

固定门禁要求：

- RPS 偏差 `<= 5%`；
- steady 最大 P95 偏差 `<= 10%`；
- steady 最大 P99 偏差 `<= 10%`。

实际结果：

| 运行 | RPS 偏差 | Steady P95 偏差 | Steady P99 偏差 | 结果 |
| --- | ---: | ---: | ---: | --- |
| v8 | 0% | 106.67% | 111.42% | 失败 |
| v9 | 0% | 132.92% | 145.48% | 失败 |
| v10 | 0% | 93.67% | 105.37% | 失败 |

三次 runner 都在该门禁处退出：

```text
Phase 18 capacity execution failed (ValueError)
ValueError: three-round repeatability gate failed
```

因为门禁失败发生在最终文档写入之前，每个工作目录只有 `preflight.json`，没有
`capacity.json`，也没有可声明的完整三轮验收 evidence。

### 4.2 SLO

从已落盘的原始 `load-report.json` 可以确定：三次运行的六轮 burst 非预期错误率
都超过 `<= 1%` 门禁，范围为 `2.850%` 到 `3.525%`；steady 错误率约
`0.690%` 到 `0.785%`。所以即使把重复性问题单独处理，SLO 门禁仍会失败。

## 5. 第一瓶颈与资源事实

v8/v9/v10 的资源采样结果一致指向 Backend Outbox：

| 运行 | 最大 Outbox pending | 最大 Outbox oldest age | 第一瓶颈 |
| --- | ---: | ---: | --- |
| v8 | 37,469 | 27.5 分钟 | `backend/outbox_backlog` |
| v9 | 37,578 | 27.7 分钟 | `backend/outbox_backlog` |
| v10 | 37,698 | 28.0 分钟 | `backend/outbox_backlog` |

其他事实：

- 没有 OOM，没有容器 restart；
- 三轮 Swap 增量均为 `0`；
- Kafka 最大 lag 为 v8 的 `152`、v9 的 `128`、v10 的 `111`；
- Router buffer 最大为 2；
- Marshaller/Search Indexer retry 没有成为第一瓶颈；
- v8 第 2 轮记录到一次 `364.21 ms` 调度滞后，但 v9/v10 没有同样幅度的
  调度滞后，不能把全部 P95/P99 离群解释为负载器调度问题。

## 6. 已完成修复与验证

本次已实际完成的修复：

1. `loadtest/internal/load/runner.go`
   - 将单个共享 job channel 改为每个 VU 独立 channel；
   - 按 slot index 固定映射 VU，避免 worker 调度不确定性改变请求路由；
   - 增加多 channel 关闭辅助函数。
2. `loadtest/internal/load/runner_test.go`
   - 新增 `TestSchedulePhaseAssignsSlotsDeterministicallyToVirtualUsers`。
3. `scripts/ci/phase18_capacity.py`
   - 保留子进程失败详情；
   - 允许恢复截止时间到达后记录有界未收敛，而不是丢掉整轮资源事实；
   - 优先记录恢复截止时仍存在的队列积压；
   - 对尚未产生 load report 的单个基础设施失败做一次有界重试；
   - 将最终异常写入私有 `run-error.txt`。
4. `scripts/ci/phase18_evidence.py`
   - 支持 `converged: false` 的有界失败 evidence；
   - 继续拒绝数据丢失、无法解释的未收敛和 converged round 的队列非空。
5. 对应 Python 测试已补充并通过。

实际执行并通过的验证命令：

```bash
gofmt -w loadtest/internal/load/runner.go loadtest/internal/load/runner_test.go
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
PYTHONPATH=scripts/ci python3 -m unittest \
  scripts/ci/test_phase18_capacity.py scripts/ci/test_phase18_evidence.py
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py \
  --branch develop/2.0.1 --base-ref upstream/main
git diff --check
```

结果：

- Go tests：通过；
- Phase 18 capacity self-test：通过，27 tests OK；
- Phase 18 capacity/evidence Python tests：20 tests OK；
- 版本元数据和分支治理检查：通过；
- `git diff --check`：通过。

直接以根目录运行 Python unittest 会因 `scripts/ci` 不在 `sys.path` 而出现
`ModuleNotFoundError`；使用 `PYTHONPATH=scripts/ci` 后同一组测试通过。该
失败调用已记录，不作为测试通过。

## 7. 计划偏差

- 计划只要求一次三轮基线；实际执行了 v8、v9、v10 三次三轮运行，均失败于
  重复性门禁。未启动 v11。
- 方案参考 Swap 为 8 GiB；实际为 16 GiB。`>=` 资源门禁和证据边界已按 16 GiB
  记录，未通过降低门禁补偿。
- 方案要求每 5 秒采样；v8/v9/v10 实际 `resources.json` 的平均间隔约
  `9.35–9.41 秒`，原因是 `docker stats` 和多次 `docker compose exec` 的采样
  开销。采样器干扰是候选解释之一，但现有证据不能证明它是 P95/P99 离群的原因。
- 三轮每轮之间实际包含约 10 分钟恢复和 recipe 重建，不是简单的连续压测；
  因此“增加 5 分钟暂停”本身不能解释或消除第 2/3 轮异常。
- v8/v9/v10 没有生成 `capacity.json`；最终候选的容量 evidence 不能从这些
  raw report 事后补写，因为运行中的 convergence 结果没有以独立文件持久化。

## 8. 已知限制与后续项

- Phase-18-01 尚未达到完成条件，不能把 v8/v9/v10 任一次声明为通过。
- 主要未完成门禁是三轮重复性；额外已观察到 burst 非预期错误率超过 SLO。
- Backend Outbox pending/oldest-age 在恢复截止时仍维持在约 37K / 28 分钟，
  是第一瓶颈证据。
- 后续必须由一个新的、明确的实现批次处理：要么修复容量 runner 的失败证据
  持久化和可重复性问题，要么按授权重新定义失败基线交付条件；在此之前不得
  直接启动 Phase-18-02 或把失败数据并入通过 evidence。
- 不执行 Docker global prune；不清理本任务以外的 registry、registry data 或
  其他用户资源。

## 9. 本次提交

本次交付包含：

- commit `8da9b90`：相位 18 负载调度确定性修复与容量失败诊断支持；
- 本实施记录。

未提交 `VERSION` 变更，因为 `2.0.1` 已在批次初始化时写入，且本批尚未满足
完成门禁。

## 10. 2026-09-24 定界后的配方修复

只读定界确认：`corpusFor()` 生成的 DELETE 帖子池与 read/interaction 帖子池
存在交集，删除请求执行后再访问同一帖子会稳定产生 404。修复仅位于负载配方，
没有调整错误率门禁或产品代码。

本次实际完成：

1. `loadtest/internal/recipe/generate.go`
   - 从 read/interaction 池排除所有 `delete_post_ids`，保留原有 session
     edit/delete 所有权分区和 DELETE 每 VU 配额。
2. `loadtest/internal/recipe/model_test.go`
   - 新增 `TestCorpusDeletePostsDoNotOverlapReadOrInteractionPools`，分别断言
     DELETE 池与 read 池、interaction 池完全不相交。

实际执行并通过的验证命令：

```bash
gofmt -w loadtest/internal/recipe/generate.go loadtest/internal/recipe/model_test.go
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
git diff --check
```

结果：

- loadtest Go tests：通过；
- Phase 18 capacity self-test：通过，27 tests OK；
- `git diff --check`：通过。

当前没有属于 Phase-18-01 的受管 Compose project，只有无关的
`gopulse-p13-local` 和 `gopulse-phase0203-integration` 项目。按定界排查限制，
未重建配方、未启动短测，也未运行完整三轮 runner；404 消失仍缺少新配方下的
运行时确认，500、steady 延迟和 Outbox 诊断证据也仍未采集。

v8/v9/v10 使用旧 corpus，不能作为本次配方修复后的验收或回归结果。后续获得
受管环境后，应重新生成 corpus，并只执行一次最长 5 分钟的短测，同时采集
404、500 request-id、逐秒延迟及同窗 Backend/MySQL/Outbox 证据。

## 11. 2026-09-24 原始样本保全后的正式三轮

先修复证据丢失窗口，再执行一次新的正式三轮。实现提交为
`0a71f57`：资源样本逐条追加并 `fsync` 到私有 `resources.raw.jsonl`，每轮
负载前写入 `load-binding.json`，重复性失败写独立
`capacity-failure.json`，并增加汇总失败和重复性失败注入测试。

修复后的验证命令与结果：

```bash
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
python3 -m py_compile scripts/ci/phase18_sampler.py \
  scripts/ci/phase18_capacity.py scripts/ci/phase18_evidence.py
git diff --check
```

结果：Go tests 通过；capacity self-test 共 `34 tests OK`；编译和 diff check
通过。随后只执行一次正式三轮：

```bash
scripts/verify-phase18-capacity.sh \
  --manifest dist/phase18-01-candidate-v6/release-manifest.json \
  --rounds 3 --work .run/phase18-01-full-v1
```

执行结果：三轮各完成 `193500` 个请求，随后 repeatability 门禁失败并停止，
没有重跑或换种子。RPS 偏差 `0.0%`；P95 偏差 `93.845%`；P99 偏差
`100.036%`。因此没有生成 `capacity.json`，只有
`.run/phase18-01-full-v1/evidence/capacity-failure.json`。

本轮实际形成的持久证据：

- 三轮 `resources.raw.jsonl` 分别为 `201/200/200` 条，SHA-256 与
  `samples-receipt.json` 一致。
- 每轮 `load-binding.json` 绑定 corpus、负载源码 commit、负载/recipe
  二进制 SHA-256 与固定负载参数。
- 三轮 `load-diagnostic.json`、`load-report.json`、`resources.json` 和
  convergence 均随失败汇总保留。
- `capacity-failure.json` 中 21 个归档工件引用的 SHA-256 已重新核对一致。

第三轮有 `18` 个 500（steady `8`、burst `10`），burst 最大调度滞后为
`360.395629 ms`。三轮恢复截止时均仍有约 `38K` Outbox pending；前两轮第一
瓶颈为 Backend Outbox backlog，第三轮为 loadtest 调度。以上仅为失败证据，
不构成 Phase-18-01 通过。

脱敏后的全部非凭据证据归档在
`dev/logs/Phase-18/Phase-18-01-full-v1-failure/`。`*.env`、
`credentials.json`、可重建二进制和空锁文件未发布；归档成员及哈希见
`evidence-manifest.json`。受管 Compose project 已清理，候选 registry 已
停止，未执行全局 prune，未启动 Phase-18-02。
