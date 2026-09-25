# Phase-18-09：限时集成恢复与 Milestone 5 收口实施方案

> 目标版本：`2.0.9`
> 开发分支：`develop/2.0.9`
> 候选边界：同一 Linux `amd64` manifest、Bundle、runtime contract、catalog 和 image/plugin digest

## 1. 批次目标

冻结 `2.0.9` 候选，通过变更影响清单选择必须在当前候选重跑的高风险短 shard，完成直接前序升级、
format v1/v2 恢复、业务与权限抽样、跨数据面 canary 和 Milestone 5 证据收口。

本批禁止调用“重新执行 Phase 18 所有历史场景”的全量矩阵。若当前候选必须重跑的有效范围不能在
90 分钟主运行器内完成，必须在开工前新增批次，不得提高本批 timeout。

## 2. 候选与证据血缘

- 冻结 revision、manifest、Bundle、runtime contract、四 catalog、全部产品/插件/第三方 image digest。
- 生成 machine-readable impact map，列出 Phase-18-02 至 18-08 每个 receipt 的候选、相关路径/digest、
  当前变化和 `reuse|rerun|invalid` 判定。
- 复用 receipt 只证明历史批次边界，不能冒充 `2.0.9` 当前候选执行结果。
- 任何影响产品行为、验收脚本、schema、mapping、Compose 或发布元数据的变化，使对应当前候选 shard
  进入 rerun；影响范围不明确时按 rerun。
- 正式主运行器每候选一次。失败后只进行定向修复；新候选重新计算 impact map。

## 3. 固定当前候选 shard 与预算

| Shard | 最大分钟 |
| --- | ---: |
| preflight、候选冻结与 impact map | 5 |
| `2.0.8 → 2.0.9` 直接前序升级 | 12 |
| format v1 输入恢复到双 ES | 12 |
| `2.0.9` format v2 backup/restore | 12 |
| 普通用户与超级管理员/权限抽样 | 12 |
| 150/300 RPS 与多副本代表性抽样 | 12 |
| 双 ES 故障、背压和独立 canary 组合抽样 | 12 |
| evidence 聚合、报告、secret scan 与清理 | 8 |
| 主运行器总计 | 85 |

每个 shard 不超过 20 分钟，静态/发布门禁最多 20 分钟，批次串行总预算不超过 105 分钟。
impact map 若要求额外 shard，不得挤压或并入上述 shard；必须在开工前新增批次。

## 4. 产品抽样范围

- 普通用户：注册/登录、发现/关注、发帖/编辑/删除、评论/点赞/收藏、feed、通知和搜索。
- 超级管理员：双 Frontend 登录、用户/角色、六插件、Metrics/Logs/Events、告警、dashboard 和审计。
- 固定 `401/403/success` 代表矩阵。
- 多副本抽样至少覆盖 Backend edge、一个 Rabbit consumer replacement 和一个 Kafka rebalance。
- 故障抽样覆盖一个 ES 停机不影响另一数据面、一个显式过载拒绝以及观测三依赖不可用时本地 canary receipt。
- cleanup 只移除当前 project/token/digest 强归属资源。

## 5. 验收与完成条件

1. impact map 完整、可机器验证；所有 `rerun` shard 均来自同一 `2.0.9` 候选。
2. 直接前序升级、format v1/v2 restore 和恢复后新写入通过。
3. 业务、权限、多副本、背压、双 ES 和 canary 的代表性跨边界路径通过。
4. Phase-18-02 至 18-09 receipt/实施记录齐全，复用证据没有冒充当前候选。
5. 主运行器小于 90 分钟，全部门禁小于 120 分钟；无阻断问题。
6. 容量报告明确参考宿主、短窗口、第一瓶颈、状态单点和适用边界。
7. 根与受管版本为 `2.0.9`。

## 6. 固定验证命令

```bash
python3 scripts/ci/generate_contracts.py --check
scripts/verify-release-artifacts.sh \
  --manifest dist/release-manifest.json \
  --platform linux/amd64 \
  --runtime
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-09 \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.9 --base-ref upstream/main
git diff --check
```

创建同名实施记录，记录 impact map、每个 shard 实耗、receipt 复用依据和最终容量边界。
全部条件达到后结束 Phase 18 与 Milestone 5。
