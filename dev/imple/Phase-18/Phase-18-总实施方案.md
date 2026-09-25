# Phase 18：高并发与可观测架构收敛总实施方案

> 规划基线（2026-09-20）：主远程 `upstream/main` 为
> `fe596514487649a59678200af1cfb3ca40b4b16f`，当前完成版本为 `1.14.5`。
> Phase 18 分配 `2.0.x`，`2.0.0` 为阶段基线，可执行批次从 `2.0.1` 开始。
>
> 历史结论（2026-09-24）：Phase-18-01 以“单副本失败基线已交付，重复性与恢复门禁未通过”
> 收口；`capacity-failure.json` 是诊断输入，不是通过 evidence。
>
> 权威重划（2026-09-26）：废止 2026-09-25 版 Phase-18-02 至 Phase-18-06 的批次设计、
> 完成条件和长矩阵入口。旧 `develop/2.0.2`、其后续探索提交、候选、qualification/scaling receipt
> 只能作为历史诊断输入，不能完成本版任何批次。自 Phase-18-02 起，每个实施批次的全部固定完成门禁
> 串行总预算不得超过 120 分钟，任何单一验收进程不得运行满 2 小时。

## 1. 阶段目标

GoPulse 保持“高并发业务系统 + 内建可观测系统”定位。本阶段不新增业务域，在固定 Linux `amd64`
Compose 宿主上，以有界、可中断、可复核的短任务依次交付：

- 确定性容量测量与明确的瓶颈结论；
- Backend、Business Worker、Search Indexer、Router、Marshaller 的多副本运行与所有权正确性；
- 业务搜索与 Logs/Events 两个 Elasticsearch 故障域；
- HTTP、MySQL、Outbox、RabbitMQ、Kafka、Marshaller 和 sink 的有界背压；
- 不依赖可观测数据面回传的 lifecycle 健康与 canary；
- Envelope、日志、指标和 API error 的机器合同源；
- 有界升级、恢复、完整产品抽样和 Milestone 5 证据收口。

Phase 18 不再设置一次性“全量最终矩阵”。每个批次证明一个独立边界；最终批次验证证据血缘和
跨边界高风险路径，不重复执行此前所有成功场景。

## 2. 强制时间与停止合同

### 2.1 验收任务定义

验收任务是一次命令调用，使用一个候选、一个强归属 work directory 和一个最终原子 receipt。
从进程启动到退出，包括预检、场景、失败取证、清理和 receipt 落盘，均计入任务耗时。

所有 Phase-18-02 及以后任务必须同时满足：

| 层级 | 硬约束 |
| --- | --- |
| 单场景 shard | 预算不超过 20 分钟；达到预算立即失败，不自动重试 |
| 批次主运行器 | 内部总 deadline 为 90 分钟；第 80 分钟必须停止启动新 shard 并进入清理 |
| 外部 watchdog | 第 100 分钟终止失控的主运行器及其受管子进程 |
| 静态门禁 | 单元测试、schema/evidence、版本、分支和 diff 检查串行合计不超过 20 分钟 |
| 批次完成门禁 | 上述主运行器与静态门禁串行合计不超过 120 分钟 |

- runner 每 60 秒更新 `progress.json`，记录当前 shard、已用/剩余预算和最后观测时间。
- 超时统一产生 `status=failed`、`reason_code=time_budget_exceeded` 的失败 receipt；不得等待、
  提高 timeout、缩小既定数据后在同一候选重跑，或把超时改记为产品失败。
- 清理只处理当前 token/project 的强归属资源，不运行 Docker 全局 prune。清理预算耗尽时记录
  `cleanup_incomplete` 并退出；清理由独立的有界运维任务处理，不能让验收进程继续越过 2 小时。
- 同一候选的正式批次主运行器最多执行一次。失败后只运行最小定向测试；产生有实质改动的新候选后，
  才能再次执行正式门禁。
- 同一阻断原因连续出现在两个新候选，或任何场景无法在 20 分钟内产生有效判定，立即停止当前批次，
  回到 `update` 拆分任务或修订合同；禁止第三次盲目重跑。
- 若有效验收天然需要超过上述预算，结论是“批次设计无效”，不是“允许延长执行”。

固定包装形式为：

```bash
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-0X \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
```

`verify-phase18-task.sh` 必须创建独立进程组、传递取消信号，并在内部 deadline 到达前完成受管清理。

### 2.2 成功证据复用

成功检查仅在相关产品代码、配置、依赖、候选元数据、验收脚本和执行环境均未变化时有效。
后续批次不得把旧 receipt 冒充当前候选证据；可以把它作为证据血缘输入，并通过变更影响清单决定
当前候选需要重跑的最小 shard。影响清单无法证明“不受影响”时必须重跑该 shard。

最终批次不因“Milestone 收口”自动重跑所有历史场景。若风险映射要求的当前候选 shard 总预算
超过 120 分钟，必须在开工前新增实施批次，不得把多个长任务串成一个后台进程。

## 3. 固定输入与参考环境

### 3.1 已知事实

- `VERSION=1.14.5` 是 Phase 18 基线；Phase-18-01 已完成 `2.0.1` 失败基线交付。
- 固定 seed 为 `18002005`，生成 5,000 用户、50,000 帖子、100,000 评论、200,000 点赞、
  200,000 关注和 25,000 收藏。
- Phase-18-01 的 18 次 HTTP 500、未收敛积压和第一瓶颈继续作为问题输入，不得包装为通过。
- 旧扩容探索发现 MySQL 空字段解析、Kafka member 列解析、顺序启动计时、任务提前耗尽和
  失败附件缺失；这些是新 Phase-18-02 的回归 fixture，不是产品失败结论。

### 3.2 参考宿主

| 项目 | 固定要求 |
| --- | --- |
| 宿主 | WSL2 Linux `amd64`，8 vCPU，12 GiB RAM，8 GiB swap |
| 工作区 | WSL2 Linux 文件系统 |
| 存储 | SSD/NVMe，可用空间不少于 80 GiB |
| 运行时 | Docker Engine/Server 与 Compose v2，记录精确版本 |
| 负载器 | 产品 Compose 外运行，记录 CPU/RSS，不得成为首个瓶颈 |
| Swap | 测量窗口增量不超过 256 MiB |

预检不满足时必须在 10 分钟内失败，不得进入场景。

## 4. 权威批次、版本与分支

| 批次 | 目标版本 | 分支 | 交付主题 | 主运行器预算 |
| --- | --- | --- | --- | ---: |
| Phase-18-01 | `2.0.1` | `develop/2.0.1` | 数据配方、负载工具与失败基线 | 历史批次，不重跑 |
| Phase-18-02 | `2.0.2` | `develop/2.0.2` | 限时验收运行器、分片与证据协议 | 60 分钟 |
| Phase-18-03 | `2.0.3` | `develop/2.0.3` | 多副本拓扑与所有权正确性 | 90 分钟 |
| Phase-18-04 | `2.0.4` | `develop/2.0.4` | 容量测量与瓶颈定界 | 90 分钟 |
| Phase-18-05 | `2.0.5` | `develop/2.0.5` | 双 Elasticsearch 隔离与迁移 | 90 分钟 |
| Phase-18-06 | `2.0.6` | `develop/2.0.6` | 全链路有界背压 | 90 分钟 |
| Phase-18-07 | `2.0.7` | `develop/2.0.7` | 独立健康诊断与 canary | 75 分钟 |
| Phase-18-08 | `2.0.8` | `develop/2.0.8` | 合同单一来源与漂移门禁 | 60 分钟 |
| Phase-18-09 | `2.0.9` | `develop/2.0.9` | 限时集成恢复与 Milestone 5 收口 | 90 分钟 |

本表是批次、版本和分支的唯一权威映射。每批从前一批合入后的最新 `upstream/main` 开工，
使用 `scripts/start-development-batch.sh Phase-18-0X --remote upstream`。

2026-09-26 重划时，本地 `develop/2.0.2` 已包含被废止方案的探索实现并运行过旧 qualification。
它不是本版 Phase-18-02 的合法分支。重新开工前必须停止旧运行、记录其失败/取消事实，并在用户知情下
将该分支原样归档为非开发分支；若已推送则不得静默改名。之后从本版方案合入后的最新
`upstream/main` 创建新的 `develop/2.0.2`。旧提交可逐项审计后移植，但旧 candidate、work directory、
receipt、执行次数和 `VERSION` 状态均不得复用。

## 5. 容量、拓扑与 SLO 合同

### 5.1 有界容量窗口

每轮固定为 1 分钟预热、3 分钟 150 RPS 稳态、1 分钟 300 RPS 突发和最长 3 分钟恢复。
三轮使用同一候选与输入，总有效窗口 24 分钟；连同快照恢复、证据和清理必须处于 Phase-18-04
的 90 分钟主运行器预算内。

- 稳态读请求 P95/P99 不高于 500 ms/1.5 s，写请求不高于 800 ms/2 s。
- 稳态非预期错误、超时和连接失败总和不高于 1%。
- 突发最多 5% 使用固定 `server_overloaded`/`Retry-After` 显式拒绝；不允许挂死、OOM 或伪成功。
- 搜索/通知 P99 在 30 秒内收敛，Metrics/Logs/Events P99 在 60 秒内可查。
- 三轮 RPS 偏差不超过 ±5%，P95/P99 偏差不超过 ±10%。

三分钟稳态在 150 RPS 下产生约 27,000 个请求，足以形成当前参考宿主的统计窗口；本阶段不再用
15 分钟窗口换取额外等待。若三分钟内结果不稳定，应判定系统或测量不稳定，不延长窗口筛选通过。

### 5.2 多副本与所有权

| 组件 | 拓扑 | 合同 |
| --- | ---: | --- |
| Frontend edge | 1 | 唯一外部入口，动态分发 Backend |
| Backend | 3 | HTTP 无状态；Outbox/alert 使用 lease 与 fencing |
| Business Worker | 2 | RabbitMQ competing consumers、手动 ack、幂等副作用 |
| Search Indexer | 2 | competing consumers、确定性 document ID、写入后 ack |
| Router | 2 | broker ack 后返回成功 |
| Marshaller | 2 | 同 group、至少 4 partitions、store-before-commit |
| Monitor | 1 | 第二所有者必须在启动插件前安全退出 |

Phase-18-03 只证明拓扑、所有权和逐实例替换正确性；Phase-18-04 才测量容量。每个组件场景独立
且不超过 20 分钟，不允许一个大矩阵失败后继续运行数小时。

Phase-18-04 对 Worker、Indexer、Router、Marshaller 使用同候选、同输入、同 measurement window
的单/多副本成对测量，默认门槛为 `>=1.3x`。Backend 在固定宿主无 CPU 隔离，饱和比值只记录，
以三副本流量分发、150/300 RPS 和替换持续服务为阻断条件。共享依赖 ceiling 使门槛无效的证据
必须先回到 `update` 修订，不得在看到结果后降标。

## 6. 存储、背压、诊断和合同

- Phase-18-05 将业务搜索与 Logs/Events Elasticsearch 分离为不同服务、卷、账户、密码和资源边界；
  验证 clean、`1.14.5` 和直接前序迁移，备份 format v2 兼容 v1。
- Phase-18-06 为 HTTP、MySQL pool、Outbox、Rabbit main/retry/dead、Router/Kafka 和
  Marshaller/sink 设置硬上限、拒绝/积压语义、指标与有界恢复。
- Phase-18-07 的 `status/doctor/verify --canary --json` 直接观察 Docker、Probe、MySQL、RabbitMQ、
  Kafka、双 ES 和 VictoriaMetrics；stdout/receipt 不经可观测链路回传。
- Phase-18-08 以 `contracts/` 中 Envelope、日志、指标和 API error catalog 作为唯一手编辑来源，
  确定性生成 Go、TypeScript、JSON Schema、Marshaller vocabulary 和 Elasticsearch mapping。

每个故障、迁移、背压或 canary shard 都必须声明独立的 20 分钟以内预算和明确的失败 reason code。

## 7. 批次闭环

### Phase-18-01：历史失败基线

保留已完成的 recipe、负载、资源样本和 `capacity-failure.json`。不重跑旧三轮，不把失败基线转为通过。

### Phase-18-02：限时验收基础

交付 deadline/watchdog、场景分片、进度心跳、原子失败附件、强归属清理和 receipt 聚合协议。
只使用 fixture 与最小 live smoke 证明运行器，禁止执行五组件正式矩阵。

### Phase-18-03：多副本正确性

交付唯一 edge、固定多副本拓扑、Kafka 4 partitions、逐实例指标、五组件替换与 Monitor 单所有者。
容量比率不在本批判定。

### Phase-18-04：容量与瓶颈

交付三轮有界业务容量、四组件单/多副本对比、资源/依赖 ceiling 和瓶颈分类。任何测量不稳定或
超时都失败，不延长窗口。

### Phase-18-05：双 ES 与迁移

交付双故障域、凭据与卷隔离、`1.14.5`/直接前序迁移、format v1/v2 恢复和交叉故障验证。

### Phase-18-06：有界背压

交付七层容量上限、统一过载错误、无界资源负向门禁和压力解除后的有界恢复。

### Phase-18-07：独立健康

交付结构化 lifecycle 健康、独立 canary 和观测数据面同时不可用时仍可落盘的脱敏 receipt。

### Phase-18-08：合同收敛

交付四个 catalog、确定性生成器、生成物和全仓 `--check`/负向漂移门禁。

### Phase-18-09：限时集成恢复与收口

冻结 `2.0.9` 候选，生成变更影响清单，验证必须重跑的高风险 shard、直接前序升级、备份恢复、
完整产品/权限抽样和证据血缘。不得调用一个重跑全部历史场景的全量矩阵。

## 8. 阶段完成条件

1. Phase-18-02 至 18-09 每批都有 `duration_seconds < 7200` 的通过 receipt；没有被杀死后伪造完成的任务。
2. 五类组件的多副本、替换和所有权正确，Monitor 单所有者安全拒绝第二实例。
3. 固定短窗口容量满足 SLO 和重复性，四类消费者达到开工前冻结的相对扩容门槛。
4. 双 ES 迁移/恢复和交叉故障通过，已接受数据无静默丢失。
5. 七层背压有界、显式、可观察并在规定时间内恢复。
6. 独立诊断/canary 和合同生成/漂移门禁通过。
7. `2.0.9` 当前候选的风险映射、升级恢复、业务、权限和跨数据面抽样通过；所有复用证据均有
   未受影响证明，不混合冒充当前候选。
8. 九份同名实施记录齐全，根版本为 `2.0.9`，没有阻断问题。

任一门禁超时、连续两个新候选同原因失败、所需 shard 总预算超过 120 分钟，或证据影响范围不可判定，
都必须停止并修订方案。不得继续等待到 6 小时或 36 小时。

## 9. 固定收口入口

每批使用同一限时入口，具体 `--batch` 和 shard 清单由拆分实施方案冻结：

```bash
timeout --signal=TERM --kill-after=20s 100m \
  scripts/verify-phase18-task.sh \
  --batch Phase-18-0X \
  --manifest dist/release-manifest.json \
  --work "$GOPULSE_PHASE18_WORK" \
  --deadline-seconds 5400
python3 scripts/verify-phase18-evidence.py \
  --task "$GOPULSE_PHASE18_WORK/evidence/task.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.X --base-ref upstream/main
git diff --check
```

runner 与静态门禁必须分别记录开始、结束和耗时；聚合器拒绝 `duration_seconds >= 7200`、
缺失 heartbeat、超预算 shard、跨候选拼接、清理失败、Secret/私有路径泄漏或手工修改 receipt。

## 10. 实施记录

每批完成前创建 `dev/logs/Phase-18/` 下的同名 Markdown，只记录实际完成、实际修改、实际命令与结果、
各任务耗时、失败 reason code、偏差和后续项。不得把计划预算写成实际通过时间。
