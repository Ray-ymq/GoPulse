# Phase 18：高并发与可观测架构收敛总实施方案

> 规划基线（2026-09-20）：主远程 `upstream/main` 为
> `fe596514487649a59678200af1cfb3ca40b4b16f`，当前完成版本为 `1.14.5`。
> Phase 18 分配 `2.0.x`，`2.0.0` 为阶段基线，可执行批次从 `2.0.1` 开始。
>
> 规划修订（2026-09-24）：Phase-18-01 以“单副本失败基线已交付，重复性与恢复门禁未通过”
> 收口；`capacity-failure.json` 是 Phase-18-02 的正式输入，不是通过 evidence。严格
> 重复性、完整收敛和阶段 SLO 统一保留为 Phase-18-05 的最终门禁。

## 1. 阶段目标

GoPulse 保持“高并发业务系统 + 内建可观测系统”定位。本阶段不增加业务域，
而是在固定 Linux `amd64` Compose 资源上给出可重复的容量、水平扩容、背压、
故障隔离和恢复证据，并把公共数据合同收敛为可确定性生成的机器来源。

阶段完成时必须同时成立：

- 固定数据和混合负载在参考宿主上满足固定 SLO，三轮结果可重复。
- Backend、Business Worker、Search Indexer、Router 和 Marshaller 具有实测的多副本合同；
  Monitor 因本地插件状态保持单所有者。
- 业务搜索 Elasticsearch 与 Logs/Events Elasticsearch 分离服务、卷、账户、密码和资源边界，
  互相故障不拖垮对方数据面。
- HTTP、MySQL 连接池、Outbox、RabbitMQ、Kafka、Marshaller 和存储写入均有硬上限、
  拒绝或积压语义、指标和恢复门禁。
- 可观测数据面故障时，lifecycle 仍能输出可机器判定的基础健康和 canary 结果。
- Envelope、日志、指标和 API 错误合同可确定性生成，全仓漂移检查为零差异。

## 2. 范围与非目标

### 2.1 本阶段交付

- 确定性数据配方、混合负载工具、容量摘要 schema 和脱敏报告。
- 单副本、多副本、突发、超载、实例替换和故障后追赶的 Compose 验收。
- 两个 Elasticsearch 故障域以及 `1.14.5`/`2.0.2` 单 ES 数据向双 ES 的可验证迁移。
- 主要容量点的配置、安全错误码、指标、状态转换和恢复时限。
- lifecycle 的版本化 JSON 诊断与端到端 canary receipt。
- `contracts/` 机器合同源、生成器、生成物和 CI `--check` 门禁。
- 同一 `2.0.5` Linux `amd64` 候选的完整验收证据与 Milestone 5 收口。

### 2.2 本阶段不做

- Kubernetes、Helm、Ingress、Operator 或集群编排对象。
- MySQL、RabbitMQ、Kafka、Elasticsearch 或 VictoriaMetrics 的生产级 HA。
- 新业务、新插件、新页面、多租户或外部告警渠道。
- 无本阶段数据支持的分库分表、读副本、新消息中间件或新存储。
- 将单机 WSL2/Compose 结果宣称为通用生产容量、集群扩容或高可用承诺。

## 3. 固定输入与参考环境

### 3.1 代码基线事实

- `VERSION=1.14.5`；Phase 17 已完成 runtime contract、Probe、信号关停、Migration 和消息可靠性收口。
- 当前 Compose 只有一个 Elasticsearch，Kafka topic 只有 1 partition，Backend 的宿主端口使基础 Compose 不能直接 scale。
- Backend Outbox 和告警调度已有 MySQL lease，RabbitMQ consumer 已手动 ack，Marshaller 已有 Kafka partition fencing。
- Monitor 对插件卷使用文件所有权 lease，因此本阶段明确保持单副本，不引入分布式插件协调。
- Frontend Nginx 已使用 Docker DNS 动态解析 Backend，可作为唯一 edge 进行多 Backend 负载分发。

### 3.2 参考宿主合同

| 项目 | 固定要求 |
| --- | --- |
| 宿主 | WSL2 Linux `amd64`，8 vCPU，12 GiB RAM，8 GiB swap |
| 工作区 | WSL2 Linux 文件系统，不使用 Windows 挂载目录 |
| 存储 | SSD/NVMe 后端，验收前可用空间不少于 100 GiB |
| 运行时 | 真实 Linux `amd64` Docker Engine/Server 与 Compose v2，记录精确版本 |
| 负载器 | 与产品同宿主、在产品 Compose 外运行；记录 CPU/RSS，不得成为首个瓶颈 |
| Swap | 仅作安全缓冲；测量窗口内使用增量不得超过 256 MiB |
| 证据 | CPU/内存/磁盘/WSL kernel/Docker/Compose、候选 digest、执行窗口和资源摘要 |

预检不满足上述条件时阻断容量门禁。允许调整 WSL `.wslconfig` 后重试，
不得通过降低负载、SLO 或数据规模宣称通过。

## 4. 权威批次、版本与分支

| 批次 | 目标版本 | 分支 | 交付主题 | 状态 |
| --- | --- | --- | --- | --- |
| Phase-18-01 | `2.0.1` | `develop/2.0.1` | 高并发数据配方、负载工具与失败基线 | 失败基线已交付（未通过 SLO） |
| Phase-18-02 | `2.0.2` | `develop/2.0.2` | 多副本扩容、负载分发与任务所有权 | 待实施 |
| Phase-18-03 | `2.0.3` | `develop/2.0.3` | 双 Elasticsearch 隔离与端到端背压 | 待实施 |
| Phase-18-04 | `2.0.4` | `develop/2.0.4` | 独立健康通道与合同单一来源 | 待实施 |
| Phase-18-05 | `2.0.5` | `develop/2.0.5` | 高并发、故障恢复与 Milestone 5 收口 | 待实施 |

每批必须从前一批合入后的最新 `upstream/main` 开工，使用
`scripts/start-development-batch.sh Phase-18-0X --remote upstream`。本表是批次、版本和分支唯一权威映射。

## 5. 数据、负载与 SLO

### 5.1 确定性数据配方

使用固定 seed `18002005`生成 5,000 用户、50,000 帖子、100,000 评论、200,000 点赞、
200,000 关注、25,000 收藏及由这些事实确定产生的通知和 Outbox。生成器必须：

- 只写入当前 Schema 允许的业务事实，生成后通过正式 reindex 和消费链路收敛投影。
- 相同 seed 得到相同计数、ID 边界和非敏感摘要 digest；重复执行要么在空 project 成功，要么安全拒绝。
- 生成、索引、通知和可观测预热不计入正式测量窗口。

### 5.2 混合业务负载

| 类别 | 比例 | 代表操作 |
| --- | ---: | --- |
| 列表与详情读 | 45% | 帖子列表/详情、Following feed、评论、用户资料 |
| 搜索 | 10% | 帖子和用户搜索 |
| 通知与收藏读 | 10% | 通知、收藏和关系列表 |
| 内容写入 | 15% | 发帖、评论、编辑和代表性删除 |
| 交互写入 | 15% | 点赞/取消、关注/取消、收藏/取消 |
| 身份与会话 | 5% | 登录、当前用户与会话刷新 |

负载器预先建立固定用户池和会话，不把密码哈希成本偷换为全部业务吞吐。

### 5.3 测量窗口与门禁

| 窗口 | 要求 |
| --- | --- |
| 预热 | 5 分钟，逐步到 150 RPS，不计入 SLO |
| 稳态 | 150 RPS，持续 15 分钟 |
| 突发 | 300 RPS，持续 2 分钟 |
| 恢复 | 负载回落后最长 10 分钟，要求本轮积压清空并收敛 |

稳态读请求 P95 不高于 500 ms、P99 不高于 1.5 s；写请求 P95 不高于 800 ms、
P99 不高于 2 s；非预期错误、超时和连接失败总和不高于 1%。突发窗口允许最多 5% 使用
固定 `server_overloaded`/`Retry-After` 的显式拒绝，不允许连接挂死、OOM 或伪成功。

已接受业务的搜索与通知 P99 在 30 秒内收敛，Metrics/Logs/Events P99 在 60 秒内可查。
相同依赖、数据、负载和单副本资源上限下，多副本的可持续吞吐或积压排空速率至少为单副本的 1.3 倍。

同一候选连续三轮稳态的 RPS 偏差不得超过 ±5%，P95/P99 偏差不得超过 ±10%。

Phase-18-01 的单副本失败基线和 Phase-18-02 的扩容对比只负责暴露并量化问题。上述重复性、
端到端收敛和完整 SLO 不在前两者中以放宽或挑选成功轮次的方式收口，最终仍须由
Phase-18-05 在同一 `2.0.5` 候选上重新验证。扩容对比必须使用 Phase-18-02 内同候选、
同 corpus、同负载版本和同配置生成的单副本/多副本成对测量；旧三轮中的低延迟轮不得充当分母。

## 6. 多副本与所有权合同

| 组件 | 最终拓扑 | 所有权/扩容语义 |
| --- | ---: | --- |
| Frontend edge | 1 | 唯一外部入口，Docker DNS 动态分发 Backend |
| Backend | 3 | HTTP 无状态；Outbox 与 alert scheduler 使用 MySQL lease/fencing |
| Business Worker | 2 | RabbitMQ competing consumers，手动 ack，幂等副作用 |
| Search Indexer | 2 | RabbitMQ competing consumers，确定性 document ID，写入后 ack |
| Router | 2 | 无本地状态，Kafka broker ack 后才返回成功 |
| Marshaller | 2 | 同 consumer group，至少 4 partitions，store-before-commit 与 lease fencing |
| Monitor | 1 | 插件卷唯一所有者；第二副本必须安全拒绝，不抢占运行插件 |

基础 Compose 不再将 Backend 端口发布到宿主；仅唯一 Frontend edge 对外。调试/验收如需直达，
使用强归属、loopback-only 的临时 override，不回退产品入口边界。

## 7. 双 Elasticsearch 与迁移合同

- 业务搜索服务使用 `SEARCH_ELASTICSEARCH_URL/USERNAME/PASSWORD`，只承载可由 MySQL 重建的业务索引。
- 观测服务使用 `OBSERVABILITY_ELASTICSEARCH_URL/USERNAME/PASSWORD`，只承载 Logs/Events template、alias 和日索引。
- 两者使用不同 Compose service、volume、账户、密码、健康检查和内存/CPU 边界；不接受共享 URL 或相同凭据。
- `ELASTICSEARCH_URL` 等单 ES 键不作为静默别名；升级必须经 lifecycle 显式生成新配置并校验凭据分离。
- `1.14.5` 和 `2.0.2` 单 ES 升级时，保留原卷作为业务搜索输入，将 Logs/Events 模板、alias 和文档复制到观测 ES；
  按 index count、mapping、alias 和抽样 ID 验证后才切换读写。中断可幂等重试，不在未验证时删除旧数据。
- 备份格式升级为 v2，分别记录两个 ES 数据集；restore 兼容 Phase 16/17 format v1，并执行同一拆分验证。
- 单实例 Elasticsearch Exporter 继续监测业务搜索 ES；lifecycle 独立诊断直接检查两个 ES，不新增插件实例。

## 8. 端到端背压合同

- Frontend/Backend 限制连接、request body、在途请求和每路由超时；饱和时返回 `503`、
  `{"error":{"code":"server_overloaded",...}}` 和 `Retry-After: 1`。
- MySQL 设置显式 max-open/max-idle/lifetime/wait timeout，并输出使用数、等待数和等待时间。
- Outbox 以可配置高/低水位停止接受会产生新异步事件的写入，超出量由 HTTP 并发上限和单请求事件上限绑定。
- RabbitMQ 主/retry/dead queue 设置消息数与字节上限，`reject-publish` 与 publisher confirm 使拒绝回到 Outbox/requeue，
  不使用丢旧消息的溢出策略。
- Router 只在 Kafka ack 后返回 `202`；有界 records/bytes 缓冲满时显式拒绝。Kafka topic 使用固定 retention/segment 与磁盘预检。
- Marshaller 每 partition 一条在途记录，只在 sink 写入成功后 commit；重试退避、连接数、request body 和请求超时有上限。
- 日志/事件在 Router 接受前的本地有界丢弃必须记录独立 counter 和状态变化；Router 已接受的数据不得静默丢弃。

所有上限进入 runtime contract、`.env.example` 和验证器；不允许只在验收脚本中硬编码另一套参数。

## 9. 独立健康与合同源

### 9.1 lifecycle 诊断

`status --json`、`doctor --json` 和 `verify --canary --json` 输出同一版本化结构，至少包含
`schema_version`、candidate identity、timestamp、overall status，以及每项检查的 `id/status/reason/facts`。

- 诊断直接使用 Docker inventory、组件 Probe、RabbitMQ queue、Kafka group lag、MySQL、两个 ES 和 VM 的限时读操作。
- canary 生成唯一 ID，从正式入口注入业务与可观测记录，直接查询目标事实并将 receipt 原子写入指定路径。
- stdout/receipt 是基础健康证据，不经 Kafka、VM 或观测 ES 回传；任一数据面不可用时仍输出结构化失败而非无结果退出。
- 输出只包含 allowlist 事实和安全 reason code，不包含凭据、DSN、payload 或宿主私有路径。

### 9.2 机器合同源

`contracts/` 按 Envelope、日志、指标和 API errors 保存四个版本化 JSON catalog 及 schema，这些 catalog 是各自领域的唯一手编辑来源。

确定性生成器输出：

- Go 端的 type、枚举、验证表和指标目录。
- 两个 Frontend 使用的 TypeScript error code、允许字段和目录。
- Envelope/log/event JSON Schema、Marshaller vocabulary 与 Logs/Events Elasticsearch template/mapping。
- 格式化、排序和文件头固定的生成物，重复生成字节一致。

`generate` 更新生成物，`--check` 在临时目录重生成并与仓库比较。未知字段、未知错误码、
mapping 不一致或非确定输出必须失败，不允许运行时宽松回退。

## 10. 批次闭环与完成条件

### Phase-18-01：容量基线

交付数据生成器、仓库原生 Go 负载器、资源取样、报告 schema 和单副本三轮基线。
完成条件（2026-09-24 修订）是：

- recipe/负载工具自测、候选/corpus/负载版本绑定和逐样本原始资源落盘门禁通过；
- 在固定条件下只执行一次正式三轮，不为筛选通过轮次重跑；若重复性或恢复门禁失败，停止并保留
  `capacity-failure.json`、原始资源样本和逐窗口诊断，不生成伪 `capacity.json`；
- 报告如实记录 RPS、延迟、HTTP 错误、Outbox/通知/搜索未收敛事实和第一瓶颈，并完成受管资源清理。

失败基线满足上述条件即完成 Phase-18-01 的交付闭环，但明确不代表重复性、恢复或 SLO 通过。
第三轮 18 次 500 已保留 request-id 线索；只有后续复现或扩大时另开修复任务，不能借扩容验收掩盖。

### Phase-18-02：多副本与所有权

交付 scale overlay、唯一 edge 负载分发、Kafka 4 partitions、多副本状态指标和替换验收。
完成条件是表 6 拓扑成立，每个组件有水平扩容或单所有者结论，1.3 倍相对扩容由本批成对测量证明，
替换无丢失或重复副作用，并完成 Outbox 排空及所有权验证。旧 Phase-18-01 单副本轮次只作为问题
输入，不参与扩容分母。重复性、完整收敛和端到端 SLO 不在本批收口。

### Phase-18-03：存储隔离与背压

交付双 ES、配置/凭据分离、单 ES 升级、backup format v2 与全链路容量上限。
完成条件是交叉故障不扩散，迁移/恢复不丢失历史，有界背压与单层恢复无 OOM、无界增长或伪成功。
完整 150/300 RPS SLO、重复性和跨系统最终收敛留给 Phase-18-05。

### Phase-18-04：独立健康与合同收敛

交付 lifecycle JSON 诊断/canary、`contracts/` 和生成/漂移门禁。
完成条件是观测面故障时诊断仍可判定，canary 可发现不收敛，所有生成物重生成零差异。

### Phase-18-05：候选验收与收口

冻结一个 `2.0.5` manifest、Bundle、runtime contract、contract catalog digest 和全部 image/plugin digest，
运行稳态、突发、多副本替换、超载、双 ES 故障、追赶、直接前序迁移和备份恢复。
完成条件是全部 receipt 绑定同一候选，报告明确容量、第一瓶颈、单点和适用边界，`VERSION=2.0.5`。

## 11. 阶段验收与停止条件

1. Phase-18-01 的失败基线、原始样本和候选/corpus/负载绑定可复核；Phase-18-02 使用本批成对测量，
   不以旧单副本低延迟轮作为 1.3 倍扩容分母。
2. 五类可扩展组件通过多副本、替换和 1.3 倍相对扩容门禁，Monitor 单所有者安全拒绝第二实例。
3. Outbox 排空与所有权、Rabbit/Kafka/alert 无越权提交、重复持久副作用或静默丢失已接受数据。
4. 双 ES 故障域、单 ES 迁移、format v1/v2 恢复和恢复后新写入通过。
5. 全链路背压在超容量时有界且可恢复，无 OOM、无无界队列/重试/日志增长。
6. 独立诊断、canary、合同生成和全仓漂移门禁通过。
7. 业务、搜索、Metrics/Logs/Events、告警、六插件、双 Frontend、权限、生命周期和备份恢复回归通过。
8. 五个批次的同名实施记录齐全，根版本为 `2.0.5`，没有阻断问题。

Phase-18-01 至 Phase-18-04 只交付各自边界内的可复核中间结果；最终重复性、收敛和完整 SLO
仍由第 1 项规定的 Phase-18-05 候选矩阵统一判定。

全部条件达到后结束 Milestone 5。报告必须显式限定于本文记录的 WSL2/Linux `amd64`、
8 vCPU/12 GiB 和负载配方，不继续无证据调优，也不以 Kubernetes 替代本阶段合同。

## 12. 固定收口命令与 evidence

各批先运行直接受影响的最小测试，再运行对应固定门禁。最终候选的权威入口为：

```bash
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-phase18.sh --manifest dist/release-manifest.json --work "$GOPULSE_PHASE18_WORK" --evidence "$GOPULSE_PHASE18_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase18-evidence.py --linux "$GOPULSE_PHASE18_WORK/evidence/linux-amd64.json"
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.0.5 --base-ref upstream/main
git diff --check
```

最终 evidence 至少包含：schema/status，候选 manifest/Bundle/runtime/contract/image/plugin digest，
宿主与资源配额，数据 recipe/seed/digest，三轮负载摘要，多副本与所有权，双 ES，背压，
诊断/canary，合同漂移，完整产品和备份恢复场景。聚合器拒绝混合候选、缺失/失败场景、
未满足宿主配额、原始凭据/路径/payload 命中、未清理受管资源或手工编辑的 receipt。

## 13. 实施记录规则

每批完成前创建 `dev/logs/Phase-18/` 下的同名 Markdown，只记录实际完成工作、实际改动、
实际命令与结果、偏差、失败轮次、已知限制和后续项。容量报告仅保存脱敏摘要与 checksum，
原始时序样本、凭据和运行日志留在私有 work directory。
