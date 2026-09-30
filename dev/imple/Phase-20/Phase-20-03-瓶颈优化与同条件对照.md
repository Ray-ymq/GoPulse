# Phase-20-03：瓶颈优化与同条件对照

> 目标版本：2.2.3；开发分支：develop/2.2.3。
> 当前状态：未开始，等待 01/02 的诊断证据解锁；不得提前创建本批分支。

## 1. 目标与前置决策

以 02 完成后的单链路时刻、可信容量基线和资源事实识别一个主要限制，完成一次同条件实验。
本文件不预先授权 SQL、缓存、GC、连接池或消息参数优化。

创建本批分支前，必须在 update 填写下表并合入 main：

| 冻结项 | 当前状态/要求 |
| --- | --- |
| 执行模式 | optimize 或 verify_only 二选一；后者必须有可信门禁达标且无已证明产品瓶颈的原始证据 |
| 具体问题和输入条件 | 待 01/02 证据；区分产品瓶颈、观测开销、验收错误与未产生事件 |
| 假设和因果证据 | 待定位；记录竞争解释及如何排除，CPU/lag 相关性不能单独解锁 |
| B0 版本、revision、制品与合同 digest | 包含 02 完成提交的主线候选；实施前冻结 |
| 单一改动及逐文件清单 | 待定位后逐一列出；当前不授权任何产品文件 |
| B1 构建方式和可变项 | optimize 必填，仅上述单一改动及版本元数据；verify_only 填 not_applicable 并说明，无 B1 |
| 主改善指标、最小有意义阈值 | 指定阶梯、单位、改善方向、三重复聚合方式、阈值及依据；optimize 必填，不看结果后修改 |
| 非退化门禁与最小产品回归 | 指定数据/消息/公共接口案例、错误率/尾延迟/资源上限、准确命令与证据路径；两种模式都必填 |
| 最终状态与撤回验证 | optimize 登记撤回文件、最终候选构建方式及撤回后的最小固定门禁；verify_only 登记版本同步后的同等门禁 |
| 无需优化路径 | 明确 B0 三次单候选复验，不生成 B1；主改善指标/阈值填 not_applicable，仍执行全部正确性与非退化门禁 |

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
loadtest/phase20-optimization-profile.schema.json 和 docs/phase20-optimization.md。
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
