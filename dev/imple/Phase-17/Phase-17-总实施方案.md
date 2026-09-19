# Phase 17：稳定性与工程化总实施方案

> 规划更新（2026-09-15）：已获取主远程 `origin/main`，基线提交 `3e13a2b`，根完成版本为 `1.14.1`，第一轮管理中心展示更新已合入。按用户要求再次临时插入前端批次，分配 `develop/1.14.2`；本次只调整计划并创建分支，不修改已完成产品版本。

## 1. 阶段目标

Phase 17 先以两个临时插入批次完成管理中心前端展示更新，再在 Phase 16 已交付的完整 Linux `amd64` Compose 产品上统一运行时和有状态处理契约，并以同一个最终候选完成 Milestone 4 验收：

```text
Phase 16 immutable bundle + current data/recovery contract
                           │
                           ▼
       admin frontend presentation update (17-01 then 17-02)
                           │
                           ▼
        runtime contract and diagnosability closure
                           │
                           ▼
       migration and state-processing reliability closure
                           │
                           ▼
      one Linux amd64 Compose resilience acceptance set
                           │
                           ▼
             Milestone 4 + future deployment handoff
```

阶段完成时必须达到：

- 独立 Admin Frontend 的共享壳层、管理大屏、Metrics、Logs 和 Exporter 页面按 `dev/imple/Phase-17/assets/Management_Center/` 第一组四张视觉基准完成首轮更新，再按 `dev/imple/Phase-17/assets/Management_Center/2/` 五张视觉基准完成含 Alerts 的二次真实数据驱动展示更新，并保持响应式、可访问性和权限边界。
- Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor 和六类 Exporter 使用一致、可机器校验的配置、启动、存活、就绪、关停和安全诊断语义。
- 对外及内部 HTTP 请求具有稳定 Request ID、结构化日志和统一错误包络；错误、日志、状态与证据不泄漏凭据、内部地址、服务器路径或原始异常。
- MySQL Schema 由单一 Migration 流程管理，空库和直接前序 `1.13.6` 当前数据均能安全推进，dirty、未来版本和并发迁移安全失败。
- RabbitMQ 与 Kafka 的成功、暂态失败、永久异常、重平衡/重连和关停中在途处理具有明确且被真实验证的 ack/offset/重试/幂等语义。
- 告警评估在重启、依赖短暂故障和重复调度下恢复，不产生重复活动事件或破坏角色与审计事实。
- 完整业务、搜索、插件、Metrics/Logs/Events、告警、统一登录和两个 Frontend 在不依赖 Kubernetes 的条件下共同通过产品级韧性验收。

只补单个组件的单元测试、只增加 `/health`、只运行 Happy Path，或依靠 Compose/Kubernetes 重启掩盖状态处理缺陷，均不构成本阶段完成。

## 2. 范围与非目标

### 2.1 本阶段交付

- 一套对齐 `dev/imple/Phase-17/assets/Management_Center/2/` 视觉基准的 Admin Frontend 管理壳层，以及管理大屏、Metrics、Logs、Alerts、Exporter 五页的真实数据展示更新。
- 一份版本化运行时合同及其校验器，覆盖长运行 Go 组件的配置项归属、敏感性、Probe 路径、硬/软依赖、退出预算和兼容别名。
- `startup`、`live`、`ready` 三类 Probe，以及 Phase 16 `/health` 兼容入口；Worker/Indexer 使用既有私有组件端口，不新增宿主暴露。
- 一套共享关停顺序：先撤销就绪，再停止接收新工作，排空在途请求/消息，限时刷新日志与事件，最后关闭连接和监听器。
- 统一 HTTP Request ID、错误包络、结构化日志字段、安全原因码和前端错误映射。
- 明确的 Migration `status/validate/up` 工作流，以及空库、直接前序版本、dirty、future 和并发执行门禁。
- RabbitMQ、Kafka 与三源告警的代表性可靠性场景和指标/状态证据。
- 一个真实 Linux `amd64` 最终候选、脱敏 evidence 集合、Milestone 4 验收记录和未来部署复用说明。

### 2.2 明确不做

- Kubernetes 资源、Helm、Ingress、Service DNS、PVC、Probe YAML 或集群对象采集；这些仅保留为未排期的未来设计。
- macOS、Windows、`linux/arm64` 产品支持或运行矩阵；Phase 16 的 Linux `amd64` 支持边界保持不变。
- 新业务、新插件、新告警来源、新管理页面或角色模型扩展；视觉更新只覆盖既有 Admin Frontend 壳层和页面，不扩展为普通用户 Frontend 重设计。
- 生产级高可用、容量认证、压测指标承诺、完整 SRE、跨地域容灾或零停机 Schema 迁移框架。
- 任意历史版本升级矩阵。Migration 兼容只覆盖空库和直接前序 `1.13.6` 当前产品数据，不据此承诺更早版本。
- 将冻结的 `scripts/*.ps1` 扩展到当前产品能力，或把 GoPulse/基础设施改造成宿主原生服务。
- 无具体失败依据的依赖审计、覆盖率活动、一般性代码审查和机会性重构。

## 3. 输入基线、环境与开工约束

### 3.1 固定输入

- `dev/imple/Phase-17/assets/Management_Center/管理大屏.png`、`Metrics.png`、`Logs 页面.png`、`Exporter 管理.png` 四张 `1440 × 1000` 视觉基准；只作为展示合同，不作为运行时页面背景或静态业务数据。
- `1.13.6` Linux `amd64` release manifest、版本化 Bundle、生命周期镜像、9 个产品镜像、6 个 current plugin 和第三方制品 digest。
- Phase 16 的唯一 edge、双 Frontend、统一登录、六插件、三源告警、backup format v1、当前数据配方及 A/B/C 同 manifest 恢复合同。
- `dev/logs/Phase-16/Phase-16-06-*` 中真实 Linux `amd64` evidence；历史结果只作为输入基线，不能冒充 Phase 17 候选通过证据。
- 当前代码已有的 typed config、`componentmetrics` 私有监听器/共享退出预算、Backend Request ID/错误包络、RabbitMQ 手动 ack 与 retry/dead 路径、Kafka 手动 commit/partition ownership 和告警 lease 状态机优先复用；本阶段收口差异，不平行重写。

### 3.2 支持与验收环境

| 项目 | 固定要求 |
| --- | --- |
| 宿主/Server | 一个真实 Linux `amd64` Docker host/server；WSL2/Linux 或原生 Linux均可 |
| 工作区 | Linux 文件系统中的 checkout；不得以 Windows 挂载目录作为受支持验收工作区 |
| 产品入口 | 从独立解压的版本化 Bundle 使用共享生命周期和唯一 edge |
| 制品 | 候选 image/plugin 均按同一 release manifest digest 拉取，不在验收目录构建源码 |
| 数据 | 独立空 project、`1.13.6` 当前数据迁移 project、故障场景 project；名称与 token 随机隔离 |
| 证据 | 记录 host/server、候选、运行时合同 digest、场景结果和脱敏日志引用，不记录 Secret |

开发中的最小包测试可在当前开发环境运行；涉及信号、真实 broker、Migration、Bundle 或完整产品的批次门禁必须在上述 Linux `amd64` 环境运行。

### 3.3 分支与实施开工

- 每批从前一批合入后的最新 `upstream/main` 开始，运行 `scripts/start-development-batch.sh Phase-17-0X --remote upstream` 创建表 4 中的分支并同步版本元数据。
- 若本地或远程已有同名分支，先核对归属与基线，不静默覆盖、重命名或重建。
- 每批只实现其拆分方案；固定门禁通过后更新同名实施记录和 `VERSION`，提交并停止。
- 规划工作留在 `update`，不提前创建开发分支、不修改产品版本。

第二轮追加输入为 `dev/imple/Phase-17/assets/Management_Center/2/` 五张 PNG，原始来源为用户工作区 `Management_Center/2/`，对应关系见 Phase-17-02。

## 4. 权威批次、版本与分支分配

Phase 17 对应 `1.14.x`，patch `0` 为阶段基线。第一轮展示更新已完成，用户再次要求插入管理中心前端更新。本阶段调整为四个实现批次和一个集成收口批次；原运行时、持久状态、最终验收三个批次的开发分支均未创建，依次后移。已推送的 `develop/1.14.1` 不改名、不重编号：

| 批次 | 目标版本 | 分支 | 交付主题 | 状态 |
| --- | --- | --- | --- | --- |
| Phase-17-01 | `1.14.1` | `develop/1.14.1` | 管理中心前端展示更新 | 已完成并合入 main |
| Phase-17-02 | `1.14.2` | `develop/1.14.2` | 管理中心前端二次展示更新 | 已完成实现及批次验收，待合入 main |
| Phase-17-03 | `1.14.3` | `develop/1.14.3` | 统一运行时契约与服务可诊断闭环 | 待实施 |
| Phase-17-04 | `1.14.4` | `develop/1.14.4` | Migration 与消息处理可靠性闭环 | 待实施 |
| Phase-17-05 | `1.14.5` | `develop/1.14.5` | 完整 Compose 韧性验收与 Milestone 4 收口 | 待实施 |

本表是 Phase 17 唯一权威的批次到版本、分支映射。批次顺序或数量在任何分支创建前变化时，必须先更新本表并重算未创建分支；已推送分支不得静默改名或重编号。

## 5. 拆分理由、顺序与依赖

```text
17-01 admin frontend layout/dashboard/metrics/logs/exporter
                    │
                    ▼
17-02 admin frontend second update (five references, including alerts)
                    │
                    ▼
17-03 runtime/config/probe/log/error contract
                    │
                    ▼
17-04 migration + Rabbit/Kafka/alert state safety
                    │
                    ▼
17-05 one-candidate Compose resilience acceptance
```

1. 管理中心展示先独立收口，使后续运行时错误映射、状态展示和最终候选验收基于稳定的 Admin Frontend 壳层，不在工程化批次中混入大范围视觉改造。
2. Probe、日志和关停必须先形成稳定公共合同，有状态处理批次才能用统一状态与日志证明故障和恢复结果。
3. Migration、RabbitMQ、Kafka 和告警均涉及“何时提交状态、失败后是否重放、重复执行是否安全”，放在一个批次形成数据正确性闭环，而不按中间件机械拆分。
4. 最终批次只消费前四批合同并运行跨批矩阵，避免在收口时首次设计接口或加入功能。
5. 五批包含两次经用户确认的临时前端插入，不再额外安排默认 Review、覆盖率或技术清单式批次。

## 6. 权威运行时合同

### 6.1 合同载体

Phase-17-03 应增加机器可读的运行时合同（建议路径 `deploy/runtime-contracts.json`）和对应 schema/校验器，至少记录：

- contract version、组件 ID、镜像逻辑名和运行模式；
- 每个组件拥有的环境变量、类型、必填/默认、是否敏感、兼容别名及废弃期限；
- HTTP/私有监听地址、`startup/live/ready/health` 路径、认证与暴露边界；
- readiness 的硬依赖、只影响状态的软依赖、单次检查超时和总体超时；
- shutdown timeout、Compose `stop_grace_period`、退出顺序和正常/失败退出码；
- 组件版本、Git revision、Schema 目标版本和消息合同版本的安全可观测字段。

校验器必须把合同与 `.env.example`、Compose 环境、Docker healthcheck、插件 manifest 及代码侧固定目录对照；允许实现期选择等价文件名，但不得只写人读文档而缺少机器校验。

### 6.2 配置语义

- 继续使用环境变量作为 Bundle/Compose 的配置输入，不新增另一套配置文件覆盖层。
- 已存在且无冲突的变量保持名称和语义；确需统一命名时只保留一个 canonical key，兼容别名只允许在本阶段明确记录的期限内保留，canonical 与 alias 同时出现且值不同必须启动失败。
- 每个进程只解析自己拥有的键，使用强类型、范围、URL/host、runtime mode 和交叉字段校验；无效配置必须在接收请求/消息前失败。
- Secret 只能通过 Phase 16 的私有配置/secret 路径进入。错误、日志、Probe、`status`、诊断和 evidence 只能输出键名或安全原因码，不输出值、DSN、userinfo、token hash 或可反推材料。
- 有效配置摘要只记录合同版本、非敏感枚举/范围、Secret 是否已提供和来源类别，不记录 Secret 内容。

## 7. 生命周期与 Probe 合同

### 7.1 通用 HTTP 语义

所有长运行 Go 组件提供：

| 路径 | 成功条件 | 禁止行为 |
| --- | --- | --- |
| `GET /startup` | 配置校验、必要本地初始化和监听器绑定完成 | 不因后续依赖短暂故障回退为未启动 |
| `GET /live` | 进程主循环可响应且未进入不可恢复终态 | 不执行数据库、broker、搜索或指标存储 I/O |
| `GET /ready` | 组件可接受其主要工作负载，且关停尚未开始 | 不把明确的软依赖故障升级为全产品不可用 |
| `GET /health` | Phase 16 兼容别名，语义等同 `/live` | 不继续承载 readiness 语义 |

四个入口只允许无 query/body 的 GET，返回稳定 JSON、`Cache-Control: no-store` 和正确 content type；失败只给组件 ID、状态和固定 reason code，不给地址、凭据或原始错误。Worker/Indexer 复用既有私有组件监听边界，Exporter 保持插件进程内端口；不得为 Probe 新增宿主端口。

### 7.2 Readiness 硬/软依赖矩阵

| 组件 | readiness 硬依赖 | 软依赖/降级方式 |
| --- | --- | --- |
| Backend | MySQL 可用、Schema 为当前目标、引导管理员约束有效 | Redis、RabbitMQ、Elasticsearch、VictoriaMetrics、Monitor 分别使缓存/异步/搜索/管理来源降级，不阻断代表性社交请求 |
| Business Worker | MySQL 可用且 RabbitMQ consumer session 已建立 | 日志/指标投递失败限界缓冲，不改变业务消息确认 |
| Search Indexer | MySQL、Elasticsearch 和 RabbitMQ consumer session | 日志/指标投递失败不改变索引消息确认 |
| Router | Kafka broker、目标 topic 与 producer 可用 | 指标采集失败不接管消息结果 |
| Marshaller | Kafka group/topic、VictoriaMetrics、Logs/Events Elasticsearch 写入端 | 单个观测来源无新数据不等于进程失活 |
| Monitor | 插件状态目录/catalog 可用、Router 发布路径可恢复 | 单个或多个 Exporter/source 故障只标记对应来源 degraded |
| 六类 Exporter | 配置、监听器和 collector 主循环已初始化 | 被采集源不可用时仍 ready 并暴露 `up=0`/安全状态，避免将来源故障误判为 Exporter 崩溃 |

具体依赖检查必须限时、限并发并避免每次 Probe 产生无界 goroutine。Compose 用 `/ready` 表达可接收工作，用 `/live`/进程退出表达需要重启；未来编排系统可直接映射三类 Probe，而不修改产品语义。

### 7.3 优雅退出

收到 SIGTERM/SIGINT 后统一执行：

1. 原子切换为 `stopping`，`/ready` 立即返回 `503`。
2. 停止接收新 HTTP 请求、Rabbit delivery、Kafka fetch、采集轮次、告警轮次和插件操作。
3. 在单一共享 deadline 内等待已接收的请求/消息完成；不能完成的 Rabbit 消息 requeue，Kafka 不越权提交 offset。
4. 限时刷新日志/事件缓冲并关闭插件子进程、producer/consumer、存储连接和 HTTP/私有监听器。
5. 正常信号关停成功返回 `0`；超时、监听器异常或不可恢复 consumer 错误返回非零，使现有 Compose restart policy 可见地接管。

各子系统共享同一个总预算，不能串行消费多个完整 timeout；Compose `stop_grace_period` 必须大于应用总预算并保留固定安全余量。日志必须明确 `stopping`、`shutdown_complete` 或安全失败原因，不能记录原始异常或伪报完成。

## 8. Request ID、日志与错误合同

### 8.1 Request ID

- 唯一 edge 为外部请求生成/替换 32 位小写十六进制 Request ID；Go 服务只接受符合合同的内部传播值，缺失时自行生成，畸形或多值输入不得进入日志关联键。
- 同一 ID 写入 `X-Request-ID` 响应头、Backend/Monitor/Router/Marshaller 的请求上下文、下游 HTTP 调用和管理审计；异步消息继续以稳定 event/message ID 为提交与幂等主键，不用 Request ID 替代业务 ID。
- 直接访问内部服务不得信任调用方提供的任意日志字段，Request ID 只用于关联，不用于认证或授权。

### 8.2 结构化日志

所有 Go 产品进程输出单行 JSON，并统一最小字段：`log_schema_version`、UTC timestamp、level、service、module、message、event、version/revision；按上下文追加 request/event/operation ID、固定 error/reason code、状态、耗时和有限计数。

- 字段名、枚举和最大长度固定；不把 payload、query、cookie、Authorization、DSN、文件绝对路径或 `.Error()` 原文直接写入产品日志。
- HTTP access、panic recovery、消费者暂态/永久处理、Migration、依赖状态切换和关停均有稳定事件名。
- 同一持续故障按状态变化或限速记录，避免 Probe、consumer 和告警轮询造成日志风暴。
- Marshaller 的严格 mapping/vocabulary 随合同更新；不兼容日志必须在生产者批次中被测试发现，而不是由最终矩阵静默丢弃。

### 8.3 API 错误

Backend 公开 API 以及 Monitor/Router/Marshaller 的 JSON API 使用统一失败形状：

```json
{"error":{"code":"stable_code","message":"safe message","request_id":"32-lowercase-hex"}}
```

- 认证、权限、校验、冲突、不存在、上游不可用、限流和内部错误映射到稳定 HTTP 状态与 allowlist code。
- 404、405、无效 JSON、panic 和响应尚未提交的内部失败也走同一包络；已提交响应发生错误只记录安全日志，不能追加第二段 JSON。
- Frontend 仅依赖 code 和 HTTP status，未知 code 显示通用安全提示；不展示服务端原始 message、地址或堆栈。
- `/startup`、`/live`、`/ready` 使用独立最小状态响应，不泄露 API 权限和依赖细节。

## 9. Migration 合同

- `backend/migrations` 的内嵌、单调递增文件是唯一 MySQL Schema 来源；产品启动只能通过 one-shot `migrate` job，长运行服务不得自行修改 Schema。
- Migration CLI 至少提供机器可判定的 `status`、`validate`、`up`；产品路径不暴露自动 `down`。现有开发用单步 down 若保留，必须明确为非产品恢复合同。
- `status` 报告 binary target、database version 和 `clean/behind/current/dirty/ahead`，不输出 DSN；Backend readiness 只在 `current` 时成功。
- `up` 使用数据库锁、按序提交并可安全重复执行；并发 runner 只有一个写者，其他实例限时等待或安全失败。
- 已知历史 migration 12 的专用恢复逻辑可保留，但不得扩展为任意 dirty 自动 force。未知 dirty、缺号、checksum/embedded set 不一致或数据库版本高于 binary 必须停止且保持原状态。
- 固定验证覆盖空库到当前版本、`1.13.6` 当前数据到 `1.14.4/1.14.5`、重复 up、并发 up、dirty/future 拒绝，以及迁移后角色、业务、搜索、告警、审计和新写入。回退依赖 Phase 16 backup/restore，不以 down migration 宣称生产回滚。

## 10. 消息与告警可靠性合同

### 10.1 RabbitMQ

- Business Worker 与 Search Indexer 继续使用手动 ack、有限 prefetch、persistent message 和隔离的 retry/dead topology。
- 业务 side effect 成功或已幂等存在后才 ack；暂态失败先确认 retry publish 再 ack 原消息，永久异常先确认 dead publish 再 ack。
- retry/dead publish nack、return、timeout 或 connection loss 时原消息 requeue；不能以本地日志代替 broker 确认。
- 关停先 cancel delivery，再完成或 requeue 当前消息；重连重建 topology/channel/consumer，保留 bounded jitter/backoff。
- 代表性重复投递只产生一次通知、搜索最终与 MySQL 一致，poison message 不形成热循环，payload 不进入日志。

### 10.2 Kafka

- Router 只有在 broker 接受记录后才返回成功；queue 满、timeout、取消和关闭使用稳定错误与状态，不伪报已投递。
- Marshaller 在目标存储写入成功后、且 partition lease 仍有效时提交 offset；暂态存储/commit 失败不得提前推进 offset。
- rebalance/lost partition 取消旧 lease 和在途重试；旧 owner 不得在失去 ownership 后写入或提交。
- 永久无效/不支持 envelope 以固定 reason code、计数和安全日志记录，并明确提交跳过以解除 partition 阻塞；不记录原 payload。
- 不可恢复 consumer 终态使 readiness 失败并令进程非零退出，由 Compose restart policy 重建；不得留下 live 但永久不消费的假健康进程。

### 10.3 告警评估

- Metrics、Logs、Events 任一来源失败只更新对应 rule 的 `unknown/stale` 与固定原因，不阻塞其他来源和社交业务。
- lease、revision 和 transaction 共同保证重启/重复调度不产生两个活动 incident 或重复 trigger/recover audit。
- 依赖恢复后从持久化 `next_evaluation_at` 继续；重启间隙不伪造连续 `for` 条件，恢复事件只发生一次。
- Scheduler panic、timeout 和取消可诊断、可在下一轮恢复；关停后不得继续提交过期结果。

## 11. 产品、安全与权限不变量

- Backend 始终从数据库当前角色授权；Frontend 路由守卫不能替代服务端 `401/403`。
- 固定回归覆盖未登录 `401`、普通用户全部管理 API `403`、超级管理员成功、角色变化后重新分流、公开作者摘要不暴露 role 和引导超级管理员不可降级。
- 浏览器与外部入口不能直达 Monitor、Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch、MySQL、Redis、RabbitMQ 或 Exporter；新增 Probe 不扩大外部端口。
- VictoriaMetrics、Elasticsearch、Monitor、Router、Kafka 或 Exporter 故障时，代表性注册/登录/发帖/评论/点赞/关注/收藏流程仍可工作；与故障来源直接相关的搜索或管理页显示稳定 partial/unavailable 状态。
- 日志、错误、Frontend bundle、diagnostic、evidence 和 runtime contract 中不得出现 Secret 值、私有备份口令、内部 URL userinfo 或宿主绝对路径。
- 所有故障注入、清理和重建继续使用 Phase 16 project/token/label/digest 强归属，不影响其他 Docker project 或用户文件。

## 12. 各批次职责边界

### Phase-17-01：管理中心前端展示更新

- 以 `dev/imple/Phase-17/assets/Management_Center/` 四张图片为视觉基准，更新独立 Admin Frontend 的侧栏/顶栏共享壳层。
- 重构管理大屏、Metrics、Logs、Exporter 四页的信息层级与视觉展示，只使用现有真实 DTO，不硬编码参考图示例值。
- 保持 Events、告警、用户角色、审计在新壳层可用，并完成管理员/普通用户、同源、响应式和可访问性回归。
- 不修改 Backend/API/Schema/消息合同，不提前实现运行时、Migration 或最终韧性验收。

### Phase-17-02：管理中心前端二次展示更新

- 使用第二组五张视觉基准更新系统概览、Metrics、Logs、Alerts、Exporter 和共享壳层。
- 延续现有真实 DTO、权限与同源边界；不把参考图中无 API 支撑的操作或数据伪装成已实现。
- 执行五页桌面/窄屏对照及直接功能回归，不修改后端或 Compose 合同。

### Phase-17-03：统一运行时契约与服务可诊断闭环

- 基于已合入的 `1.14.2` 管理中心展示建立机器可读运行时合同与校验器。
- 对齐配置、三类 Probe、`/health` 兼容、共享退出预算、Request ID、结构化日志和 API 错误。
- 在 Compose 中验证配置失败、软/硬依赖、Probe 状态变化和 SIGTERM/SIGINT 排空。
- 不修改 Schema，不重新设计 Rabbit/Kafka/告警状态机，不运行 Milestone 4 最终候选矩阵，也不回退 Phase-17-02 管理体验。

### Phase-17-04：Migration 与消息处理可靠性闭环

- 完成 Migration status/validate/up、直接前序数据推进和安全拒绝路径。
- 完成 RabbitMQ、Kafka 与告警在重试、重连、重平衡、重复投递、poison、关停和恢复下的固定合同。
- 复用 Phase-17-03 的 Probe/日志/错误输出结果，不另建诊断协议。
- 不新增业务能力、备份格式或 Kubernetes 资源，不提前宣称 Milestone 4 完成。

### Phase-17-05：完整 Compose 韧性验收与 Milestone 4 收口

- 冻结同一个 `1.14.5` Linux `amd64` manifest/Bundle/digest 候选。
- 从独立 Bundle 运行运行时、Migration、消息、告警、角色、双 Frontend、六插件、业务故障隔离和资源清理矩阵。
- 聚合 Phase 17 专属脱敏 evidence，更新支持、运维、未来部署复用文档及同名实施记录。
- 只修复最终矩阵暴露的直接阻断，不首次加入功能或安排一般性 Review。

## 13. 阶段级验收标准

### 13.1 管理中心前端展示

1. `1440 × 1000` 下的管理壳层、管理大屏、Metrics、Logs、Alerts 和 Exporter 页面与第二组五张视觉基准保持一致的信息层级与视觉语言，且只展示真实 API 事实。
2. loading/empty/partial/error、长字段、插件生命周期和 unknown 语义正确；参考图示例数据、地址和账号未硬编码进产品。
3. 代表性窄屏无不可控溢出，键盘/focus/heading/label/status 可用；普通用户不能挂载管理页面或发送管理 API 请求。
4. Events、告警、用户角色和审计在新壳层中无功能回退，统一登录、返回社交、退出、角色撤销和同源边界保持成立。

### 13.2 运行时与诊断

1. 12 个长运行 Go 组件均被运行时合同覆盖，合同、Compose、`.env.example`、插件 manifest 和实现通过一致性校验。
2. `startup/live/ready/health` 方法、响应、状态转换、超时和暴露边界通过；软依赖故障不误杀业务，硬依赖故障不伪 ready。
3. SIGTERM/SIGINT 下先撤就绪再排空，全部组件在统一预算内退出；超时和不可恢复终态非零退出，无孤儿插件或在途状态伪完成。
4. Request ID、统一错误、结构化日志和安全原因码在公开及内部 HTTP 链路可关联，Secret/路径/原始异常扫描无命中。

### 13.3 数据与异步可靠性

1. 空库和 `1.13.6` 当前数据可迁移到最终 Schema，重复/并发 up 安全，dirty/future/不一致输入不改数据且服务不 ready。
2. RabbitMQ 成功、retry、dead、确认失败、broker 重连、重复投递和关停中在途场景无消息静默丢失，代表 side effect 幂等。
3. Kafka store-before-commit、暂态重试、永久异常跳过、rebalance ownership、commit 失败和非零重启路径通过，offset 不越权推进。
4. 三源告警在重启、依赖失败与恢复后状态正确、无重复活动 incident/audit，且不阻断代表性社交流程。

### 13.4 完整产品与 Milestone 4

1. 同一 `1.14.5` 候选从独立 Bundle 在真实 Linux `amd64` server 上完成 lifecycle、唯一 edge、两个 Frontend、六插件、三源告警和代表性业务闭环。
2. 身份/权限矩阵、内部服务负向探测、可观测故障隔离、当前 backup/restore 回归、资源归属与清理全部通过。
3. evidence 绑定同一 release manifest、runtime contract digest、Git revision、Bundle checksum 和场景输入；聚合器拒绝缺项、失败、不同候选或非 Linux `amd64` runtime。
4. 五份拆分方案均有同名真实实施记录，根完成版本为 `1.14.5`，五个批次按顺序合入主线，无阻断问题。

只有以上全部满足才完成 Milestone 4。Kubernetes 不是任何一项成立条件。

## 14. 验证策略与固定阶段门禁

开发中只运行最小受影响包；每批在最终 diff 上运行其拆分方案固定门禁一次。仅因共享运行时、安全边界、持久数据和消息提交契约的明确风险执行跨组件检查，不开展无依据的全仓扩张。

管理中心展示批次先运行 `./scripts/test-frontends.sh` 及真实浏览器的 Admin Frontend/dashboard 直接场景，并以实施记录中的桌面/窄屏截图对照视觉基准；最终候选拟提供以下权威入口；具体脚本在对应实现批次落地，若名称等价调整，必须同步总方案、拆分方案和实施记录：

```bash
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-phase17.sh --manifest dist/release-manifest.json --runtime-contract deploy/runtime-contracts.json --work "$GOPULSE_PHASE17_WORK" --evidence "$GOPULSE_PHASE17_WORK/evidence/linux-amd64.json"
python3 scripts/verify-phase17-evidence.py --linux "$GOPULSE_PHASE17_WORK/evidence/linux-amd64.json"
```

`verify-phase17.sh` 必须调用正式 Bundle lifecycle、edge/API/浏览器与进程入口，覆盖：

- clean install、运行时合同、Probe/配置/SIGTERM；
- `1.13.6` 当前数据 Migration、重复/并发和 dirty/future 拒绝；
- RabbitMQ/Kafka/告警代表成功与失败恢复；
- 用户/超级管理员、双 Frontend、六插件、三源可观测和社交故障隔离；
- 当前候选 backup/restore 必要回归、Secret 扫描、强归属和清理。

Phase 16 runtime gate 已包含的完整 Compose 场景应由最终聚合 runner 复用 receipt，不重复执行同候选同输入场景。前批已成功且未受最终修复影响的单元/局部门禁不因上下文压缩或收口而重跑。

## 15. Evidence 合同

Phase-17-05 的最终 JSON 至少记录：

- schema version、Git revision、product version、release manifest/Bundle/runtime contract digest；
- host OS/kernel/CPU、Docker server OS/arch、Compose version 和运行时间窗口；
- project/token hash、镜像/plugin digest、Migration from/to/status；
- 每个场景的 ID、开始/结束、结果、稳定 reason code、关键事实摘要和脱敏附件路径；
- Probe 转换、signal 退出耗时、Rabbit ack/retry/dead、Kafka offset/ownership、alert incident/audit 摘要；
- 角色矩阵、内部服务负向结果、Secret scan、资源快照和 cleanup 结果。

证据必须原子完成。聚合器拒绝失败/中断、缺少必填场景、候选 digest 混用、模拟架构、Secret 命中、未清理受管资源或无关资源快照变化。

## 16. 有限前置确认与禁止猜测

| 批次 | 开工前确认 | 未满足时 |
| --- | --- | --- |
| 17-01 | 四张视觉基准、现有 Admin Frontend DTO/路由/权限与真实 Compose 管理入口 | 只实现现有数据可表达的展示；不硬编码示例值或猜测新 API |
| 17-02 | 第二组五张视觉基准、`1.14.1` 前端、现有 DTO/告警操作与真实 Compose 管理入口 | 无数据区域明确标注；不扩展后端合同 |
| 17-03 | 组件/端口/配置真实清单、现有 Probe 和退出预算、Phase 16 Bundle 合同 | 先补机器清单；不凭文档猜测实现已一致 |
| 17-04 | Migration 当前版本、`1.13.6` 当前数据输入、Rabbit topology、Kafka topic/group、告警 lease | 阻断对应真实状态门禁；不以 mock 冒充 |
| 17-05 | 可拉取的同一 `1.14.5` 候选、真实 Linux `amd64` server、独立目录/project/数据与执行窗口 | 阻断 Milestone 4 收口，不回滚前批已通过结果 |

不得猜测依赖已恢复、消息已提交、Schema 已 clean、角色未降级、日志已脱敏、候选来自同一 manifest 或 Kubernetes 将自动修复问题；结论必须来自实际状态、业务事实和 evidence。

## 17. 实施记录、版本与提交

- 每批完成前创建 `dev/logs/Phase-17/` 下与拆分方案同名的 Markdown 记录。
- 记录实际改动文件、命令与结果、首次失败及最小修复、候选/evidence、偏差、已知限制和非阻断后续项；未运行的检查不得写为通过。
- 五批完成时分别把 `VERSION` 及受管版本元数据更新为 `1.14.1`、`1.14.2`、`1.14.3`、`1.14.4`、`1.14.5` 并纳入本批提交。
- 每批只提交本任务文件，不包含用户或其他任务的工作区改动；规划提交不修改 `VERSION`。

## 18. 停止条件与未来部署复用

每批在其固定验收通过、无阻断问题、实施记录和版本提交后立即停止，不追加重构、更多边界用例或独立 Review。

Phase 17 只在第 13 节全部通过时完成并收口 Milestone 4。可供未来部署适配复用的固定输入是：

- `1.14.5` Linux `amd64` 完整产品、不可变 release manifest、Bundle 和制品 digest；
- 已通过第二组五张视觉基准、响应式、可访问性、统一登录和权限回归的独立 Admin Frontend 管理体验；
- 机器可读运行时合同、配置目录、三类 Probe、`/health` 兼容期、统一退出预算与稳定错误/日志语义；
- 当前 Schema target、`1.13.6 → 1.14.x` 单跳 Migration 证据，以及 Rabbit/Kafka/告警可靠性合同；
- 完整 Compose 产品、角色矩阵、故障隔离、backup/restore 和资源清理的 Phase 17 evidence。

未来部署适配只能迁移同一产品并接入这些既有 Probe、退出和配置合同，不得以 sidecar、init 脚本、Ingress 或临时旁路补做 Phase 17 产品缺口。
