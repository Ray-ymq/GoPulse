# Phase-19-04：容量认证与阶段收口

> 目标版本：`2.1.4`
>
> 开发分支：`develop/2.1.4`
>
> 被测候选版本：`2.1.3`

## 1. 前置条件

- Phase-19-03 已完成、合入 primary remote `main`，根 `VERSION=2.1.3`。
- 从该最新 `main` 创建本批分支后，构建 revision 与 manifest 均绑定该主线提交的不可变 `2.1.3` 候选。
- 修订后的 profile、Compose、runtime contract、recipe descriptor、runner 和 verifier digest 已完成 preflight；
  本批不得再修改它们。
- 顺延前的未完成预检没有调用正式入口，不占用本批唯一正式调用。

## 2. 目标

冻结同一 `2.1.3` 候选、profile、数据配方、资源和验收程序，只调用一次正式入口，由 runner 完成
三次独立重复，发布指定环境的容量曲线、第一瓶颈、恢复事实和 `target_met` 或 `boundary_found` 结论。

`2.1.4` 表示本认证与阶段收口批次完成，不表示正式测试了另一个候选；所有文档和 evidence 必须同时列出
被测 candidate version/revision/digest 与收口版本。

## 3. 实施范围

- 正式执行前完成只读 deterministic preflight，并记录 candidate/profile/Compose/runtime-contract/recipe/runner digest。
- 正式 runner 按 profile 完成三次独立重复；单次产品边界不触发筛选重跑。
- 保存逐阶梯负载、资源、队列、异步闭合、观测新鲜度、恢复和清理原始证据。
- 严格 verifier 通过后生成脱敏实施记录、evidence manifest 和当前能力状态。
- 本批不修改产品、负载模型、阈值、runner、采样器、profile 或 evidence 代码。

## 4. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `dev/logs/Phase-19/Phase-19-04-容量认证与阶段收口.md` | 记录绑定、三次原值、聚合、能力状态、第一瓶颈、偏差和限制 |
| `dev/logs/Phase-19/Phase-19-04-evidence/summary.json` | 仅发布脱敏、严格验证后的 summary 与哈希引用，不包含凭据/环境文件 |
| `dev/logs/Phase-19/Phase-19-04-evidence/evidence-manifest.json` | 列出发布工件、来源和 SHA-256，拒绝未验证替换 |
| `README.md` | 只声明 `2.1.3` 冻结候选在指定环境中的实际容量或边界，不外推生产能力 |
| `dev/status/capability-status.md` | 将 Phase 19 结论写入已验证或已发现边界，保留未验证项 |
| `dev/phases/Plan.md`、`dev/phases/README.md`、`dev/phases/Phase-19-容量认证与性能可观测.md` | 记录阶段真实完成状态，不改写 Phase 18 历史 |
| `dev/imple/Phase-19/Phase-19-总实施方案.md` | 仅填写最终候选、执行状态和能力状态，不改变既有验收规则 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 执行完整并收口时六处版本一致为 `2.1.4` |

除 evidence 归档和上述状态/版本文件外，本批不得修改可执行代码、配置或验收工具。发现工具或产品缺陷时，
执行状态为 `incomplete`，停止并另立新方案；不得在本批修复后筛选重跑。

## 5. 固定正式验收

```text
scripts/verify-phase19-capacity.sh --manifest <2.1.3-manifest> --work <new-owned-directory>
python3 scripts/verify-phase19-evidence.py --capacity <same-directory>
```

正式入口只调用一次。runner 从冻结 profile 读取三次重复和所有阶梯，不接受命令行次数、RPS、窗口、阈值、
seed、base URL、corpus 或 credentials 覆盖。结果同时包含：

- `execution_status=complete|incomplete`；
- 执行完整时的 `capability_status=target_met|boundary_found`；
- 三次逐阶梯原值和 median/min/max/CV；
- 第一瓶颈及其同窗产品/资源证据；
- 每次项目清理和最终无归属泄漏证明。

## 6. 必需回归范围

- Phase-19-03 的 self-test、calibration 和真实 preflight evidence 在候选、profile、Compose、runtime contract、
  recipe、runner/verifier 及环境不变时继续有效，不无理由重复。
- 本批固定回归仅为一次正式三重复、同目录 strict verifier、版本/分支治理、文档差异和敏感信息扫描。
- 任一候选、验收代码、profile、Compose、runtime contract 或 evidence schema 变化都会使正式证据失效；
  本批不得修复后重跑，必须以 `incomplete` 停止并另立方案。

## 7. 完成条件

1. deterministic preflight 通过，候选和全部 digest 在三次重复中一致。
2. `execution_status=complete`，三次重复和安全清理均有完整证据。
3. strict verifier 通过，没有第四次运行、证据覆盖、平均算法错误、合成恢复回执或凭据泄漏。
4. 能力状态为 `target_met` 或 `boundary_found`，文档明确被测候选为 `2.1.3` 且不扩大声明。
5. 创建实施记录和脱敏 evidence manifest，同步完成版本 `2.1.4` 并提交。

若执行状态为 `incomplete`，本批不得更新 `VERSION`、不得创建完成提交、不得关闭 Phase 19。
