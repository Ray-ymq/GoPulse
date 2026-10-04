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

开工前核对 05 的 4.1 节完整工具交付回执，包括同一正式任务图的 dry-run、自测、真实
短预检和 publication/source 校验。正式路径仍只支持 preflight、U2 尚无持续运行实现、
或 evidence 入口缺少 --closure/--source 时，本批不得解锁；缺口在未完成的 05 收口。

## 2. 固定矩阵

先完成确定性工具/schema/绑定检查，再对当前冻结候选执行真实非正式 preflight，覆盖
05 的 B07 固定案例及归属清理；只有当前候选预检完整通过才启动正式矩阵。
05 的非正式 receipt 不能改写或复用为本批候选预检/正式证据，短预检也不计入正式重复。
preflight 使用 05 冻结的短 profile：U1 为 50/200 RPS 两个短单元，U2 为真实 300 秒
负载及第 60 秒开始、持续 60 秒的 Collector 故障，U3/U4 取得并校验真实原始事实。
当前候选真实 C01/生命周期回执和业务/观测恢复必须可重算；单元测试退出码或容器 Running
不能代替产品通过。05 的 180 分钟执行保护针对 05 固定门禁，不替代本批正式长窗口。

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

逐单元保存不可变回执及阶段耗时。对相同候选因中断尚未开始的单元，可用已冻结的
`--resume <未完成目录>` 恢复；必须重核 manifest、工具/config/profile/依赖和环境身份，
旧失败单元不原地覆盖，不增加第四次容量重复或第三次持续运行以筛选好结果。
候选相关变更按末节转回有效实施批次，不复用原候选受影响证据。

### 2.1 专项验收成本与开始条件

本批是专项长时验收。05 的本轮结果先按原合同收口，不为本节规划重新验收；06 原已
要求对最终候选进行的预检/矩阵继续遵循原身份规则，不能把不同候选证据相互换绑。

开工前从 05 本轮实际阶段耗时计算并登记：构建/预检、十二个 U1 单元、两个 U2 单元、
U3 的已证明可复用项/剩余项、U4 校验与清理。最低时间至少包含 U2 的 120 分钟测量，
加上 U1 的 15 分钟预热/测量以及各单元实际初始化、恢复和清理；不以“每条命令都有超时”
代替总成本。登记预计总时间、有限总上限、每单元上限和剩余预算检查后才启动。

长实验执行期间不开发或修补工具。第一次工具故障即保存失败单元，转为最小诊断；
同原因最多两次、每次最多 10 分钟。累计耗时包含失败尝试，换候选或工作目录不清零。
已完成的当前候选单元只在身份与相关条件有效且恢复实现实际支持时接续；不再自动
从 U1 第一个单元重跑。到达登记上限先有界收尾、报告剩余项，不延长等待到“全部通过”。

每次命令启动前核对预计耗时与安全收尾是否能容纳，执行 50%/80% 进度停点；
具体登记和过渡规则见 [实施耗时预算与验收接续](../../rules/implementation-execution-budget.md)。

## 3. 允许变更文件

- dev/logs/Phase-20/Phase-20-06-完整验收与阶段收口.md。
- dev/logs/Phase-20/Phase-20-06-evidence/summary.json 和 evidence-manifest.json（同 evidence 目录），
  仅发布严格校验选定工件及原始来源引用，不能修改原始 receipt。
- README.md、dev/status/capability-status.md、dev/phases/Plan.md、dev/phases/README.md、
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
scripts/verify-phase20-closure.sh --dry-run --manifest <2.2.5-manifest> --work <任务图新目录>
scripts/verify-phase20-closure.sh --preflight --manifest <2.2.5-manifest> --work <预检新目录>
python3 scripts/verify-phase20-evidence.py --preflight <预检目录>
scripts/verify-phase20-closure.sh --manifest <同候选manifest> --work <正式新目录> --preflight-evidence <已校验预检目录>
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
