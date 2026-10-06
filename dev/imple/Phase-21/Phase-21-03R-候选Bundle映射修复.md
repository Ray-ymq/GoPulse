# Phase-21-03R：候选 Bundle 服务映射修复

> 状态：执行中（Phase-21-03 的有界修复合同）。目标候选仍为冻结产品 `2.3.2`，完成版本仍为 `2.3.3`。
> 本补充合同由 Phase-21-03 首次 candidate 构建失败触发；不重置该批次已消耗预算，也不复用失败候选。

## 1. 观察到的失败与已证明原因

- Phase-21-03 的冻结候选构建已完成各产品镜像构建，但在 `release_artifacts.py` 组装产品 Compose Bundle 时失败：`ValueError: unmapped product service: business-worker-2`。
- 冻结 Compose 还包含 `search-indexer-2`、`router-2`、`marshaller-2` 和 `observability-elasticsearch`；它们分别应复用无后缀产品镜像或第三方 `elasticsearch` 镜像。
- `product_compose()` 仅对 `backend-2`、`platform-api`、初始化服务做别名转换，副本服务名和观测 Elasticsearch 名称没有闭包映射。`victoriametrics` 已由现有第三方锁文件归一化逻辑覆盖，不属于实际缺口。
- 这是候选构建工具的映射缺口，不是产品运行失败；S01～S07 尚未执行。

## 2. 修复范围

只允许修改以下文件：

- `scripts/ci/release_artifacts.py`：补齐已冻结 Compose 服务到逻辑镜像的显式别名。
- `scripts/ci/test_release_artifacts.py`：增加映射闭包回归测试，覆盖所有副本别名、`platform-api`、初始化服务和 `observability-elasticsearch`，并确认未知服务仍被拒绝。
- `scripts/ci/runtime_acceptance.py`：允许 Phase-21-03 严格 preflight 绑定同一 candidate manifest，并在 formal 模式拒绝未绑定 manifest 的旧式 preflight receipt；保留 02 的无 manifest 源码 preflight 兼容性。
- `scripts/ci/verify_runtime_contracts.py`：允许并校验严格 preflight receipt 的 manifest digest 字段，继续拒绝格式错误或 formal 证据缺少 manifest/preflight 绑定。
- `scripts/ci/test_runtime_acceptance.py`：保护严格 preflight evidence 的 manifest digest 合同。
- `scripts/ci/runtime_acceptance.py`：在 clean detached checkout 中先创建 `.run/` 私有证据根，再创建本次 runtime project 目录。
- `scripts/ci/test_runtime_acceptance.py`：覆盖不存在 `.run/` 父目录时的运行目录初始化。

不修改产品源码、Compose、运行合同、manifest schema、正式验收入口或验收标准；不手工生成/修补 candidate、receipt、summary 或 publication。

## 3. 验收与回归门禁

1. `python3 -m unittest discover -s scripts/ci -p 'test_release_artifacts.py'` 通过。
2. `python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'` 通过。
3. `python3 -m unittest discover -s scripts/ci -p 'test_runtime_*.py'` 通过。
4. `python3 scripts/ci/verify_runtime_contracts.py --candidate 2.3.2` 通过。
5. 严格 manifest preflight CLI 能接受 `--manifest`，生成的 preflight receipt 带有 manifest digest；formal runner 只接受与该 manifest 相同 digest 的 preflight receipt；旧式无 manifest preflight 仍可被单独识别但不能解锁 formal candidate。
6. clean detached checkout 在不存在 `.run/` 时能创建私有 runtime evidence 根，不依赖任务工作树历史目录。
7. 在修复后的同一提交上重新构建全新 candidate；Bundle manifest 的 Compose 映射闭包、digest、source revision 和校验均通过。失败 candidate 不得继续进入正式验收。

修复门禁通过后，按原 Phase-21-03 顺序重新执行 preflight、正式 S01～S07、receipt verifier、publication 和 publication verifier。旧的失败构建输出只保留为历史失败证据，不与新候选拼接。

## 4. 执行预算与停止条件

- 本修复预计活跃时间 45 分钟，阶段上限 85 分钟；Phase-21-03 的累计上限仍为 180 分钟，之前约 35 分钟继续计入，不因新候选或新提交重置。
- 预算分配：发现/合同登记 5 分钟（已完成）、实现 25 分钟、直接回归 15 分钟、修复后候选重建与 Bundle 核验 40 分钟。正式验收继续使用原方案剩余预算。
- 已完成首次失败后的最小诊断；不再进行同一原因的无界尝试。若修复后映射闭包、构建或直接回归再次失败，立即保存结果并停止正式矩阵，最多只做一次不超过 10 分钟的受影响边界诊断。
- 若累计成本达到 90/144 分钟，报告已完成门禁、剩余预算和下一步；达到 180 分钟或剩余预算不足以完成下一条命令及安全清理时停止，不更新完成版本、不生成完成提交。

## 5. 完成条件

修复文件和回归测试已提交到当前未完成的 `develop/2.3.3` 批次；新 candidate 构建并严格验证通过；随后原 Phase-21-03 的 formal S01～S07、清理、脱敏 publication 及最终版本门禁全部完成。此补充合同不降低或替代任何原有阶段验收条件。
