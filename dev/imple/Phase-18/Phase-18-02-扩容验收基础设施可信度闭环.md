# Phase-18-02：扩容验收基础设施可信度闭环实施方案

> 目标版本：`2.0.2`
> 开发分支：`develop/2.0.2`
> 批次性质：验收基础设施与测量合同整改；不修复或调优产品扩容能力

## 1. 批次目标

在重新实施多副本扩容前，先把 MySQL lease、RabbitMQ backlog、Kafka consumer group、
逐实例替换和单/多副本吞吐测量收敛为可重复、可解释、失败现场完整的验收基础设施。

本批只证明“测量工具能够可信地区分产品通过、产品失败和验收基础设施失败”，不证明
Backend、Business Worker、Search Indexer、Router 或 Marshaller 已达到 Phase-18-03 的
多副本完成条件，也不生成或接受 `scaling.json`。

## 2. 固定输入与历史处置

- Phase-18-01 的 `capacity-failure.json`、固定 seed/corpus、原始资源样本和第一瓶颈结论继续作为输入，
  不把失败基线包装为通过 evidence。
- 原多副本方案的最后一次探索候选为 revision
  `32c1c90311570b0663fa2fd2f13d5b29ee01d6ce`，其失败地图记录 8 个失败场景：
  Worker/Indexer/Marshaller replacement、runtime ownership，以及 Worker/Indexer/Router/Marshaller
  成对扩容。对应比值为 `1.1372/1.2612/1.0378/0.9996`。
- 上述候选的 MySQL 空字段解析、Kafka group member 列解析、Marshaller 单样本计时、Rabbit consumer
  顺序启动计时和失败前未落盘现场均属于本批必须消除的已知测量风险。原结果只能用于构造回归 fixture，
  不能作为 Phase-18-03 的通过或失败 evidence。
- 规划时远端不存在 `develop/2.0.2`。现有同名本地分支保存原多副本失败探索，实施本批前必须先在
  用户知情下原样归档为非开发分支，不改写其 commit、candidate 或 evidence；随后从本批开工时最新
  `upstream/main` 创建新的 `develop/2.0.2`。本次规划提交不执行该分支迁移。
- 原多副本产品与验收改动不得整段合并到新分支。只允许按本方案逐项审计后移植验收基础设施改动，
  并以新提交、新候选和新 qualification receipt 重新证明。

## 3. 实施范围

### 3.1 结构化观测与 parser

- MySQL lease 观测使用保留 NULL/空字符串边界的结构化输出；不得再以通用空白 `split()` 解析字段。
  fixture 必须覆盖 future lease、过期回收、发布后 owner/expiry 为空、时间精度和不存在行。
- Kafka group 观测不得把 partition 列当成 consumer member。优先使用仓库内 Go/Admin API 探针；
  若仍使用 CLI，必须按受控表头解析并覆盖空组、单成员、双成员、四 partition、rebalance 和无 offset。
- RabbitMQ queue、Kafka offset、逐实例应用 counter 和外部副作用计数使用同一采样时间轴；每个样本记录
  相对时间、观测来源和解析状态。解析失败是 acceptance-infrastructure failure，不得映射成产品失败。

### 3.2 启动、稳态处理与计时边界

- Business Worker/Search Indexer 分开记录 cold-start readiness 与稳态 backlog drain。吞吐计时只能在
  所有目标消费者健康、固定积压已确认、消费者保持暂停后，从同一受控 release 时刻开始；单/双副本
  使用完全相同的生命周期，不把顺序 Compose 启动时间计入相对吞吐。
- Marshaller 在消费者启动前固定 Kafka backlog，并以真实 group member/partition assignment 作为释放前置。
  正式 qualification 的单/双副本测量窗口均不得短于 30 秒，至少取得 10 个有效中间样本；消息数由
  deterministic preflight 校准后冻结，同一候选不得运行中自适应或挑选有利规模。
- Router 保持宿主持久连接发布器和相同总并发，另记录单 Router 并发阶梯、双 Router 并发阶梯及直接
  Kafka producer ceiling。负载器不能成为首个瓶颈，应用与依赖上限必须分开报告。
- 所有成对测量显式记录启动耗时、有效测量起止、首/末 counter、样本数、单/多副本执行顺序和计算公式；
  qualification 只判定计时合同是否可信，不判定 Phase-18-03 的扩容门槛。

### 3.3 确定性替换工作保持

- Backend、Worker、Indexer、Router、Marshaller replacement 使用受控的持续工作或 release barrier，
  在移除副本前证明目标副本与存活副本均已处理、外部积压仍存在，避免停机期间任务已经耗尽。
- Search Indexer 的恢复任务必须在原固定积压之外使用独立 event-id 集合；发布、ack、索引副作用和
  最终空队列分别核对，不能用瞬时 queue=0 代替 Outbox 与 RabbitMQ 同时收敛。
- 每个断言在失败前先原子写入 before/during/after 队列、offset、逐实例 counter、成员/partition、
  外部副作用和容器状态。异常聚合器引用附件 checksum，不能只保留一条 RuntimeError。
- 夹具必须能稳定制造并识别：真实 survivor 不推进、任务已耗尽造成的假阴性、重复 ack/commit、
  旧 owner 晚提交和恢复实例确实处理新任务。

### 3.4 资源与瓶颈定界

- 为 Worker、Indexer、Router、Marshaller 的单/多副本场景增加与 Backend 同级的宿主及逐容器
  CPU、RSS、I/O 样本，并记录 MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics 的直接压力指标。
- Router 至少执行固定并发阶梯和直接 Kafka ceiling；Marshaller 至少记录 poll、store、commit 分段耗时、
  sink 请求速率和 partition 分配；Worker/Indexer 分别记录 handler/数据库或 Elasticsearch 写入耗时。
- `bottleneck-diagnostic.json` 必须把结论限定为 component、shared dependency、load generator 或
  inconclusive，并附支持样本。它可以触发后续计划评审，但不能自动修改 1.3 门槛。
- 若证据证明共享依赖 ceiling 已在单副本时达到、应用未饱和且增加副本无法提供 1.3 倍，必须先在
  `update` 修订 Phase 总方案并获得用户确认，再启动 Phase-18-03；不得在 Phase-18-03 看到结果后降标。

### 3.5 Qualification evidence

生成 `qualification.json` 和 `bottleneck-diagnostic.json`，至少绑定：

- `2.0.2` candidate revision、manifest/Bundle/image digest、验收脚本和探针 digest；
- 宿主资源、Docker/Compose 版本、corpus/snapshot、固定 workload 与 release barrier 参数；
- parser fixture、单/双副本计时资格、替换工作保持、失败附件完整性和资源采样结果；
- `status=qualified|failed`、逐项 reason code、清理结果和 Secret/私有路径扫描。

qualification 聚合器拒绝旧 candidate、跨 revision 拼接、少于最小测量窗口/样本数、成员身份不可判定、
失败无原始附件、资源采样缺失或受管 Compose 资源未清理。`qualified` 只授权 Phase-18-03 使用该工具，
不表示任何产品扩容能力已经通过。

## 4. 不在本批范围

- 不修改 Backend、Worker、Indexer、Router、Marshaller 的业务处理、并发模型、存储算法或产品资源配置。
- 不把 1.3 门槛改为非阻断，不改变 Backend 已记录为非阻断容量特征的现行合同。
- 不运行 Phase-18-03 完整正式扩容矩阵，不生成 `scaling.json`，不更新原失败 candidate receipt。
- 不实施双 Elasticsearch、端到端背压、独立 canary、合同生成或 Milestone 5 最终矩阵。

## 5. 预计直接影响

- `scripts/ci/phase18_scaling.py`、对应 evidence validator、self-test 和 fixture。
- RabbitMQ/Kafka/MySQL 观测探针及 `loadtest` 中直接服务验收的最小工具。
- Phase-18 qualification schema、runner、脱敏失败附件和同名实施记录。
- 不应改动五类产品组件的运行代码；若实施中发现必须修改产品才能取得资格，停止并先修订本方案。

## 6. 验收与完成条件

1. MySQL 和 Kafka parser 的正反 fixture 通过，真实 consumer member、partition 和空字段不再混淆。
2. Worker/Indexer 的单/双副本使用同一 release barrier，启动与稳态处理分开计时，固定输入和计数闭合。
3. Marshaller 两侧测量均达到 30 秒和 10 个样本下限，能观察真实双 member 分区与 rebalance。
4. 五类 replacement 在工作保持成立时可观察 survivor/replacement；任务耗尽时返回夹具失败而非产品失败。
5. 任一断言失败均先落盘足以复核的 before/during/after 附件，聚合器能区分产品与验收基础设施失败。
6. Router/Marshaller 及 Rabbit consumer 的资源和依赖 ceiling 可机器判定或明确标为 inconclusive。
7. `qualification.json` 为 qualified、Secret/路径扫描和受管资源清理通过；未生成 `scaling.json`。
8. 根 `VERSION` 仅在上述条件全部通过的完成提交更新为 `2.0.2`。

## 7. 固定验证命令

实现后固定门禁为：

```bash
PYTHONPATH=scripts/ci python3 -m unittest \
  scripts/ci/test_phase18_scaling.py scripts/ci/test_phase18_evidence.py
scripts/verify-phase18-scaling.sh --self-test
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-phase18-scaling.sh --qualify --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK"
python3 scripts/verify-phase18-evidence.py --qualification "$GOPULSE_PHASE18_WORK/evidence/qualification.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.2 --base-ref upstream/main
git diff --check
```

不得用 Phase-18-03 正式矩阵代替本批 qualification，也不得因 qualification 通过而记录产品扩容通过。

## 8. 实施记录与后续交接

完成前创建 `dev/logs/Phase-18/Phase-18-02-扩容验收基础设施可信度闭环.md`，只记录实际修改、命令、
qualification 结果、失败附件、偏差和瓶颈结论。成功时更新 `VERSION=2.0.2` 并创建完成提交。

Phase-18-02 合入后，Phase-18-03 必须从新的最新 `upstream/main` 创建 `develop/2.0.3`。原本地
`develop/2.0.2` 的产品实现只能逐项重新审计和移植，不得沿用旧候选、旧 receipt 或旧正式矩阵次数。
