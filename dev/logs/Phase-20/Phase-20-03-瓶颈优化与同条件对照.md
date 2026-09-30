# Phase-20-03：瓶颈优化与同条件对照实施日志

## 已完成工作

- 读取总方案、03 方案及 01/02 实际记录，按前置证据冻结 `verify_only`：无已证明的产品瓶颈，不创建 B1，不计算改善率。
- 在 `update` 提交解锁规划 `18e3543`，经 PR #203 以 merge commit `06eef4644fe03561991212ad18a5cb8be3a3c682` 合入主线；随后 fetch，并从该 `origin/main` 创建 `develop/2.2.3`。
- 增加 optimization profile/schema、runner、wrapper、O01/O02/O03/O05 测试和统一 evidence 入口。沿用 01 的空项目、确定性配方、台账关联、四维独立恢复、资源采样和归属清理，所有运行叠加 02 的冻结 Trace overlay。
- 将 B0 绑定为版本 `2.2.2`、revision `4f867ad21783d158619ea88563bb6190364c6398`、Git tree `1b93f2b11f7ad5d679cba77987c4007da29a1c9c`。重建九个自研镜像，冻结各 image ID、第三方 digest、Collector 身份、运行输入和工具哈希。
- 以修正后的正式命令接口重新生成合同，旧合同和失败预检保留在独立私有目录；最终合同下两格预检通过，结果为 `complete / target_met / preflight_passed`。
- 正式执行四阶梯三重复、十二个独立空项目；工具及独立 evidence 入口均重算为 `execution_status=complete`、`capability_status=target_met`、`optimization_status=not_needed`。12 个单元均通过同步请求、台账关联、四维恢复和归属清理门禁。
- 90,000 个测量请求的超时、传输错误、意外错误、显式拒绝和丢弃调度槽均为零。最高 P95 为 46.669917 ms、最高 P99 为 148.581729 ms；最长四维恢复观察值为 40.73648422000042 秒。
- 在 `docs/phase20-optimization.md` 发布各阶梯三次的吞吐、P95/P99、排空、四维恢复、调度滞后、请求/错误、CPU/RSS、磁盘/ES 增长与队列原值及 median/min/max/CV；公开投影严格限定数值列，未发布对象、用户、请求、Trace 或凭据身份。原始时间序列和关联证据完整保留在私有正式目录。
- 同步完成版本为 `2.2.3`，更新 README、能力状态和阶段执行状态。交付核对确认封闭允许集合之外的 1,136 个仓库条目与 B0 的 Git 内容及权限一致；封闭版本元数据只有登记的版本字段变化，无产品行为改动。

## 实际变更文件

- `scripts/ci/phase20_optimization.py`、`scripts/ci/test_phase20_optimization.py`、`scripts/verify-phase20-optimization.sh`。
- `scripts/verify-phase20-evidence.py`。
- `loadtest/phase20-optimization-profile.json`、`loadtest/phase20-optimization-profile.schema.json`。
- `docs/phase20-optimization.md`、本实施日志。
- `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`。
- `README.md`、`docs/capability-status.md`、`dev/phases/Plan.md`、`dev/phases/README.md`、`dev/phases/Phase-20-端到端性能闭环与可观测治理.md`。

## 已执行命令与结果

- `git fetch origin`；规划解锁提交、推送及 PR #203 merge；从更新后的 `origin/main` 创建 `develop/2.2.3`：成功。
- `python3 -m unittest scripts.ci.test_phase20_optimization scripts.ci.test_phase20_evidence`：11 项通过。
- `python3 -m unittest scripts.ci.test_phase20_chain`：02 链路固定 3 项通过。
- `python3 -m py_compile scripts/ci/phase20_optimization.py scripts/ci/test_phase20_optimization.py scripts/verify-phase20-evidence.py`：通过。
- `scripts/verify-phase20-optimization.sh --help` 与 `git diff --check`：通过。
- `scripts/verify-phase20-optimization.sh --init-contract --contract /var/tmp/gopulse-phase20-03-20261001/contract.json`：首次合同生成成功；正式入口补齐省略模式参数的用法后该合同不再用于验收。
- 首次私有目录下 `preflight-r1`：缺少 `kafka-python` 元数据而失败；`preflight-r2`：宿主可用磁盘低于 profile 门槛而失败；两次失败记录均保留，未改写为成功。
- `python3 -m pip install --target /tmp/gopulse-phase20-tooldeps kafka-python==2.2.15 python-snappy==0.7.3 cramjam==2.11.0`：成功，依赖只安装到仓库外。
- 核对旧 `gopulse-phase2002`、`gopulse-phase2002b`、`gopulse-phase2002c` 三个项目标签后停止其运行容器；保留容器、卷和网络。核对没有容器引用后逐个移除 22 个旧 `gopulse/acceptance:*` 镜像标签；没有执行 global prune，未删除当前候选或 Phase 20 镜像。可用磁盘由约 95G 恢复到约 101G，预检前运行容器列表为空。
- `scripts/verify-phase20-optimization.sh --init-contract --contract /var/tmp/gopulse-phase20-03-20261001-r2/contract.json`：成功，manifest digest 为 `sha256:5740b644be10441336ae0ad025b5acc7fae0918f8c78e31e8c21ac0ceef97cc0`。
- `env PYTHONPATH=/tmp/gopulse-phase20-tooldeps scripts/verify-phase20-optimization.sh --preflight --contract /var/tmp/gopulse-phase20-03-20261001-r2/contract.json --work /var/tmp/gopulse-phase20-03-20261001-r2/preflight-r2`：两格完整通过。
- `env PYTHONPATH=/tmp/gopulse-phase20-tooldeps scripts/verify-phase20-optimization.sh --contract /var/tmp/gopulse-phase20-03-20261001-r2/contract.json --work /var/tmp/gopulse-phase20-03-20261001-r2/formal-r1`：12 格完成，结果为 `complete / target_met / not_needed`。
- `env PYTHONPATH=/tmp/gopulse-phase20-tooldeps python3 scripts/verify-phase20-evidence.py --optimization /var/tmp/gopulse-phase20-03-20261001-r2/formal-r1`：独立重算通过；完整输出保存于仓库外 `external-verification.json`。
- `python3 /var/tmp/gopulse-phase20-03-20261001-r2/publish-summary.py` 与同脚本 `--check`：逐项核对原始工件哈希后生成脱敏原值/聚合，公开 Markdown 与该白名单投影完全一致。
- `python3 /var/tmp/gopulse-phase20-03-20261001-r2/verify-delivery.py`：1,136 个 B0 文件的 Git 内容/权限一致，包元数据与环境配置仅变化版本字段；最终产品/行为配置 identity digest 为 `sha256:9d8cb33c439691c5cd11cafbd2dde59375b8bce26bf0445c577c056680d0500b`。合同、正式 verification、公开 Markdown 和六处完成元数据绑定于私有 `final-delivery-identity.json`。
- 最终候选 `python3 -m unittest scripts.ci.test_phase20_optimization scripts.ci.test_phase20_evidence`：11 项通过；`python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/2.2.3 --base-ref origin/main`、`git diff --check`：通过。正式结束后 `docker ps` 无运行容器。

## 偏差、限制及后续项

- Bash wrapper 的环境变量处理和正式入口默认模式在冻结最终合同前修正；修正后重新运行固定测试并生成新合同，未复用旧工具哈希下的候选证据。
- 一次命令提前创建工作目录，入口按不覆盖规则返回 `FileExistsError`；改用新的 `preflight-r2`，未覆盖该目录。一次清理新目录的 `rm -rf` 命令被自动执行审查拒绝；改用直接创建新目录，不再执行删除。
- O04 因 `verify_only` 没有 B1 或实验产品改动，显式为 `not_applicable`；O02 在本模式验证拒绝虚构改善率。没有优化收益或统计显著性声明。
- 合同初始化限于本批完成前的 B0 产品状态；完成版本元数据不能冒充独立构建并测试过的产品制品。证据只覆盖固定宿主、配方、业务比例、Trace、采样和有限窗口。
- 最终源码核对首次直接按文件字节计算 Git blob，遇到既有 CRLF 工作树与 LF 仓库内容而失败；确认 `core.autocrlf=input` 且 `git hash-object` 与 B0 一致后，改用 Git clean 语义和文件权限核对并通过。未修改或批量规范化产品文件。
- 最终合同 digest 为 `sha256:190405c1c6c2b72656d6699afcc5772b6cd2e0284be4431a44682bc92d8a1635`；正式证据位于 `/var/tmp/gopulse-phase20-03-20261001-r2/formal-r1/`。CPU/RSS 是所有归属容器的采样合计峰值，队列是分区/队列采样合计峰值，宿主磁盘包含验收原始文件和其他宿主活动，ES 增长只覆盖本单元有限窗口；这些值不是优化成本差额。完整生命周期、预算治理及持续运行仍属于 04～06。
