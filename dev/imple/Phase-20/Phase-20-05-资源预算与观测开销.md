# Phase-20-05：资源预算与观测开销

> 目标版本：2.2.5；开发分支：develop/2.2.5；当前状态：未开始，依赖 04 完成。

## 1. 目标与范围

把既有进程内部并发预算与宿主/容器/磁盘预算结合，量化观测对业务的成本并冻结最终验收合同。
沿用 Linux amd64 Compose，状态层与 Monitor 保持现有单节点边界。

- 在独立机器合同中记录宿主规格、每服务 CPU/RSS/连接/队列预算、磁盘安全水位、
  Trace/日志采样、采集频率、预期数据增长及超限行为；固定目录与 runtime contract 一致。
- 将应由运行环境限制的 CPU/内存预算落实到受支持 Compose 配置，验证实际 inspect 值；
  不用一份未生效 YAML 宣称资源隔离。磁盘逻辑水位与实际配额区分。
- 不在本批无证据调整产品吞吐参数；资源布局改变后重新记录最终候选基线，不把加机器/加内存
  描述为 03 的算法收益。
- 观测正常/观测出口故障时比较业务终态和延迟；队列满、丢弃、重试与关停有界且可解释。
- 在相同业务负载下固定测量观测启用/关闭、Trace 采样差异和采样器本身的开销，记录开销口径、
  宿主共用 CPU/I/O 以及预算不足时的降级。诊断对照不作为正式容量认证。
- 依据每分钟实际增长估算磁盘保留成本，注明压缩、索引放大和原生回收的误差。
- 完成 06 所需编排、verifier、持续运行 profile、故障步骤和脱敏发布工具；先完成自测与预检。

预算数值、开销阈值及最终 profile 在创建本批分支前依据 01～04 证据，在 update 细化并合入
main。完成本批时构建配方及全部执行合同冻结；实际最终镜像/Bundle/manifest 在 06 从包含
本批完成提交的 main 构建后冻结。06 不再修改任何可执行或验收配置。

## 2. 预算记录与开工前冻结项

每条预算必须包含 budget_id、服务/进程/队列或磁盘目标、测量来源、单位、窗口起点/长度、
聚合统计、阈值、触发所需连续样本/持续时长、超限动作、恢复条件/时限及原始证据路径。
schema 拒绝空值、无限值、TBD、未登记目标与单位不符。所有限值绑定前序实测证据；
只记录 Compose limit 或“超限告警”而没有实际行为和结果，不能通过。

| 类别 | 必填统计及规则 |
| --- | --- |
| CPU | cores 或核秒/秒，明确区间平均/峰值和采样间隔；区分 quota、实际用量及 throttling；SUT、压测器、采样器、Collector 分别核算，另记宿主合计 |
| 内存 | RSS bytes 与容器 memory.current/limit 分列；峰值、OOM/非预期重启、正常结束和稳定窗口增长上限；不把 RSS 等同容器用量 |
| 连接/队列 | 对象名称、单位 count、配置容量、实际峰值、满载触发、拒绝/丢弃/重试/背压动作及恢复时限；业务队列与尽力而为 Trace 队列分别说明 |
| 磁盘 | 明确数据目录/归属卷，used/free bytes、安全水位、每分钟增长、预计保留成本和触发动作；逻辑水位与 OS/容器强制配额分列 |
| 观测与观察者开销 | 启用/关闭的具体组件集合、Trace 采样率、采样器配置、比较负载/时长/三重复、CPU/RSS/业务尾延迟差阈值及测量误差 |
| 故障/饱和 | case_id、精确目标、注入/恢复动作与时刻、最长持续时间、安全停止条件、期望动作/计数及最终恢复判据 |

稳定窗口内存增长同时检查峰值和窗口趋势。最终每次 60 分钟运行比较第 10～15 分钟与
第 55～60 分钟 RSS/容器用量的 median，并计算固定间隔样本的增长斜率；分别冻结绝对增长
和 bytes/minute 上限，不强制 GC、不事后挑低点。磁盘以真实卷读数计算增量与斜率，
保留索引/压缩/原生回收影响；预测成本不能代替安全水位门禁。

最终持续运行 profile 在开工前必须填入：稳定 RPS（来自前序已通过的稳定阶梯，06 不再
根据 U1 临时选择）、同一配方/业务比例、预热时长、采样间隔/缺样容忍上限、各资源预算引用，
以及观测存储出口的一次有界故障。两次运行都先完成初始化/预热，再计时 60 分钟；
第 15 分钟注入已登记的观测存储故障，持续 60 秒后恢复，期间业务继续。
实际故障目标/实现方式和预期计数在开工前确定；宿主/业务状态依赖不属于该注入范围。
恢复后仍按独立 120 秒业务/观测水位门禁验证；第 60 分钟停止调度、排空和最终恢复另记。
故障未实际生效、持续时长不符或缺少回执均为 incomplete。

持续负载中至少在预热后和故障恢复后各执行一次检查点：使用不会被背景编辑/删除的独立
发帖探针，按接受事实/事件/真实搜索证明业务新鲜度，三通道仍按 01 产生真实标记并独立
计时；不用全局 lag=0 或变化中的全表快照证明闭合。探针与背景压测统计分开，开销计入预算。
这些检查点只证明被探测链路的恢复；全量已接受背景请求的台账/最终投影在停止并排空后
按 01 核对，不能把一个探针通过宣称为所有后台请求均已完成。

开工冻结表还必须登记：机器规格/可用空间、每个预算实际数值及依据、所有 B 案例的准确
动作与证据位置、最终 U1～U4 清单、依赖/第三方镜像 digest、构建配方、发布脱敏清单。
任一项未填不得创建 develop/2.2.5；不能在验收过程中补阈值使结果通过。

## 3. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| deploy/phase20-resource-budgets.json、deploy/phase20-resource-budgets.schema.json | 精确可机读预算、单位、超限与采样/Trace 开销阈值 |
| deploy/compose.yaml、deploy/phase20-trace.yaml、deploy/runtime-contracts.json、deploy/runtime-contracts.schema.json | 预算真正生效、私有网络和现有角色一致 |
| loadtest/phase20-capacity-profile.json、loadtest/phase20-capacity-profile.schema.json、loadtest/phase20-sustained-profile.json、loadtest/phase20-sustained-profile.schema.json | 最终四阶梯/三重复及两次 60 分钟运行的负载、阈值和故障合同 |
| scripts/ci/phase20_budget.py、test_phase20_budget.py、scripts/verify-phase20-budget.sh | 实际 inspect、增长、开销、饱和和故障降级证据 |
| scripts/ci/phase20_closure.py、test_phase20_closure.py、scripts/verify-phase20-closure.sh | 单候选固定编排、阶段停点与归属清理，拒绝临时阈值覆盖 |
| scripts/ci/phase20_sampler.py、test_phase20_sampler.py、phase20_evidence.py、test_phase20_evidence.py（均在 scripts/ci） | 最终资源/生命周期/链路证据可复核，拒绝缺失、错窗和漂移 |
| scripts/verify-phase20-evidence.py、scripts/ci/verify_runtime_contracts.py、test_runtime_contracts.py（后者同 scripts/ci） | 最终入口与机器合同严格验证，不降低历史合同 |
| docs/observability-resource-budgets.md、docs/phase20-acceptance-matrix.md、docs/phase20-capacity-methodology.md | 最终矩阵、候选绑定、数据增长、开销与超限合同 |

产品行为若暴露新的阻断缺陷，不得在预算工作中泛化修复；先按实际风险修订规划及具体文件
清单，再进入实现。日志、状态与版本元数据文件遵循总方案。

## 4. 固定验收、开销口径与回归

| case_id | 操作与通过规则 |
| --- | --- |
| B01 实际预算 | schema/runtime 合同与 Compose 渲染一致；逐容器 inspect 核对 CPU/内存，连接/队列核对生效配置及受控峰值，单位换算可重算，无缺失目标或未生效限制 |
| B02 正常窗口 | 冻结稳定负载下业务/观测闭合，CPU/RSS/容器用量/队列/磁盘均符合各自统计和阈值，采样缺失/自身开销符合合同；不能以全程平均掩盖峰值 |
| B03 开销对照 | 同宿主/配方/负载，观测关闭、正常观测、Trace 关闭/启用及采样器关闭/启用按登记组合各三次独立短运行；逐组合报告原值、差值及开销门禁 |
| B04 观测故障与饱和 | 对已登记出口、Trace 队列及至少一个可安全触发的有界连接/队列实施故障或饱和，保存真实触发证据；拒绝/丢弃/重试/背压符合预算，已接受业务事实不丢失，恢复后水位闭合 |
| B05 磁盘安全水位 | 在独占归属目录或有限配额 fixture 触发冻结水位，不填满宿主；验证触发动作、停止/恢复、增长读数和 Trace 轮转上限；不得触及其他项目数据 |
| B06 关停与清理 | 有积压时 SIGTERM，按合同完成关闭或有理由的有限退出；lease/offset/业务事实不破坏，归属工件清理可核对，无全局 prune |
| B07 最终工具预检 | U1～U4 固定编排自测；同一非正式候选真实短预检覆盖启动、负载、业务/三通道水位、C01 关联、R02/R03/R04/R06/R07/R08 生命周期、故障、关停、发布校验和归属清理 |

开销比较以各组合三重复的 median 为统计，按候选相同稳定负载比较业务 P99、CPU 核秒/RSS
等原值。关闭到启用的差值与比例都保留；基线为零时比例为 null，只判冻结绝对差阈值。
“关闭观测”的具体范围在 profile 列出；若进程仍在产生日志/指标，差值只代表被关闭的
采集/运输/存储部分，不能声称测出所有观测成本。停止采样器的对照使用同样冻结的低开销
基准计数源收集业务及资源结果，不能以无记录为零开销。

饱和测试不得通过临时放宽 quota/阈值使结果通过；与正常容量 profile 不同的注入或诊断负载
明确 formal=false。Trace 的有理由丢弃与可靠业务事件分别判定；普通观测出口失败不能
解释已接受业务丢失。缺少触发或恢复证据属于 incomplete，动作不符合合同属于门禁失败。
连接/队列无法安全触发时必须开工前冻结最低层受控验证，不可执行后静默删例。

本批命令如下；phase20 入口待实现，B07 的短窗口与固定案例由预检合同读取：

```bash
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.2.5
scripts/verify-phase20-budget.sh --manifest <本批非正式候选manifest> --work <预算新目录>
python3 scripts/verify-phase20-evidence.py --budget <预算目录>
scripts/verify-phase20-closure.sh --preflight --manifest <同候选manifest> --work <预检新目录>
python3 scripts/verify-phase20-evidence.py --preflight <预检目录>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.5 --base-ref origin/main
git diff --check
```

回归角色/端口边界、真实资源限制、生命周期、观测失败不阻塞业务以及安全清理；
不在本批提前运行 06 的正式三重复容量或两次 60 分钟矩阵。

## 5. 冻结时点与完成条件

B01～B07 及固定门禁全部通过、执行状态 complete，预算与行为一致，开销及增长事实完整。
冻结构建配方、依赖/第三方镜像 digest、矩阵/profile/工具、预算/保留/Trace 合同和发布清单，
创建同名实际日志、更新 2.2.5 并提交后完成。本批不替代最终容量或持续运行结论。

05 的预检 manifest 只绑定该预检的 revision 和制品，不冻结尚未构建的最终自研 digest。
06 fetch 后选定包含本批完成提交的 main revision，按冻结配方构建实际 Bundle/镜像，再
冻结最终 manifest 与宿主证据，先执行该候选预检再执行正式矩阵。不能将 05 receipt 的
revision 改成 06 候选，也不能以配方相同为由省略 06 当前候选预检。
