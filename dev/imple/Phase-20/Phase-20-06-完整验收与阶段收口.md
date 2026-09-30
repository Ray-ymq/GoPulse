# Phase-20-06：完整验收与阶段收口

> 目标版本：2.2.6；开发分支：develop/2.2.6；被测候选版本：2.2.5。
> 当前状态：未开始，依赖 01～05 完成且最终合同冻结。

## 1. 前置条件与范围

从包含 05 完成提交的最新 origin/main 构建不可变 2.2.5 候选，绑定 Bundle/镜像/manifest、
revision、Compose、runtime contract、预算、两个最终 profile、配方、Trace/保留策略、
runner/verifier、宿主和 Docker。冻结发布目录、脱敏规则和证据清单。

本批只执行已冻结验收并发布事实，不修改产品代码、配置、依赖、runner、verifier 或阈值。
2.2.6 表示验收与阶段收口完成，不是另一个被正式测试的产品候选。

## 2. 固定矩阵

执行前 deterministic preflight 对当前候选检查绑定、工具、环境、归属和可执行固定矩阵；
若与 05 的非正式候选不同，不能直接复用其 revision 证据。

| 单元 | 固定执行 | 结论 |
| --- | --- | --- |
| U1 容量与端到端闭合 | 正式容量入口一次，四阶梯各三次独立重复；阶梯恢复隔离 | 同步、业务投影、观测水位、尾延迟、资源与能力状态 |
| U2 持续运行与故障恢复 | 两次独立 60 分钟，每次空项目、同一配方和冻结负载 | 前/中/后资源、磁盘增长、新鲜度、观测故障隔离、恢复和终态 |
| U3 生命周期确定性检查 | 按 05 固定集合执行；复用同候选且相关环境未变的已有事实 | 过期/未过期/迟到/失败恢复及业务索引保留 |
| U4 制品、证据与清理 | 校验每个单元及实际发布工件 | 制品与原始证据一致，无凭据泄露和归属泄漏 |

U2 的具体稳定负载、故障注入时刻、恢复及资源门禁在 05 冻结，不在本批依据 U1 结果临时调整。
固定轮数不是追求最好结果；短预检不能算成正式重复，缺失运行不能用摘要补造。

## 3. 允许变更文件

- dev/logs/Phase-20/Phase-20-06-完整验收与阶段收口.md。
- dev/logs/Phase-20/Phase-20-06-evidence/summary.json 和 evidence-manifest.json（同 evidence 目录），
  仅发布严格校验选定工件及原始来源引用，不能修改原始 receipt。
- README.md、docs/capability-status.md、dev/phases/Plan.md、dev/phases/README.md、
  dev/phases/Phase-20-端到端性能闭环与可观测治理.md、dev/phases/GoPulse-高并发与可观测后续路线图.md。
- dev/imple/Phase-20/Phase-20-总实施方案.md：只记录实际最终结果，不追溯修改冻结规则。
- VERSION、.env.example、frontend/package.json、frontend/package-lock.json、
  admin-frontend/package.json、admin-frontend/package-lock.json：完成版本同步为 2.2.6；
  候选仍保持绑定的 2.2.5，不用收口元数据重建后冒充已测制品。

## 4. 验证命令与回归

下列接口由 05 实现并冻结，本规划不声明其目前存在：

- scripts/verify-phase20-closure.sh --preflight --manifest <2.2.5-manifest> --work <新目录>。
- scripts/verify-phase20-closure.sh --manifest <同候选manifest> --work <正式新目录>。
- python3 scripts/verify-phase20-evidence.py --closure <正式目录>。

另执行 python3 scripts/ci/validate_versions.py；
python3 scripts/ci/validate_branch.py --branch develop/2.2.6 --base-ref origin/main；
git diff --check 及实际脱敏工件/manifest 来源校验。
所有产品回归来自 05 冻结矩阵，不额外扩展为全项目审计或覆盖率活动。

## 5. 完成条件与失败处理

全部确定性、数据安全、权限与持续运行门禁通过，运行完整、所有证据严格核对且归属清理通过，
容量结论可为 target_met 或已解释的 boundary_found。公开 03 对照与本候选结果各自适用范围，
列出尚未证明的多日运行、生产 SLO、长期 Trace、状态层 HA 与 Kubernetes。

创建实际日志、发布脱敏证据、同步完成版本并提交后才可关闭 Phase 20。
工具错误、候选漂移、证据缺失或不安全清理为 incomplete；不更新完成版本、不创建完成提交。
产品确定性或持续运行门禁失败时，保留证据并修订后续实现规划，不编辑回执或筛选重跑。
