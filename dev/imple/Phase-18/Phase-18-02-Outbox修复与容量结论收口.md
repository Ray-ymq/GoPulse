# Phase-18-02：Outbox 修复采纳与容量结论收口

> 优先级：**P0**
>
> 目标版本：`2.0.2`
>
> 开发分支：`develop/2.0.2`
>
> 历史容量结论已冻结，本批不重测。

## 1. 目标

从已归档候选中只采纳已定位的 Outbox 调度修复和最小测试，并把已有容量运行收口为
`boundary_found`。本批不以“跑到通过”为目标，不再执行容量正式运行。

## 2. 实施范围

- 从最新 `upstream/main` 创建分支，逐项采纳 Outbox 产品改动，不整体 cherry-pick 归档分支。
- claim 返回满批且无错误时立即继续下一批；空批、部分批或错误时按现有周期等待。
- 保持 owner/lease/fencing、幂等发布和“确认后标记完成”语义。
- 为上述路径增加最低层测试；不新增 runner、receipt、observer 或容量验收脚本。
- 创建同名实施记录，引用归档证据事实，但不将其改写为通过证据。

## 3. 硬门禁

1. 满批路径不再额外等待 poll interval。
2. 空批、部分批、错误和取消路径不产生忙循环。
3. owner/lease/fencing 和 ack 后完成的现有测试全部通过。
4. Backend 相关回归、运行时合同和版本/分支检查通过。

这些门禁只判断待交付修复是否可合并，不重新判断 150/300 RPS 是否达标。

## 4. 容量结果记录

实施记录必须如实保留：

- 历史运行的请求完成情况、Outbox 峰值与恢复结果。
- 稳态 pending `15 -> 31` 超过冻结上限 `25`。
- event-state 总量未被观测，已接受事件闭合未被证明。
- 结果为 `boundary_found`，不宣称已证明 150 RPS 稳定容量。

不允许为改变这个结论重跑、调整阈值或修改历史摘要。

## 5. 固定验证

```bash
go -C backend test ./internal/outbox/...
go -C backend test ./...
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

本批没有容量动态入口，不执行 150/300 RPS 场景。

## 6. 完成条件与版本

- 硬门禁通过：创建同名实施记录，更新 `VERSION=2.0.2`，批次和 Phase 18 完成。
  容量结论仍是 `boundary_found`，并作为下一阶段输入。
- 硬门禁未通过：不合并候选，不更新 `VERSION`，记录候选回归后以 `incomplete` 关闭
  Phase 18。不在本阶段新增修复批次。
