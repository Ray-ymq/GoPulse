# Phase-19-03：容量认证与阶段收口

> 目标版本：`2.1.3`
>
> 开发分支：`develop/2.1.3`

## 1. 目标

冻结同一候选、profile、数据配方、资源和验收程序，只调用一次正式入口，由 runner 完成三次独立重复，
发布指定环境的容量曲线、第一瓶颈、恢复事实和 `target_met` 或 `boundary_found` 结论。

## 2. 实施范围

- 正式执行前完成 deterministic preflight，并记录 candidate/profile/Compose/runtime-contract/recipe digest。
- 正式 runner 按 profile 完成三次独立重复；单次产品边界不触发筛选重跑。
- 保存逐阶梯负载、资源、队列、异步闭合、观测新鲜度、恢复和清理原始证据。
- 严格 verifier 通过后生成脱敏实施记录和当前能力状态。
- 本批不修改产品、负载模型、阈值、runner、采样器或 evidence 代码。

## 3. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `dev/logs/Phase-19/Phase-19-03-容量认证与阶段收口.md` | 记录绑定、三次原值、聚合、能力状态、第一瓶颈、偏差和限制 |
| `dev/logs/Phase-19/Phase-19-03-evidence/summary.json` | 仅发布脱敏、严格验证后的 summary 与哈希引用，不包含凭据/环境文件 |
| `dev/logs/Phase-19/Phase-19-03-evidence/evidence-manifest.json` | 列出发布工件、来源和 SHA-256，拒绝未验证替换 |
| `README.md` | 只声明实际支持的指定环境容量或边界，不外推生产能力 |
| `docs/capability-status.md` | 将 Phase 19 结论写入已验证或已发现边界，保留未验证项 |
| `dev/phases/Plan.md`、`dev/phases/README.md`、`dev/phases/Phase-19-容量认证与性能可观测.md` | 记录阶段真实完成状态，不改写 Phase 18 历史 |
| `dev/imple/Phase-19/Phase-19-总实施方案.md` | 仅填写最终候选、执行状态和能力状态，不改变既有验收规则 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 执行完整并收口时六处版本一致为 `2.1.3` |

除 evidence 归档和上述状态/版本文件外，本批不得修改可执行代码、配置或验收工具。发现工具或产品缺陷时，
执行状态为 `incomplete`，停止并另行修订未开始批次或新方案；不得在本批修复后筛选重跑。

## 4. 固定正式验收

```text
scripts/verify-phase19-capacity.sh --manifest <2.1.3-manifest> --work <new-owned-directory>
python3 scripts/verify-phase19-evidence.py --capacity <same-directory>
```

正式入口只调用一次。runner 从冻结 profile 读取三次重复和所有阶梯，不接受命令行次数、RPS、窗口、阈值
或 seed 覆盖。结果同时包含：

- `execution_status=complete|incomplete`；
- 执行完整时的 `capability_status=target_met|boundary_found`；
- 三次逐阶梯原值和 median/min/max/CV；
- 第一瓶颈及其同窗产品/资源证据；
- 每次项目清理和最终无归属泄漏证明。

## 5. 完成条件

1. deterministic preflight 通过，候选和全部 digest 在三次重复中一致。
2. `execution_status=complete`，三次重复和安全清理均有完整证据。
3. strict verifier 通过，没有第四次运行、证据覆盖、平均算法错误或凭据泄漏。
4. 能力状态为 `target_met` 或 `boundary_found`，文档不扩大声明。
5. 创建实施记录和脱敏 evidence manifest，同步版本 `2.1.3` 并提交。

若执行状态为 `incomplete`，本批不得更新 `VERSION`、不得创建完成提交、不得关闭 Phase 19。
