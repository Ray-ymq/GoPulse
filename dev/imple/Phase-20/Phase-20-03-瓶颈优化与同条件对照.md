# Phase-20-03：瓶颈优化与同条件对照

> 目标版本：2.2.3；开发分支：develop/2.2.3。
> 当前状态：已由 01/02 证据解锁；执行模式为 `verify_only`，不得创建 B1。

## 1. 目标与前置决策

以 02 完成后的单链路时刻、可信容量基线和资源事实识别一个主要限制，完成一次同条件实验。
本文件不预先授权 SQL、缓存、GC、连接池或消息参数优化。

创建本批分支前，已在 `update` 根据 01/02 实际结果填写并合入主线下表：

| 冻结项 | 当前状态/要求 |
| --- | --- |
| 执行模式 | `verify_only`。01 的正式 12 单元为 `execution_status=complete`、`capability_status=target_met`，02 的 C01～C07 为完整真实矩阵；两批均没有证明产品瓶颈的原始证据。 |
| 具体问题和输入条件 | 复验 01 的 50/100/150/200 RPS、原配方、业务比例和独立空项目语义，并复核 02 的发帖→Outbox→RabbitMQ→Indexer→搜索可见链路。01 的变化来自新验收隔离/恢复/采样语义，02 的 C02/C03 分别是受控 Elasticsearch 延迟和 Collector 故障；它们不能归因成 SQL、缓存、GC、连接池或消息参数瓶颈。 |
| 假设和因果证据 | 不登记产品优化假设。竞争解释已由 01 的失败归档、采样开关对照和独立恢复记录，以及 02 的依赖故障/重试/重启案例区分；CPU、Kafka lag、Elasticsearch 延迟或 Trace 导出开销的相关性均不单独解锁产品改动。 |
| B0 版本、revision、制品与合同 digest | B0=`2.2.2`，主线 revision=`4f867ad21783d158619ea88563bb6190364c6398`（2026-10-01 fetch 后的 `origin/main`，与 02 完成候选树一致）。固定输入 digest：`loadtest/phase20-capacity-profile.json`=`sha256:87726dea82dd0907a23730a9fc9b87ca22e83e1d95cce9429100f2312a7b1a9e`、`loadtest/phase20-capacity-profile.schema.json`=`sha256:5f6942dcc22e9d1fa5448f845c4ef2dfc181d6cd4989bb81a70a0a32b8f95b02`、`deploy/runtime-contracts.json`=`sha256:6ca5d437eefcacf02ed974c5c346f18f394c59ba6bcb927ef14a731341a6d0a3`、`deploy/phase20-trace.yaml`=`sha256:510c72d064bac1402a2c385e52fde09572550da6cc7998f706120119c7bcc553`、`deploy/otel/phase20-collector.yaml`=`sha256:6d49672bd90b9c669fbde3effc427db360dd126a4d961e74d891418bd0cf2053`。制品不复用 02 私有 manifest；预检从该 revision 重新构建全部自研镜像并将每个不可变 image ID、依赖 digest 和最终 manifest digest 写入本批合同，正式运行只引用该已生成合同。 |
| 单一改动及逐文件清单 | 无产品改动。仅允许本批登记的验收工具、其测试、优化 profile/schema、验收说明、实施日志和 `VERSION`/封闭版本元数据；不允许 SQL、缓存、GC、连接池、消息参数、业务接口或运行行为文件改变。 |
| B1 构建方式和可变项 | `not_applicable`：`verify_only` 不生成 B1，不改变产品源码、镜像行为、运行配置、profile、采样、Trace、宿主预算或阈值；只有候选 manifest 的运行时身份和本批完成元数据可记录。 |
| 主改善指标、最小有意义阈值 | `not_applicable`。不计算改善率、不筛选最好结果、不把三次复验伪装成 A/B；每个 50/100/150/200 RPS 阶梯仍完整保留三次的吞吐、P95/P99、排空、业务/Metrics/Logs/Events 恢复、CPU/RSS/磁盘、队列和错误原值及 median/min/max/CV。 |
| 非退化门禁与最小产品回归 | 每个单元必须满足：到达/终态台账完整、目标 RPS 达成、profile 同步错误/超时/拒绝为零、P95/P99 和调度滞后不超过既有 gate、四个恢复维度均在 120 秒内、归属清理 inventory 恢复且无 global prune；02 链路固定测试和候选合同必须通过。命令为 `python3 -m unittest scripts.ci.test_phase20_optimization scripts.ci.test_phase20_evidence`、`scripts/verify-phase20-optimization.sh --preflight --contract <冻结合同> --work <预检目录>`、`scripts/verify-phase20-optimization.sh --contract <同合同> --work <正式目录>`、`python3 scripts/verify-phase20-evidence.py --optimization <正式目录>`；原始证据固定保存于私有 `<正式目录>/`，公开仓库只保存脱敏结论。 |
| 最终状态与撤回验证 | `not_needed` 的最终候选仍是 B0 的产品源码和行为配置，提交版本同步为 `2.2.3` 只改变版本元数据。无 B1、无实验产品 diff、无撤回；最终候选重新绑定本批合同并重跑同等正确性/非退化门禁，最终状态须由 verifier 从原始证据重算。 |
| 无需优化路径 | 以 B0 执行 3 次独立重复、4 个独立阶梯，共 12 个归属空项目；不生成 B1、不报告改善率。只有全部适用 O01～O05、正确性、消息所有权、持久数据、公共接口和非退化门禁通过，才报告 `optimization_status=not_needed`。 |

### 解锁依据

- 01 的最终脱敏基线为 `execution_status=complete`、`capability_status=target_met`；12 个阶梯均有完整台账、四维恢复和归属清理，最大恢复观察上界 44.97 秒，且实施记录明确写明没有产品优化和 unresolved 容量项。
- 02 的最终候选为 `2.2.2` 主线树，C01～C07 的真实链路、重试、旧/损坏消息、Collector 故障、重启和权限边界均完成，固定门禁和 evidence verifier 通过；C02/C03 的受控依赖故障不能作为产品瓶颈证据。
- 因而本批只验证 B0 的可重复性和非退化事实；如果真实复验出现正确性、消息所有权、持久数据或公共接口失败，执行状态为阻断的 `incomplete`，不得改写为 `not_needed`。

无法建立证据时保持待解锁，修订未执行规划；不得用泛化文件授权绕过决策门槛。

## 2. 范围与交付

- 使用 01 runner 和 02 的关联证据观察一个代表性受限链路；有界诊断运行与正式对照分开。
- optimize 的 B0/B1 在同一宿主资源预算、配方、业务比例、阶梯、采样、Trace 及恢复语义下
  各三次独立重复；verify_only 只执行 B0 三次独立复验。
- 先冻结实验合同、完成确定性检查与工具预检，再执行全部规定运行；每个阶梯重建归属明确
  的空项目，停止/排空/独立恢复/清理沿用 01，禁止前一阶梯积压进入下一单元。
- 记录吞吐/尾延迟/异步新鲜度/恢复/CPU/RSS/磁盘与错误，展示全部原值及指定聚合。
- 若仅需修正验收层，保留其结果且不宣称产品性能改善；不追溯改写 Phase 19。

规划内可新增 scripts/ci/phase20_optimization.py、scripts/ci/test_phase20_optimization.py、
scripts/verify-phase20-optimization.sh、loadtest/phase20-optimization-profile.json、
loadtest/phase20-optimization-profile.schema.json 和 dev/validation/Phase-20/phase20-optimization.md。
同时允许 scripts/ci/phase20_evidence.py、test_phase20_evidence.py（同 scripts/ci）及
scripts/verify-phase20-evidence.py 增加本批模式/原值/撤回身份的严格校验。
产品文件必须经第 1 节解锁后补入本文件；日志、版本与状态文件遵循总方案封闭规则。

## 3. 结果判定与最终产品状态

- 对照工具拒绝资源、数据、Trace、业务比例和阈值漂移，允许预先登记的优化可变项及候选身份差异。
- 按模式完整执行规定重复，无最好结果筛选、无事后改变阈值或样本混合；各阶梯独立报告。
- 实际改动保留业务正确性、消息所有权、持久数据与公共接口合同；这些失败属于阻断问题。
- optimize 用指定阶梯三重复指标的 median 比较。越小越好的改善率为 (B0-B1)/B0，
  越大越好为 (B1-B0)/B0；B0=0 时只用预先冻结的绝对差阈值。公布全部原值、
  median/min/max/CV 及额外 CPU/RSS/磁盘成本，不将该比较称为统计显著性证明。

| optimization_status | 必须满足的证据 | 最终交付状态 |
| --- | --- | --- |
| improved | optimize；主指标达到冻结改善阈值，全部正确性与非退化门禁通过 | 保留登记的优化；最终候选与被测 B1 的产品行为一致，仅允许登记的完成元数据差异 |
| no_improvement | optimize；完整实验可复核，全部正确性与非退化门禁通过，但主指标未达到改善阈值 | 撤回实验产品改动，保留工具/证据/文档；最终产品源码及行为配置与 B0 对应集合一致，执行冻结的撤回门禁 |
| not_needed | verify_only；前置证据成立，B0 三次复验及全部正确性/非退化门禁通过 | 不产生 B1，不报告改善率；发布单候选复验与未改产品的事实 |

撤回必须保存 B0/B1 与最终源码/配置 digest、实际 diff、构建身份及验证结果；B1 回执保持原样。
最终版本为 2.2.3，报告清楚实验候选与交付候选的对应关系，不以 B1 的结果证明撤回后的候选。
任何正确性/非退化门禁失败均为阻断，不能简单标为 no_improvement 后完成；先保存证据并
修订规划。工具不完整也不能转为 not_needed。

## 4. 固定命令与回归

```bash
python3 -m unittest scripts.ci.test_phase20_optimization scripts.ci.test_phase20_evidence
scripts/verify-phase20-optimization.sh --preflight --contract <冻结合同> --work <预检新目录>
scripts/verify-phase20-optimization.sh --contract <同冻结合同> --work <正式新目录>
python3 scripts/verify-phase20-evidence.py --optimization <正式目录>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.3 --base-ref origin/main
git diff --check
```

以上 phase20 入口待实现；合同绑定每个被测候选 manifest 和最终交付状态。
产品最小检查、集成回归和确切对照参数必须随解锁规划冻结；不能凭未知瓶颈列出通用全面测试。
工具固定案例 O01 拒绝未登记漂移/缺重复/混合样本；O02 按方向与阈值重算改善；
O03 verify_only 不要求 B1 且拒绝伪造改善率；O04 no_improvement 检查撤回身份和最终门禁；
O05 正确性/非退化失败不能完成。两种模式只执行适用案例，not_applicable 必须有模式依据。

## 5. 完成条件

解锁记录完整、全部适用案例与规定运行完成、证据和最终交付身份核验通过、确定性与非退化
门禁通过，形成可复核 improved/no_improvement/not_needed 结论，按对应分支完成保留或撤回，
记录实际变更与限制、更新 2.2.3 并提交。
工具执行不完整或确定性行为退化时不能完成；容量未达标但原因已解释可保留为能力边界。
