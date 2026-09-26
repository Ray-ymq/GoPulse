# Phase-18-06：最终容量与故障恢复验收

> 优先级：P0/P1 收口
>
> 目标版本：`2.0.6`
>
> 开发分支：`develop/2.0.6`
>
> 正式运行与批次累计上限：60 分钟
>
> 正式运行次数：一次；失败后不得在本批重跑

## 1. 目标

在同一冻结候选上，用一次有界运行确认 Phase 18 的跨边界结果。该批只验收，不重建通用 runner，
也不重放 Phase-18-02～18-05 的完整场景。

## 2. 固定场景与预算

| 场景 | 最长时间 |
| --- | ---: |
| 候选/环境预检与受影响静态检查 | 5 分钟 |
| 150 RPS 稳态 | 30 分钟 |
| 300 RPS 突发与恢复 | 5 分钟 |
| 观测 ES 故障与恢复 | 10 分钟 |
| 摘要、Secret 扫描、强归属清理与最终静态检查 | 10 分钟 |
| 总计 | 60 分钟 |

本批不执行实例替换、六依赖故障、迁移或历史所有权矩阵。预检必须在产品场景启动前通过；最多执行
两次预检，第二次仍失败则本批直接以 incomplete 结束。

## 3. 验收条件

1. 30 分钟稳态达到 150 RPS；读 P95/P99 不高于 500 ms/1.5 s，写不高于 800 ms/2 s，
   非预期错误、超时和连接失败总和不超过 1%。
2. 300 RPS 突发只有预期成功或明确过载拒绝，明确拒绝不超过 5%；无 OOM、挂死、伪成功或无界增长。
3. 稳态与突发期间接受的 Outbox/Rabbit/Kafka event-id 与唯一持久结果闭合；恢复结束 pending/lag
   不高于突发开始前加各自单批上限，搜索/通知和观测新数据在 60 秒内可查。
4. 观测 ES 停机 2 分钟期间，固定 100 个业务搜索请求全部成功；独立健康在 120 秒内报告固定
   dependency-id。恢复后，本场景已接受的 Logs/Events 在 5 分钟内全部可查。
5. 全部结果来自同一 revision、manifest、镜像 digest、配置和参考宿主。

出现首个阻断问题时立即停止。本批没有第二次正式运行和动态诊断额度；不得修复后重跑、跑完其余场景
制造“部分通过”，或更换候选后拼接证据。继续工作必须先在总方案中新建修复批次和版本。

## 4. 固定验证

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
python3 scripts/ci/generate_contracts.py --check
timeout --signal=TERM --kill-after=20s 5m \
  scripts/verify-phase18-06.sh --preflight-only --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
timeout --signal=TERM --kill-after=20s 50m \
  scripts/verify-phase18-06.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.6 --base-ref upstream/main
git diff --check
```

正式动态入口全批只运行一次，只输出脱敏摘要并清理当前 Compose project。CI job 必须另设 60 分钟
硬 timeout，覆盖本节全部命令，而不是只约束动态入口。

## 5. 完成条件

唯一一次正式验收在 60 分钟内通过，创建同名实施记录和最终容量摘要，更新 `VERSION=2.0.6`，
Phase 18 与 Milestone 5 结束。原始日志、采样流和压测附件只上传 GitHub Actions Artifact。
