# Phase-18-02：Outbox 瓶颈修复与短时容量验证（Phase-18-02R 补救批次）

> 优先级：**P0**
>
> 目标版本：`2.0.2`
>
> 开发分支：`develop/2.0.2`
>
> 正式运行单次上限：30 分钟
>
> 批次累计上限：30 分钟；只允许一次正式运行

> 当前执行批次：`Phase-18-02R`。原 `Phase-18-02` 已归档为
> `archive/phase-18-02-outbox-incomplete-20260926`；本分支从 `origin/main` 重建，只采用已记录的
> Outbox 产品修复和验收工具修复，不复用旧候选、manifest、receipt 或 raw evidence。
> 本批预检最多 2 次，正式入口只能运行一次，明确称为“新补救批次的唯一一次运行”。该运行失败后，
> Phase 18 直接结束为 `incomplete`，不再新增批次。

> 实际结果（2026-09-26）：宿主预检 1 次、候选预检 1 次，共 2 次；“新补救批次的唯一一次运行”
> 于 `16:27:33.064 +08:00` 启动，至 `16:37:57.914 +08:00` 生成摘要，耗时约 10 分 24.850 秒。
> 负载请求全部完成，但 `steady_pending_bound` 和 `event_state_check` 失败；本补救批次归档为
> `incomplete`，Phase 18 直接结束，不再新增批次，根 `VERSION` 保持 `2.0.1`。

## 1. 目标

直接处理 Phase-18-01 已确认的第一瓶颈：Outbox 在 150 RPS 下持续积压、实际发布能力远低于配置上限。
本批不再建设验收框架，也不扩展到其他性能优化。

## 2. 实施范围

- 最长 5 分钟复现并区分 claim 等待、lease 争用、Rabbit publish-confirm、失败退避或数据库查询问题；
  得到首个直接证据后立即停止。
- 只修改 Outbox claim/publish/release、必要索引/配置和对应低基数指标。
- 保持 owner/lease/fencing、幂等发布和“确认后标记完成”语义。
- 复用现有负载工具；允许增加一个薄的批次入口和脱敏摘要，不新增通用 runner/receipt/observer 框架。

## 3. 验收条件

1. 每个产品候选只执行一次最长 10 分钟的窗口：1 分钟预热、5 分钟 150 RPS、1 分钟 300 RPS、
   最长 3 分钟恢复。
2. 150 RPS 最后 2 分钟满足：`oldest_age <= 30s`，窗口末 pending 不高于窗口起点加
   `OUTBOX_CLAIM_BATCH`。
3. 恢复结束 pending 不高于突发开始前加 `OUTBOX_CLAIM_BATCH`；所有已接受 event-id 恰好处于
   published 或明确 pending/retry 状态，无缺失和重复持久副作用。
4. 稳态非预期错误、超时和连接失败不超过 1%；无 OOM，swap 增量不超过 256 MiB。
5. 修复有最低层单元/集成测试，runtime contract、示例配置与实际配置一致。

本补救批次预检最多执行 2 次，不启动产品场景；不带 `--preflight-only` 的正式入口只能执行一次，
明确称为“新补救批次的唯一一次运行”。不得在预检、定向诊断或工具修复后增加第二次正式运行，
也不得通过修改验收脚本刷新次数。

## 4. 固定验证

```bash
go -C backend test ./...
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
timeout --signal=TERM --kill-after=20s 5m \
  scripts/verify-phase18-02.sh --preflight-only --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
timeout --signal=TERM --kill-after=20s 20m \
  scripts/verify-phase18-02.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

`verify-phase18-02.sh` 是本批交付的薄入口，只编排上述固定窗口、输出摘要并清理当前 Compose project。
CI job 须以 30 分钟硬 timeout 覆盖本节全部命令。

## 5. 完成条件

在本补救批次唯一一次正式运行和 30 分钟累计预算内通过，创建同名实施记录与脱敏摘要，记录预检、正式运行
和诊断耗时，才更新 `VERSION=2.0.2`。原始压测/采样数据只上传 GitHub Actions Artifact，不提交到 Git。

若“新补救批次的唯一一次运行”失败，本补救批次以 `incomplete` 结束，Phase 18 直接结束，不再新增批次，
且不更新 `VERSION`。

本补救批次的唯一一次运行已失败，完成条件未满足；`VERSION` 不更新。
