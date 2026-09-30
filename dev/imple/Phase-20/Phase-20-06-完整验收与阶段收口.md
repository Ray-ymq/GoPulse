# Phase-20-06：完整验收与阶段收口

> 目标版本：2.2.6；开发分支：develop/2.2.6；被测候选版本：2.2.5。
> 当前状态：未开始，依赖 01～05 完成且最终合同冻结。

## 1. 前置条件与范围

fetch 后记录包含 05 完成提交的 origin/main 精确 revision；从该 revision 按 05 冻结配方
构建不可变 2.2.5 候选。此时冻结实际 Bundle/自研镜像 digest 与 manifest，绑定 Compose、
runtime contract、预算、两个最终 profile、配方、Trace/保留策略、runner/verifier、宿主和
Docker，以及 05 已登记的发布目录、脱敏规则和证据清单。工作区必须干净，制品源必须能
复核到该 revision；不以分支名、浮动 tag 或未来 digest 代替具体身份。

本批只执行已冻结验收并发布事实，不修改产品代码、配置、依赖、runner、verifier 或阈值。
2.2.6 表示验收与阶段收口完成，不是另一个被正式测试的产品候选。

## 2. 固定矩阵

先完成确定性工具/schema/绑定检查，再对当前冻结候选执行真实非正式 preflight，覆盖
05 的 B07 固定案例及归属清理；只有当前候选预检完整通过才启动正式矩阵。
05 的非正式 receipt 不能改写或复用为本批候选预检/正式证据，短预检也不计入正式重复。

| 单元 | 固定执行 | 结论 |
| --- | --- | --- |
| U1 容量与端到端闭合 | 正式容量入口一次，四阶梯各三次独立重复，共十二个空项目单元；每单元按 01 排空/独立恢复/清理 | 同步、台账/业务投影、三个明确水位、尾延迟、资源与能力状态 |
| U2 持续运行与故障恢复 | 两次独立 60 分钟，每次空项目、同一配方和冻结负载；预热在计时外，第 15 分钟故障 60 秒后恢复 | 10～15 与 55～60 分钟内存窗口、全程峰值/增长斜率、磁盘/新鲜度、故障隔离和最终闭合 |
| U3 关联与生命周期检查 | 固定 C01 及 R01～R08；同候选 preflight 已通过且相关环境未变的对应案例可引用原始证据，其他必须执行 | 当前候选真实链路与身份、过期/未过期/迟到/竞态/失败追赶、查询兼容及业务索引保留 |
| U4 制品、证据与清理 | 校验每个单元及实际发布工件 | 制品与原始证据一致，无凭据泄露和归属泄漏 |

U2 的具体稳定负载、故障注入时刻、恢复及资源门禁在 05 冻结，不在本批依据 U1 结果临时调整。
固定轮数不是追求最好结果；短预检不能算成正式重复，缺失运行不能用摘要补造。
U3 的复用必须逐项记录 case_id、原 receipt/digest、候选/config/依赖身份和有效环境证据；
预检清理后已删除的索引/容器不能作为 U1/U2 当前状态，基于旧夹具状态的事实需重新执行。
无法证明相关条件未变则不复用，不能把 04/05 旧版本通过当成本候选通过。

各单元保存初始化/配方、ledger、marker、span/存储查询、资源样本、故障生效/恢复与清理
原始记录。verifier 按 case_id 输出 pass/fail/incomplete，并重算阈值/窗口/首次时刻；
summary.json 的布尔值和 03 的实验结论均不能代替当前候选事实。

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

下列 phase20 接口由 05 实现并冻结，本规划不声明其目前存在：

```bash
python3 -m unittest scripts.ci.test_phase20_closure scripts.ci.test_phase20_evidence
python3 scripts/ci/verify_runtime_contracts.py --contract <候选runtime合同> --compose <候选compose> --env <候选env> --candidate 2.2.5
scripts/verify-phase20-closure.sh --preflight --manifest <2.2.5-manifest> --work <预检新目录>
python3 scripts/verify-phase20-evidence.py --preflight <预检目录>
scripts/verify-phase20-closure.sh --manifest <同候选manifest> --work <正式新目录>
python3 scripts/verify-phase20-evidence.py --closure <正式目录>
python3 scripts/verify-phase20-evidence.py --publication <实际选定发布目录> --source <正式目录>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.6 --base-ref origin/main
git diff --check
```

--publication 必须验证实际将提交的脱敏文件、source digest 与 manifest 的对应关系，并拒绝
凭据、越界路径、漏项和 receipt 改写；不能只验证私有工作目录然后发布另一份未校验工件。
此发布接口及严格自测在 05 完成，06 不能为接受结果修改校验器。
所有产品回归来自 05 冻结矩阵，不额外扩展为全项目审计或覆盖率活动。

## 5. 完成条件与失败处理

当前候选预检、U1～U4、确定性/数据安全/权限与持续运行门禁通过，十二个容量单元和两次
60 分钟记录完整，所有证据及实际发布工件严格核对且归属清理通过，
容量结论可为 target_met 或已解释的 boundary_found。公开 03 对照与本候选结果各自适用范围，
列出尚未证明的多日运行、生产 SLO、长期 Trace、状态层 HA 与 Kubernetes。

创建实际日志、发布脱敏证据、同步完成版本并提交后才可关闭 Phase 20。
工具错误、候选漂移、证据缺失或不安全清理为 incomplete；不更新完成版本、不创建完成提交。
产品确定性或持续运行门禁失败时，保留证据并修订后续实现规划，不编辑回执或筛选重跑。
若需修改产品/工具/合同，在 update 先分配后续有效批次，所有受影响证据随新 revision 失效；
本批不得就地补实现再继续使用原候选结果。已通过且相关条件未变的门禁按规则保留，
故障原因、必要重验范围及未完成单元如实记录。
