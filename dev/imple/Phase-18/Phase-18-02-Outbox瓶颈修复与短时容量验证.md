# Phase-18-02：Outbox 瓶颈修复与短时容量验证

> 优先级：**P0**
>
> 目标版本：`2.0.2`
>
> 开发分支：`develop/2.0.2`
>
> 完成门禁总时限：30 分钟

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

1. 对新候选只执行一次最长 10 分钟的窗口：1 分钟预热、5 分钟 150 RPS、1 分钟 300 RPS、
   最长 3 分钟恢复。
2. 150 RPS 最后 2 分钟内 Outbox 不持续单调增长，`oldest_age <= 30s`。
3. 突发产生的积压在恢复窗口内回到稳态高水位以内；已接受事件无静默丢失或重复持久副作用。
4. 稳态非预期错误、超时和连接失败不超过 1%；无 OOM，swap 增量不超过 256 MiB。
5. 修复有最低层单元/集成测试，runtime contract、示例配置与实际配置一致。

若 5 分钟定界仍不能指出一个根因，或正式窗口出现新的第一瓶颈，本批停止并记录下一次最小动作，
不得延长窗口或继续跑三轮。

## 4. 固定验证

```bash
go -C backend test ./...
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
timeout --signal=TERM --kill-after=20s 20m \
  scripts/verify-phase18-02.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
scripts/verify-runtime-contracts.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

`verify-phase18-02.sh` 是本批交付的薄入口，只编排上述固定窗口、输出摘要并清理当前 Compose project。

## 5. 完成条件

全部验收通过，创建同名实施记录与脱敏摘要，更新 `VERSION=2.0.2`。原始压测/采样数据只上传
GitHub Actions Artifact，不提交到 Git。
