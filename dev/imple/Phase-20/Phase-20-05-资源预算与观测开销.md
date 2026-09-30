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
main。完成本批时全部合同冻结；06 不再修改任何可执行或验收配置。

## 2. 允许变更文件与验收

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

## 3. 固定验收与回归

必需门禁：机器 schema/runtime 校验；实际容器资源限制读取；有界正常负载/观测故障/饱和
对照；磁盘增长与水位边界；两套最终 profile 自测；真实非正式预检涵盖启动、短负载、
业务/观测标记闭合、生命周期、SIGTERM、证据核验和归属清理。

已知命令：docker compose --env-file .env.example --file deploy/compose.yaml config --quiet；
python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure
scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence；
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json
--compose deploy/compose.yaml --env .env.example --candidate 2.2.5。
待实现固定入口：scripts/verify-phase20-budget.sh；
scripts/verify-phase20-closure.sh --preflight --work <新目录>；
python3 scripts/verify-phase20-evidence.py --preflight <同目录>。

回归角色/端口边界、真实资源限制、生命周期、观测失败不阻塞业务以及安全清理；
不在本批提前运行 06 的正式三重复容量或两次 60 分钟矩阵。

## 4. 完成条件

预算与行为一致，开销及增长事实完整，确定性门禁和真实预检通过，最终矩阵/profile/工具/工件
清单冻结，创建同名日志、更新 2.2.5 并提交。本批不替代最终容量认证或持续运行结论。
正式候选由包含本批完成提交的 main 构建，preflight 与正式候选的 revision 不能混用。
