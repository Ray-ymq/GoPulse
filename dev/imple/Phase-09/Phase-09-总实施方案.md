# Phase 9：LogMonitor 与日志链路总实施方案

## 1. 实施目标

在 Phase 4 已交付统一 Schema v1 JSON Lines 业务日志、Phase 7 已交付 Message Router 与 Kafka 单 Topic、Phase 8 已交付正式 Marshaller 消费与可靠 offset 语义的基础上，接入 LogMonitor 被动接收路径，形成第一条真实 Logs 端到端链路：

```text
Backend / Business Worker / Search Indexer / search-reindex
  → stdout Schema v1（始终保留）
  → 有界异步 HTTP Push
  → LogMonitor 第一次校验与标准 logs Envelope v1
  → Message Router → gopulse-observability-v1
  → Marshaller logs 二次校验与确定性文档转换
  → Elasticsearch 独立日志索引
  → Backend 实时 admin 授权查询
```

阶段完成必须同时证明：真实 Backend API 产生的日志不是由测试 fixture 伪造；业务线程不等待 LogMonitor、Router、Kafka、Marshaller 或 Elasticsearch；LogMonitor 只在 Router/Kafka 已接受后返回成功；Marshaller 在 Elasticsearch 确认写入且 partition ownership 仍有效后提交 offset；相同 `message_id` 重放不会形成重复日志文档；日志索引与帖子搜索索引严格隔离；未登录、普通用户和管理员继续使用同一套 Cookie 身份，并分别得到 `401`、`403` 和受限查询结果；Logs 与既有 Metrics 链路能在同一 Topic、同一 Marshaller 中共同运行。

只新增一个接收接口、直接读取 stdout 文件后手工写 Elasticsearch、绕过 Router/Kafka、用静态 JSON 代替真实 API 日志、只验证 Elasticsearch 文档存在，或把日志查询开放给浏览器直连内部组件，均不构成 Phase 9 完成。

## 2. 当前真实基线与规划输入

本方案以编写时可见的本地 `upstream/main` 跟踪提交 `ce72a8e` 为代码基线。根 `VERSION` 与 Frontend npm 元数据均为 `1.5.4`；PR #79 已关闭 Phase 8 Review 的 ownership、IPv6 loopback、无效 poll 配置和分支治理问题。实施开始前仍必须重新 fetch 主远程并以最新 `main` 为准。

当前与 Phase 9 直接相关的真实基线如下：

- Backend、Business Worker、Search Indexer 和 search-reindex 已输出 Phase 4 Schema v1 单行 JSON；`cmd/migrate` 仍是面向操作者的文本命令，不属于日志源。
- 日志公共字段固定包含 `log_schema_version`、`timestamp`、`level`、`service`、`module` 和 `message`，并按场景附带 `request_id`、`event_id`、安全错误码和有限数值标识；用户内容、认证材料、连接凭据、原始错误和服务器路径已被禁止。
- 当前所有日志只写 stdout；没有远程 sink、内存队列、LogMonitor 接收、日志 Envelope 或日志查询 API。
- `monitor` 是独立 Go module 和单一 Monitor 进程，已包含 Plugin Manager 与主动 Pull 的 MetricsMonitor；HTTP 服务当前只有 health、ready 和插件管理接口。
- Monitor 已通过 HTTP Publisher 调用 Router 的 `POST /internal/v1/messages`。Router 使用 Bearer 服务身份，只接受 Envelope v1 的 `metrics/redis` 组合，并把原始 JSON bytes 写入固定 Topic `gopulse-observability-v1`。
- `marshaller` 是独立 Go module，使用正式 group `gopulse-marshaller-metrics-v1`、禁用自动提交、从 earliest 开始、按单 partition 单 record 处理，并具备 generation ownership fencing、永久异常跳过、暂时存储故障重试和确定性重放能力。
- 当前 Marshaller decoder/processor 只接受 `metrics/redis`，writer 只连接 VictoriaMetrics；Phase 9 必须增加按 `type/source` 的显式处理边界，不能放宽已经通过的 metrics payload 契约。
- Elasticsearch `9.5.2` 已由 Compose 以 loopback 端口运行，Backend 通过有界共享 transport 执行帖子搜索；帖子读 alias 固定为 `gopulse-post-search-v1`，Phase 9 尚无日志 template、alias、index 或 repository。
- Backend 已有数据库实时 `admin` 授权中间件。现有管理员插件 API 证明：未登录返回 `401 authentication_required`，普通用户返回 `403 permission_denied`，拒绝请求可在调用内部服务前终止。
- Bash 生命周期、验证脚本和 CI 已覆盖 Backend、Monitor、Router、Marshaller、Kafka、VictoriaMetrics、Elasticsearch、进程记录与强资源归属；Phase 9 必须扩展这些入口，不能建立第二套弱清理流程。

若最新主远程改变上述 Schema、Topic、consumer group、offset、Elasticsearch、授权或生命周期契约，Phase-09-01 开工前必须先更新本总方案和所有尚未开始的拆分方案，不得以兼容猜测替代真实基线。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 8 全部批次已经合入主远程 `main`，远程固定门禁成功，根与 Frontend 版本均为 `1.5.4`，实施记录与真实提交一致。
- Metrics 的真实 Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics 链路、永久异常继续、存储/Kafka恢复和 group ownership 已有可引用证据。
- 实施、应用测试和集成验收固定在 Windows 宿主的 WSL2 Linux filesystem 中进行；Bash 是唯一维护的本地生命周期和验收入口。
- 每批开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立 `develop/x.x.x` 分支；不得在 `update`、Phase 8 分支或已完成分支实施产品能力。
- 开始前保存 Git 状态、日常 Compose project/container/network/volume、`.run` 进程、端口、插件根和当前日志文件快照，不停止、删除、暂存或提交其他任务资源。
- 每批只实现对应验收合同；没有直接失败依据时，不开展 Elasticsearch 集群调优、通用日志平台设计、全仓审计或覆盖率扩张。

### 3.2 权威批次、版本与开发分支

Phase 9 使用 `1.6.x` 版本线，`1.6.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-09-01 | `1.6.1` | `develop/1.6.1` | 未开始 |
| Phase-09-02 | `1.6.2` | `develop/1.6.2` | 未开始 |
| Phase-09-03 | `1.6.3` | `develop/1.6.3` | 未开始 |

执行规则：

- 同一批次全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-09/Phase-09-XX-*.md`，只记录实际改动、实际验证、偏差、失败和限制。
- Phase-09-01 交付真实 Backend API 日志到管理员查询的可运行最小纵向闭环，不把 LogMonitor、Router logs、Marshaller logs、Elasticsearch 写入或 `401/403` 查询授权中的任一环节推迟给后续批次。
- Phase-09-02 在已正确的纵向闭环上接入其余 Phase 4 后台进程日志，并完成有界队列、短时故障恢复、重复重放、混合 Metrics/Logs、日常生命周期和资源安全。
- Phase-09-03 只在已合入的最终实现上执行跨批次阶段矩阵、必要业务回归、文档、版本和远程状态收口；除真实复现的阻断问题外不增加产品功能。
- 已推送分支不得静默改名或重新编号。批次数量或顺序在实施前变化时，先更新本表并重新计算所有尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 在现有 Monitor 进程内增加被动接收的 LogMonitor，不创建与 MetricsMonitor 重复的独立守护进程。
- Backend 日志继续写 stdout，同时通过不阻塞调用线程的有界异步 HTTP sink 推送到 LogMonitor。
- 覆盖 Phase 4 的生产日志源：`backend`、`business-worker`、`search-indexer`、`search-reindex`；`cmd/migrate` 不纳入。
- LogMonitor 对单条 Schema v1 日志执行大小、JSON、字段、类型、服务/module/message 词汇、时间和敏感字段边界的第一次校验，并封装标准 logs Envelope v1。
- Router 在保持原始 bytes、不解析 logs payload 的前提下接受受支持的 logs source，并继续路由到单一 `gopulse-observability-v1` Topic。
- Marshaller 把公共 Envelope 校验、类型分派、metrics handler 和 logs handler 分离；既有 metrics 规则与 offset 语义保持不变。
- logs handler 执行第二次严格校验，生成固定 Elasticsearch 文档，以 `message_id` 作为文档 `_id` 写入按 UTC 日期划分的日志索引。
- 建立日志 index template、严格 mapping、读 alias 和 Backend 日志 repository；帖子搜索与日志操作使用互不重叠的固定名称。
- 新增受认证且实时 admin 授权的日志查询 API，支持有限时间范围、精确过滤和 HMAC 绑定游标，不接受任意 Elasticsearch DSL。
- 完成 Logs 与 Metrics 并存、故障恢复、权限隔离、业务隔离、Bash 生命周期、独立验收、CI、README、版本和实施记录。

### 4.2 明确不做

- 不实现 EventMonitor、`events` Envelope、事件索引或事件查询；这些属于 Phase 10。
- 不实现 Phase 11 的日志页面、Dashboard、图表、指标查询产品 API或浏览器可视化。
- 不采集 Frontend、容器 stdout、系统日志、Kafka/Elasticsearch 自身日志或 Kubernetes 日志。
- 不实现文件 tail、syslog、Fluent Bit、OpenTelemetry Logs、OTLP、TCP 协议、批量 ingest、压缩上传或第三方日志 Agent。
- 不建立本地磁盘 spool、持久发送队列或零丢失承诺；HTTP `202` 之前为有界 best-effort，之后才进入 Kafka/Marshaller 的 at-least-once 边界。
- 不拆分新的 Kafka Topic，不迁移既有 consumer group，不引入 Schema Registry、DLQ、重放管理 API、Kafka transaction 或 exactly-once 声明。
- 不实现 Elasticsearch ILM、自动删除、冷热分层、rollover、容量规划、多节点、高可用、TLS、xpack 用户体系或公网入口。
- 不提供任意全文查询、正则、通配符、聚合、脚本、排序字段、索引名或原始 DSL 透传。
- 不把 LogMonitor、Router、Kafka 或 Marshaller 加入 Backend readiness；Elasticsearch 继续保持 Phase 3 已存在的 readiness 语义。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收，不创建应用容器镜像。

## 5. 组件职责与运行架构

### 5.1 进程与模块边界

- Backend 日志基础包负责生成 Schema v1 JSON、写 stdout 和把完全相同的单条 JSON bytes交给异步 shipper；业务调用点不感知 HTTP、Router、Kafka 或 Elasticsearch。
- Log shipper 负责 message ID、内存队列、HTTP、重试和有界关闭，不负责解释业务日志语义；发送状态日志必须走 stdout-only logger，避免递归投递。
- LogMonitor 属于现有 `monitor` module，负责被动接收、第一次清洗、标准 Envelope 封装和调用既有 Router Publisher；不读文件、不 tail stdout、不导入 Kafka SDK。
- Router 只验证 Envelope 顶层与受支持的 `type/source` 组合、选择固定 Topic并等待 Kafka delivery；不重排、不清洗、不重新序列化 payload。
- Marshaller 负责公共 Envelope 二次校验和按类型分派；metrics handler 继续写 VictoriaMetrics，logs handler 独立写 Elasticsearch。
- Elasticsearch 保存严格、可查询的日志文档；不保存原始请求体、Authorization、完整 Envelope、Kafka metadata 或未知扩展字段。
- Backend 日志查询 repository 只读固定日志 alias，经现有身份和实时数据库 admin 授权后返回白名单字段；浏览器不能获得内部 URL或凭据。

### 5.2 启动与关闭顺序

日常启动顺序固定为：

```text
Compose 基础设施（MySQL / Redis / RabbitMQ / Elasticsearch / Kafka / VictoriaMetrics）
→ 显式核对 Kafka Topic 与 Elasticsearch 可用
→ Message Router
→ Marshaller（Metrics + Logs handlers）
→ Monitor（Plugin Manager + MetricsMonitor + LogMonitor）
→ Backend / Business Worker / Search Indexer / Frontend
```

关闭顺序固定为：

```text
Backend / Business Worker / Search Indexer（停止产生并有界 drain 日志）
→ Monitor / Exporter（停止接收与产生可观测消息）
→ Marshaller（停止 poll 并处理已确认边界）
→ Router（完成或取消在途 produce）
→ 其余应用 → Compose 基础设施
```

- `search-reindex` 是一次性命令；成功或失败退出前只在配置的日志 drain timeout 内等待，不因远程日志不可用改变命令原始退出结果。
- 依赖暂不可用时，Backend 等业务进程继续启动和服务；日志 shipper 保留有限队列并退避。配置本身非法时允许安全启动失败，但不得打印配置值。
- Monitor `/health` 仍只表达进程存活；`POST /internal/v1/logs` 用自己的状态码表达 Router/Kafka 可用性。Marshaller `/ready` 增加 Elasticsearch 检查，但 `/health` 保持不访问依赖。

## 6. Backend stdout 与异步 Push 契约

### 6.1 双输出与消息标识

- 每条应用日志首先写 stdout；远程 sink 不得替代 stdout，也不得改变 Phase 4 JSON bytes、日志等级、业务返回值或消息状态机。
- 每个待发送记录使用 `crypto/rand` 生成 16 bytes 并编码为 32 位小写十六进制 `message_id`。同一内存队列项的全部 HTTP 重试复用同一 ID。
- HTTP 请求正文是原始单条 Schema v1 JSON object，`Idempotency-Key` 等于该队列项 `message_id`；正文中不增加 transport 字段。
- 只有 LogMonitor 返回 `202 Accepted` 才移除队首。网络、timeout、`429` 或 `5xx` 使用带抖动的有限指数退避；明确的 `400/413/422` 作为当前记录永久拒绝并只写有限本地状态；`401` 进入退避降级并等待配置重启修复。
- HTTP redirect 禁止；响应最多读取 4 KiB 后丢弃，不把响应正文、URL、token 或原日志写入状态日志。

### 6.2 有界性与业务隔离

- 日志调用只做 stdout write、message ID 生成和非阻塞 enqueue；不得在 HTTP handler、Outbox、Worker ack/nack 或搜索索引控制流内等待网络。
- 默认队列容量 `256` 条，允许 `1..4096`；单条原始日志最大 `64 KiB`。队列满时只丢弃远程副本，stdout 记录仍保留，业务结果不变。
- 同一进程保持一个发送 worker 和一个队首在途请求，不创建每条日志 goroutine或无界重试集合。
- 队列满、鉴权失败、持续不可用和恢复只通过 stdout-only 的固定状态转换日志表达并做节流，不把底层错误、日志正文、token、URL 或路径回送到远程 sink。
- shutdown 先拒绝新 enqueue，再在默认 `5s`、允许 `0..30s` 的窗口内 drain；超时后安全停止并记录未发送计数，不修改原命令/业务退出码。
- 队列仅在内存中。进程崩溃、超过队列的长时间故障或 drain 超时允许丢失尚未获得 `202` 的远程副本；该限制必须写入 README 与实施记录，不得宣称全链路 exactly-once 或零丢失。

## 7. LogMonitor 接口、第一次清洗与 Envelope

### 7.1 内部接收接口

| Method | Path | 身份 | 请求 | 成功状态 |
| --- | --- | --- | --- | --- |
| `POST` | `/internal/v1/logs` | 专用 Log ingest Bearer token | 单条 Schema v1 JSON 日志 | `202` |

- 新配置 `LOG_MONITOR_INGEST_TOKEN` 至少 32 bytes且拒绝 CR/LF。它只授权写日志，不得复用 `MONITOR_API_TOKEN` 的插件管理权限，也不得接受用户 Cookie、admin Cookie、JWT、query token 或浏览器 Origin。
- 只接受唯一一个 `Content-Type: application/json`，拒绝 `Content-Encoding`、重复/缺失 `Idempotency-Key`、超限 Content-Length 或 chunked body。
- `Idempotency-Key` 必须为 32 位小写十六进制；LogMonitor 将它直接作为 Envelope `message_id`，不得另生成会破坏重试幂等的 ID。
- 请求 body 上限固定 `64 KiB`；错误返回有限 code：`internal_authentication_required`、`log_invalid`、`log_too_large`、`log_unsupported`、`transport_unavailable`。
- 只有 Router 返回 `202` 时 LogMonitor 返回 `202`。Router/Kafka 不可用或结果不确定时返回 `503 transport_unavailable`，由源端以同一 ID 重试。

### 7.2 Schema v1 第一次清洗

输入必须是有效 UTF-8 的唯一 JSON object，递归拒绝重复 key，拒绝未知字段、尾随 token、`null` 和错误 JSON 类型。公共必需字段为：

| 字段 | 规则 |
| --- | --- |
| `log_schema_version` | 整数 `1` |
| `timestamp` | UTC RFC3339Nano；最多超前接收端 `5m`，不设置历史下限 |
| `level` | `info`、`warn`、`error` |
| `service` | `backend`、`business-worker`、`search-indexer`、`search-reindex` |
| `module` | 必须属于 Phase 4 对应 service 的固定 module 集合 |
| `message` | 必须命中受版本控制的 `service/module/message` 低基数词汇表 |

允许的可选字段固定为：

```text
request_id, event_id, event_type,
user_id, post_id, comment_id, notification_id, outbox_id,
method, route, status, duration_ms, response_bytes,
error_code, reason, operation, resource, stage, result,
attempt, batch_size, document_count,
panic_recovered, response_committed
```

- `request_id` 为 32 位小写十六进制；`event_id` 为 canonical UUID；数值 ID 必须为正整数；计数、时长、状态和 attempt 使用有限非负整数并按字段范围校验。
- `method`、`route`、`error_code`、`event_type`、`reason`、`operation`、`resource`、`stage` 和 `result` 使用有限词汇或安全 token 规则；route 只允许 Gin template 或 `unmatched`，不得含 query、fragment 或原始路径值。
- 字符串必须有效 UTF-8、长度有界且无控制字符。日志中出现用户名、密码、JWT/Cookie/Authorization、帖子/评论内容、搜索词、连接 URL、底层错误、响应 body、服务器绝对路径或堆栈时必须在接收端拒绝，而不是保存后再隐藏。
- 不适用字段保持省略；LogMonitor 不补零值、不猜测用户身份、不重写日志时间，也不接受任意嵌套 `attributes` 对象。

### 7.3 logs Envelope v1

成功清洗后构造：

```json
{
  "schema_version": 1,
  "message_id": "<Idempotency-Key>",
  "type": "logs",
  "source": "<validated service>",
  "timestamp": "<validated log timestamp>",
  "payload": { "<canonical Schema v1 log fields>": "..." }
}
```

- `source` 必须逐字等于 payload 的 `service`；`timestamp` 必须逐字表达同一时刻；payload 保留 canonical 字段，不携带 ingest token、URL、HTTP header 或接收时间。
- Envelope 由标准 JSON encoder 生成并受 Router 1 MiB 上限保护；同一输入与 message ID 的重试必须产生确定性等价内容。
- LogMonitor 使用现有 `MONITOR_ROUTER_TOKEN` 调用 Router。Log ingest 身份、插件管理身份和 Router 发布身份三者职责分离。

## 8. Router、Kafka 与 Marshaller 公共处理

### 8.1 Router 与 Kafka

- Router 顶层 validator 扩展为接受：`metrics/redis` 和 `logs/{backend|business-worker|search-indexer|search-reindex}`；其他 schema/type/source 组合继续返回 `422 message_type_unsupported`。
- Router 仍不解析 logs payload，不重写 value，不从 source、query、header 或 payload 接受任意 Topic 名。
- 路由表继续使用单一 Topic：

```text
metrics → gopulse-observability-v1
logs    → gopulse-observability-v1
```

- Kafka record key 继续等于 `message_id`，value 继续是 Router 收到的原始 Envelope JSON bytes；Topic 仍为 1 partition / replication factor 1，禁止自动创建。
- Phase 9 不增加日志 Topic 或改变 retention。Metrics/Logs 的顺序、积压和重放共同遵循现有 Topic 合同。

### 8.2 Marshaller 公共 Envelope 与类型分派

- 把 key/ID、大小、UTF-8、唯一 JSON、顶层字段、schema、timestamp 和 future skew 提取为公共 Envelope 校验；payload 以 `json.RawMessage` 保留给类型 handler。
- 显式 registry 只允许 `metrics/redis` 与四个 logs source。未知组合是永久无效消息，记录有限 reason 后在 ownership 有效时提交，不调用任何存储。
- metrics handler 保留 Phase 8 全部严格 payload、transform、VictoriaMetrics writer、timestamp 和标签契约；重构不得把 logs 可选字段或 source 放入 metrics validator。
- logs handler 独立完成 payload 二次校验、Document 生成和 Elasticsearch writer；两种 handler 使用同一 ownership/commit 状态机，但 storage 名称、readiness、错误 reason 和测试 fixture 必须清晰区分。
- 合法 record 只有在所属 writer 成功且当前 lease 仍有效后提交。永久无效 record 不调用 writer并安全跳过；网络、timeout、认证、非成功响应、模板不可用和结果不确定均不提交。
- 保留正式 group `gopulse-marshaller-metrics-v1`，避免无依据迁移 offset；该名称是既有兼容标识，不意味着只处理 metrics。
- 首版保留单 Topic、单 partition、单正式 group 的有序 backpressure。当前 logs record 遇到 Elasticsearch 暂时故障时会延迟其后的 metrics record，但不得越过或误提交；多 Topic/独立 group 隔离须基于真实负载另行规划。

## 9. Elasticsearch 日志存储契约

### 9.1 名称与隔离

| 用途 | 固定名称 |
| --- | --- |
| Index template | `gopulse-logs-v1-template` |
| 物理索引 | `gopulse-logs-v1-YYYY.MM.DD`（按 Envelope UTC 日期） |
| Backend 读 alias | `gopulse-logs-v1-read` |
| 帖子搜索 alias | 保持 `gopulse-post-search-v1` |

- template 只匹配 `gopulse-logs-v1-*`，自动附加读 alias；Marshaller 只能创建/写该日精确索引，Backend 只能读固定 alias。
- 日志代码不得使用 `gopulse-*`、`*logs*` 或客户端提供的 index；帖子 search/reindex 代码不得访问日志 prefix。
- Elasticsearch volume 继续复用现有服务，但索引、mapping、repository、query builder 和测试 fixture 独立。

### 9.2 严格文档与写入

- mapping 使用 `dynamic: strict`，`@timestamp` 使用 `date_nanos`；`service`、`module`、`level`、`message`、关联 ID、route 和有限状态字段使用 exact-match 友好的 `keyword`，数值字段使用 `long`，panic/commit 标记使用 `boolean`。
- `_source` 只包含第 7.2 节 canonical 白名单字段，并把 payload `timestamp` 映射为 `@timestamp`；不保存 `message_id`、Envelope、Kafka partition/offset、原始 JSON、未知字段或 ingest token。
- 文档 `_id` 固定为 Envelope `message_id`。Marshaller 使用确定性 JSON 文档执行幂等 `PUT`；同一 record 在 HTTP 结果不确定或 commit 失败后重放，只能创建或覆盖同一 `_id`，不能产生第二份命中。
- writer 使用有界 HTTP client、禁止 redirect、限制响应大小并验证状态与 `_index/_id/result`；错误与日志不打印响应 body、文档、URL 或凭据。
- template 缺失时由 Marshaller 以固定正文幂等确保；template/mapping 不兼容、目标不可用或写入结果不确定均视为暂时基础设施失败，不通过提交 offset 静默丢弃。
- 本阶段不启用 ILM 或自动删除；开发环境日志会随现有 Elasticsearch volume 保留，容量限制作为明确后续事项。

## 10. Backend 日志查询 API 与用户态边界

### 10.1 HTTP 契约

| Method | Path | 未登录 | 普通用户 | admin |
| --- | --- | --- | --- | --- |
| `GET` | `/api/v1/observability/logs` | `401 authentication_required` | `403 permission_denied` | `200` 或安全 `4xx/503` |

- 路由必须按 `RequireAuthentication → RequireAdmin → Logs Handler` 顺序注册。授权从 MySQL 当前用户角色读取，不信任 JWT role，也不建立第二套管理员会话。
- 未登录和普通用户请求在构造 Elasticsearch request 前结束；定向测试使用调用计数证明拒绝请求不触达 repository。
- 成功响应沿用 `{data,meta.next_cursor}`；每项只返回 canonical 安全字段，不返回 `_index`、`_id`、score、PIT、Envelope、Kafka metadata、内部 URL 或底层错误。
- Elasticsearch 不可用返回 `503 logs_unavailable`；错误正文只含公共 code/message。

### 10.2 查询、范围与分页

允许的 query 参数固定为：

```text
from, to, service, module, level, message,
request_id, event_id, error_code, limit, cursor
```

- 时间使用 UTC RFC3339Nano。首次请求默认最近 `15m`，最大范围 `24h`；`from < to`，`to` 不得超前服务器当前时间超过 `5m`。
- `service/module/level/message/error_code` 使用精确匹配与已知词汇；`request_id`、`event_id` 使用固定格式；不接受重复参数、空值、未知参数、通配符、正则、脚本或任意 JSON。
- `limit` 默认 `50`、范围 `1..100`。排序固定为 `@timestamp desc`、`_shard_doc desc`，不得由客户端覆盖；`_shard_doc` 只在同一 PIT 内作为稳定次序值进入签名游标，不返回给调用者。
- 第一页可使用除 `cursor` 外的过滤参数并打开固定日志 alias 的 Elasticsearch PIT；服务端把解析后的实际 `from/to`（包括默认值）、全部 canonical filters 和 `limit` 固化进签名游标。翻页请求只能携带唯一的 `cursor` 参数，使用其中的 PIT 与 `search_after`，不能重新计算默认时间或混入新过滤条件。
- 游标最长 2 分钟，使用由 `AUTH_JWT_SECRET` 派生且与业务搜索不同 domain 的 HMAC key，并绑定 canonical filters、PIT、过期时间和最后 sort 值。游标被篡改、过期、与其他参数混用或 PIT 失效返回 `400 validation_failed`；不回显解析细节。无日志或 alias 尚不存在时返回空 `data` 和 `next_cursor=null`，不得把空库误报为 `503`。

## 11. 配置合同

Phase 9 新增或扩展的最小配置如下；具体实现可合并重复项，但不得弱化限制：

```text
LOG_MONITOR_URL=http://127.0.0.1:9090
LOG_MONITOR_INGEST_TOKEN=<minimum-32-bytes>
LOG_SHIP_REQUEST_TIMEOUT=2s
LOG_SHIP_QUEUE_CAPACITY=256
LOG_SHIP_RETRY_MIN=250ms
LOG_SHIP_RETRY_MAX=5s
LOG_SHIP_SHUTDOWN_TIMEOUT=5s

MONITOR_LOG_MAX_BYTES=65536
MONITOR_LOG_FUTURE_SKEW=5m

MARSHALLER_ELASTICSEARCH_URL=http://127.0.0.1:9200
MARSHALLER_ELASTICSEARCH_TIMEOUT=3s
MARSHALLER_LOG_TEMPLATE=gopulse-logs-v1-template
MARSHALLER_LOG_INDEX_PREFIX=gopulse-logs-v1-

BACKEND_LOG_READ_ALIAS=gopulse-logs-v1-read
BACKEND_LOG_QUERY_DEFAULT_RANGE=15m
BACKEND_LOG_QUERY_MAX_RANGE=24h
```

- `LOG_MONITOR_URL` 为空时保持 stdout-only，便于独立运行；非空时必须是无 credentials/query/fragment 的 loopback HTTP base URL，且 ingest token 必须合法。
- shipper timeout、retry、queue 和 shutdown 范围必须有限且彼此一致；配置错误在启动网络 worker 前返回安全错误，不打印原值。
- Monitor 接收上限不能高于 Router 的消息上限；Marshaller logs record 上限继续受公共 1 MiB 限制。
- Marshaller 与 Backend Elasticsearch URL 沿用现有 URL 安全解析和有界 transport；日常 WSL2 配置固定 loopback。index template/prefix/read alias 必须等于代码常量，不接受任意名称。
- `.env.example` 只提供开发凭据；`scripts/dev.sh` 可在未设置时从 `MONITOR_URL` 衍生 LogMonitor URL，但不得复用 `MONITOR_API_TOKEN` 作为 ingest token。

## 12. 故障、恢复与安全边界

- **LogMonitor/Router/Kafka 暂不可用**：业务日志仍写 stdout，业务请求与 RabbitMQ 状态机不等待；shipper 保留队首并有界退避，短时恢复后以同一 ID继续。队列满只影响未接受的远程副本。
- **Elasticsearch 暂不可用**：当前 logs record 不提交，Marshaller health 存活、ready 失败并重试；现有 Backend readiness 因 Phase 3 Elasticsearch 依赖保持原语义，但注册、帖子、评论、点赞等非搜索业务不得因新增日志代码改变结果。
- **VictoriaMetrics 暂不可用**：当前 metrics record 继续使用 Phase 8 语义；不得误写 Elasticsearch。由于单 partition 有序处理，当前失败 record 可能延迟后一类型，恢复后按原顺序继续。
- **永久无效日志**：LogMonitor 接收层拒绝不进入 Kafka；受控 fixture 绕过 LogMonitor进入 Kafka时，Marshaller 二次校验不调用 Elasticsearch、提交坏 record 并继续后续合法 record。
- **HTTP 接受结果不确定或 Kafka commit 失败**：源端复用 message ID，Marshaller 复用 `_id`；重复 Kafka record和重放不会形成第二文档，但系统仍明确为 at-least-once。
- **进程关闭/崩溃**：未获得 LogMonitor `202` 的内存记录可能丢失；已进入 Kafka但未提交的记录由正式 group 重取。不得用本地文件或日志猜测提交成功。
- **权限失败**：Log ingest token、Monitor admin token、Router token、Marshaller token、用户 Cookie/JWT职责分离；任一不能替代另一身份。
- **敏感信息**：两次 validator、严格 mapping、查询 DTO 和负向验收共同禁止凭据、用户内容、任意路径与底层错误；管理员权限不放宽存储或返回字段。
- **内部暴露**：Monitor、Router、Kafka、Marshaller、Elasticsearch 和 VictoriaMetrics 的日常端口保持 loopback/受控网络；Frontend bundle 不包含任何内部 URL或 token。

## 13. Bash 生命周期、隔离验收与资源归属

- `scripts/dev.sh` 加载日志配置、构建更新后的四个 Go module，按第 5.2 节启动并为 Backend/Worker/Indexer 注入 LogMonitor sink；不得覆盖现有 `.env`。
- `scripts/verify.sh` 保持只读，只检查进程、内部 readiness、固定 index template/alias、有限日志查询和 Metrics 查询；不得产生日志、提交 Kafka offset、创建索引、删除文档或修复资源。
- `scripts/down.sh` 先有界停止日志源，再停止 Monitor/Marshaller/Router和基础设施；只操作强 PID/Compose project 归属资源，保留日常 volumes。
- 新增 `scripts/verify-logs.sh`。`--self-test` 只执行无 Docker 的 token、URL、queue、body limit、index/alias、查询参数、PID、port、project/container/volume 与清理目标负向测试。
- 默认验收使用随机 Compose project、数据库、Kafka group/offset观察、Elasticsearch/VM volume、端口、进程目录、插件根、日志文件和临时凭据；成功、失败、中断都只清理本次强归属资源。
- 主成功路径必须由真实 HTTP API 与真实后台事件产生；fixture 仅用于一个接收层无效输入、一个 Kafka 层永久无效 logs Envelope 和完全相同 message ID重放。
- 验收以 request ID、event ID、message ID、partition/offset 和 UTC 时间窗关联证据，但输出中不得记录 token、用户内容或完整日志/Envelope。

## 14. 跨批次依赖与摘要

| 批次 | 纵向交付 | 关键输入 | 关键输出 |
| --- | --- | --- | --- |
| Phase-09-01 | Backend API 日志端到端查询闭环 | Phase 4 Backend JSON 日志、Phase 7 Router、Phase 8 Marshaller、现有 Elasticsearch/admin | 非阻塞 Push、LogMonitor、logs Envelope、Router/Marshaller logs、独立索引、admin 查询与 `1.6.1` |
| Phase-09-02 | 后台日志、可靠投递与故障恢复闭环 | 已合入的 `1.6.1` 纵向能力 | Worker/Indexer/reindex 接入、短时恢复、幂等重放、Metrics/Logs 并存、完整运维与 `1.6.2` |
| Phase-09-03 | 集成验收与阶段收口 | 已合入的 `1.6.2` 最终能力 | 阶段矩阵、权限/业务/资源隔离、文档与 `1.6.3` 交接 |

- 09-01 必须独立可运行、可写、可查且权限正确，不能只交付中间组件骨架。
- 09-02 不重新设计公共 API、Envelope、mapping 或查询契约，只扩展剩余日志源并关闭可靠性/运维矩阵。
- 09-03 不以“收口”为由重跑所有排列或增加全文检索、前端、告警、保留策略等范围；只修固定矩阵暴露的阻断问题。

详细方案：

- [Phase-09-01：Backend 日志端到端查询闭环](Phase-09-01-Backend日志端到端查询闭环.md)
- [Phase-09-02：后台日志、可靠投递与故障恢复闭环](Phase-09-02-后台日志可靠投递与故障恢复闭环.md)
- [Phase-09-03：集成验收与阶段收口](Phase-09-03-集成验收与阶段收口.md)

## 15. 测试策略与固定阶段矩阵

### 15.1 执行效率与停止规则

- 每批先从详细方案提取“改变的生产行为 → 最低有效测试层 → 固定完成门槛”，初始探索只读直接受影响的日志、Monitor、Router、Marshaller、Elasticsearch、授权和脚本代码。
- 第三方源码只在公共 API、官方文档和具体错误无法解决阻断失败时读取最小相关符号，并在实施记录写明原因。
- 同一状态转换优先一个代表性成功和一个代表性失败；严格字段全集在 validator unit 层证明，真实链路只选择代表，不做组合爆炸。
- 验证顺序为受影响 package → 注入 HTTP/writer/committer → 真实 Kafka/Elasticsearch → 真实业务全链路。只有共享基础设施、安全边界或观察到回归时扩大。
- 已成功检查在相关代码、配置、依赖和环境未变化时保持有效；最终固定门槛通过且无阻断失败后立即更新记录、版本、提交并停止。

### 15.2 批次验证边界

- Phase-09-01：四个直接模块的 unit/vet/race、日志 sink 有界性、LogMonitor 接收、Router logs 顶层路由、Marshaller handler/offset、Elasticsearch writer/query、`401/403` 前置授权、最小 lifecycle与真实 API纵向闭环。
- Phase-09-02：后台进程接入、queue full/重试/drain、LogMonitor/Router/Kafka/ES/Marshaller故障、同 ID重放、永久异常后继续、Metrics/Logs 混合、日常生命周期和资源安全。
- Phase-09-03：最终构建的阶段主矩阵、分页/过滤、索引隔离、敏感信息、业务/权限/内部访问、资源清理、版本/分支和远程状态；未变化的组件正确性结果直接引用。

### 15.3 阶段级封闭端到端矩阵

1. **真实 API 日志**：管理员登录后执行发帖或评论，使用响应 `X-Request-ID` 查询到同请求的唯一 HTTP 完成记录和对应业务成功记录；字段、级别、route template、状态及资源 ID正确。
2. **真实后台日志**：真实业务事件经 Outbox/RabbitMQ 被 Business Worker 或 Search Indexer 处理，按 `event_id` 查询到发布与处理记录；执行一次 search-reindex 查询到安全 lifecycle/result 记录。
3. **完整传输事实**：捕获同一 message ID的 LogMonitor `202`、Router/Kafka record metadata、Marshaller commit 和 Elasticsearch `_id`/查询时间窗，证明没有绕过任一组件。
4. **查询授权**：未登录 `401`、普通用户 `403`、admin `200`；前两者 repository 调用为零，Cookie/JWT 不能调用 LogMonitor、Router、Marshaller或 Elasticsearch。
5. **查询限制**：默认/最大时间范围、精确 filter、limit、PIT/search_after、游标签名/过期/篡改、未知参数和空结果均符合契约，响应不含内部字段。
6. **索引隔离**：日志只进入 `gopulse-logs-v1-*` 并经固定 alias 查询；帖子 search alias/count/content 不变，日志查询无法指定帖子索引，search/reindex 无法写日志索引。
7. **永久异常继续**：接收层无效输入不进入 Router；Kafka 中一个代表性无效 logs Envelope 不写 ES但被安全提交，随后真实合法日志继续。
8. **短时故障恢复**：分别选择 LogMonitor/Router、Kafka和 Elasticsearch 的代表性故障窗口，证明业务继续、未接受日志保留在有界队列或未提交 offset，恢复后无需手工修复即可查询。
9. **重复与重放**：源端超时重试或 fixture 原样重放同一 key/value，Elasticsearch 只保留同一 `_id` 的一个文档；不同 message ID的真实不同日志仍分别保留。
10. **Metrics 并存**：日志前后均有真实 Redis metrics 被消费并写 VM；logs 不进入 VM，metrics 不进入日志索引；永久异常与恢复不放宽 Phase 8 validator。
11. **敏感信息**：在用户名、密码、Cookie/JWT、帖子/评论、搜索词、内部 URL和路径中放置哨兵，stdout 既有日志、LogMonitor/Marshaller拒绝路径、ES `_source`、Backend响应和验收输出均不泄漏。
12. **业务隔离**：可观测组件故障期间，代表性注册/登录/帖子/评论/点赞及 RabbitMQ必要流程保持原结果；仅 Elasticsearch 故障保留 Phase 3 已有 readiness/search退化，不得新增其他业务依赖。
13. **资源隔离**：正常、失败、signal和脚本中断路径不误杀日常进程、不删除非归属 container/network/volume、不遗留随机端口、Kafka group、PIT、临时 token、插件根或日志文件。

以上是封闭矩阵。除非真实失败证明当前契约不足，不追加性能压测、长时容量、全文搜索、聚合、ILM、多 Topic、多 consumer group、磁盘 spool、TLS或 Frontend。

## 16. CI 与固定完成门槛

CI 保留现有 Backend、Monitor、Router、Marshaller、Exporter、Frontend、Scripts/Compose 和 Integration jobs，并增加 `verify-logs.sh` 的独立 Logs pipeline job或等价固定步骤。模块 job 负责严格 validator、HTTP、handler、writer、offset 和授权单元/集成测试；Logs job 负责真实 API → ES → admin 查询的代表性纵向证据。现有 `verify-marshaller.sh` 继续保护真实 Metrics，不得被 Logs 验收替代。

阶段最终固定门槛至少包括：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-logs.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

未改变的 package 或 Phase-09-01/02 已记录故障结果可以按实施记录引用；Phase-09-03 必须实际完成阶段主路径、权限、索引隔离、Metrics 并存、代表性故障窗口、业务隔离和资源快照。完整验收只在 WSL2 Linux filesystem、真实 Kafka/Elasticsearch/VictoriaMetrics 和强归属隔离资源执行；mock、静态 Envelope 或直接写 ES 不能替代阶段主证据。

## 17. 实施记录规则

每批在提交前创建：

```text
dev/logs/Phase-09/Phase-09-01-Backend日志端到端查询闭环.md
dev/logs/Phase-09/Phase-09-02-后台日志可靠投递与故障恢复闭环.md
dev/logs/Phase-09/Phase-09-03-集成验收与阶段收口.md
```

每份记录必须包含：

- 实际完成行为和实际变更文件。
- 实际执行的验证命令、WSL2/依赖环境、结果和必要有限输出摘要。
- request ID、event ID、message ID、Kafka partition/offset、ES index/_id 和查询时间窗等不含敏感值的证据。
- 相对方案的偏差、实施中真实失败及其最小修复。
- 未完成限制、日志可能丢失边界、后续事项、Pull Request、远程 checks 和合入状态。

规划阶段不创建空实施记录，不得把计划命令写成已通过，也不得在未观察 push、远程 checks 或合入时把批次/阶段标记为完成。

## 18. Phase 9 验收、完成与后续交接

### 18.1 阶段验收标准

- 四类 Phase 4 日志源保留 stdout Schema v1，并通过非阻塞、有界、可取消的 Push 接入 LogMonitor；远程故障不改变业务控制流。
- LogMonitor 使用专用写身份，完成严格第一次清洗和确定性 logs Envelope；Router 只做顶层识别并把原始 bytes 写入固定 Topic。
- Marshaller 对 metrics/logs 使用独立严格 handler，继承 Phase 8 ownership/commit/replay语义；logs 成功写 ES后才提交，永久异常不写存储且不阻断后续记录。
- 日志文档以 message ID作为 `_id` 幂等写入 `gopulse-logs-v1-*`，严格 mapping 和固定 read alias 与帖子索引完全隔离。
- 管理员可按受限时间、精确条件和签名游标查询安全字段；未登录 `401`、普通用户 `403` 且拒绝请求不触达 Elasticsearch。
- 真实 API request ID与后台 event ID均能通过完整链路查询；相同 message ID重放无重复文档。
- Metrics/Logs 同时运行且不互写存储；短时 LogMonitor/Router/Kafka/ES故障可恢复，已接受 Kafka记录不因重启丢失。
- 内部组件没有浏览器入口，token/Cookie/JWT不能越界复用，ES与 API响应无敏感数据或任意内部字段。
- 可观测故障不破坏非搜索社交 API、RabbitMQ必要流程或既有授权；日常与隔离生命周期不误杀、不误删、不泄密、不遗留资源。
- 三批实施记录真实完整，固定本地/远程门禁通过，根与 Frontend 版本均为 `1.6.3`。

### 18.2 完成与停止条件

只有第 18.1 节全部满足、Phase-09-03 Pull Request 已合入主远程 `main`、远程固定门禁成功且三份实施记录与真实提交一致，Phase 9 才完成。任一真实日志源、LogMonitor/Router/Kafka/Marshaller/ES 环节、admin 授权、索引隔离、重复重放、Metrics并存、业务隔离或资源安全证据缺失时，不得标记完成。

达到条件后立即停止。Frontend 日志页、告警、全文分析、聚合、ILM、磁盘 spool、Topic 拆分、容量与生产安全加固记录为后续，不继续占用 Phase 9。

### 18.3 Phase 10 与 Phase 11 交接

向 Phase 10 交付：

- Monitor 内已验证的被动接收、专用服务身份、严格输入和 Router Publisher 模式，可复用于 EventMonitor，但 events 必须有独立 payload validator。
- Router 已支持多 `type/source` 显式组合并保持单 Topic原始 bytes 契约。
- Marshaller 公共 Envelope + typed handler registry、Elasticsearch writer/index template 模式和既有 offset状态机；events 不得放宽 logs/metrics 契约。
- 单 Topic故障可能造成跨类型排队的明确限制，Phase 10 不得无证据静默迁移 group或拆 Topic。

向 Phase 11 交付：

- Backend `GET /api/v1/observability/logs` 的实时 admin授权、受限 filters、签名 cursor和安全 DTO。
- 固定日志 read alias与 index隔离；Frontend 只能调用 Backend，不得直连 Elasticsearch、Monitor、Router、Kafka或 Marshaller。
- stdout-before-`202` best-effort 与 Kafka-after-`202` at-least-once 的准确交付边界，UI不得把缺失日志解释为业务事实不存在。

Phase 9 只是 Milestone 3 的第一阶段，不单独宣称“完整可观测 MVP”完成；必须等待 Phase 10 Events 与 Phase 11 统一管理员前端全部验收通过。
