# Phase 8：Marshaller 与 VictoriaMetrics 总实施方案

## 1. 实施目标

在 Phase 7 已交付 Message Router、Kafka、固定 metrics Topic 和原始 Envelope v1 record 契约的基础上，交付可独立运行的 Marshaller，并通过单节点 VictoriaMetrics 完成第一条真实指标采集、传输、转换、存储和查询闭环，收尾 Milestone 2 指标采集 MVP。

本阶段固定形成以下可独立验证的最小闭环：

```text
真实 Redis → Redis Exporter → MetricsMonitor 周期采集
          → Message Router → gopulse-observability-v1
          → Marshaller 正式 Consumer group
          → Prometheus text import → VictoriaMetrics
          → 受控内部 MetricsQL/PromQL 查询
```

阶段完成必须同时证明：Marshaller 消费真实 Router record，而非绕过 Kafka；合法 `metrics/redis` Envelope 被严格第二次校验并确定性转换；Marshaller 只在 VictoriaMetrics HTTP 接受导入且当前 partition ownership 仍有效后推进对应 Kafka offset，并以查询和 invalid-row 指标证明代表性存储结果；异常消息不会永久阻断同分区后续合法消息；VictoriaMetrics 故障保留未确认 offset 并在恢复后继续；查询能够看到真实连接、请求、状态等 Redis 指标；整个可观测数据面不成为浏览器或普通用户入口，也不改变社交业务结果。只完成 JSON 解析、手工向 VictoriaMetrics 写入、直接向 Kafka produce 静态 fixture、只证明容器健康，或只执行单元测试，均不构成阶段完成。

## 2. 当前真实基线与规划输入

本方案在 2026-09-04 重新核对主远程后，当前真实产品基线为 `upstream/main` 提交 `60f9aa8`，根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 均为 `1.4.3`。Phase 7 的三个批次已经完成并合入：PR #69 完成原阶段收口，PR #70 修复远程 Integration migration 启动期瞬态失败重试，PR #71 关闭 Phase 7 Review 的严格 schema token、Producer 有界并发/取消、验收矩阵和资源安全问题。最新整改没有改变 Phase 8 的 Topic、record key/value 或 Envelope 交接契约：

- `monitor` 已是独立 Go 1.26 module，可从真实 Redis Exporter 周期采集并生成 Envelope v1。
- 当前 Envelope 固定包含 `schema_version`、`message_id`、`type`、`source`、`timestamp` 和 `payload`；生产消息固定为 `schema_version=1`、`type=metrics`、`source=redis`。
- payload 固定包含 `plugin_id`、`plugin_version`、`target_id`、`scrape_status` 和 `samples`；sample 固定包含 `name`、`kind`、`labels` 和 `value`。
- MetricsMonitor 已严格限制 Redis 指标 family、gauge/counter 类型、标签、有限数值、样本数量和排序；`success` 包含完整指标集合，`target_unavailable` 只包含 `gopulse_redis_up=0`。
- `router` 已是独立 Go 1.26 module，固定使用 `github.com/twmb/franz-go v1.21.0`；Topic 为 `gopulse-observability-v1`，record key 为 `message_id`，value 为 Router 收到的原始 Envelope JSON bytes，Router 不做 payload 清洗或重新序列化。
- Router 只有在 Kafka delivery callback 成功后返回 `202`，请求超时仍可能形成不确定写入；Phase 7 最终记录明确同一 `message_id` 可能形成重复 record。
- `deploy/compose.yaml` 已包含 `apache/kafka:4.3.1` 单节点 KRaft、显式 Topic 初始化、loopback 端口和独立 `kafka_data` volume；Broker 禁止自动建 Topic，当前 Topic 为 1 partition / replication factor 1。
- `marshaller/` 仍只有占位 README，`deploy/compose.yaml` 尚无 VictoriaMetrics；正式 Consumer、第二次校验、指标转换和存储仍全部属于 Phase 8。
- 日常 Bash 生命周期已经把 Kafka、Router、Monitor/Exporter 纳入进程、端口、Compose project、container、network、volume 和插件根强归属边界；Phase 8 必须在此基础上增加 Marshaller/VictoriaMetrics，不能建立弱化的停止或清理路径。
- Backend 已有数据库实时 `admin` 授权，但本阶段不需要新增产品查询 API；Phase 11 才交付管理员可观测页面，Phase 9/10 会继续扩展 Marshaller 数据类型。

因此，本文关于 Kafka、Router 和 record 的内容以已完成的 Phase 7 真实实现和三份最终实施记录为输入。Phase-08-01 开工时仍必须 fetch 最新主远程，并重新核对 Phase 7 实施记录、真实 Router/验证 Consumer 代码、Kafka 镜像与 Topic 参数；若 `60f9aa8` 之后的主远程改变 Topic、record key/value、Envelope、Kafka 客户端或故障语义，必须先同步更新本总方案和所有尚未开始的拆分方案，不得靠兼容猜测开工。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 7 两个批次全部合入主远程 `main`，远程固定门禁成功，实施记录与真实提交一致。
- 根与 Frontend 版本均为 `1.4.3`，Phase-07-03 已以 PR #71 合入，真实 Redis → Exporter → MetricsMonitor → Router → Kafka Consumer 链路已经通过。
- Kafka Topic、record key/value、Envelope v1、潜在重复及 Router/Kafka 恢复语义均有真实验收证据。
- 实施环境仍固定为 Windows 宿主上的 WSL2 Linux filesystem；Bash 是唯一维护的本地生命周期和验收入口。
- 每批开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立 `develop/x.x.x` 分支，不沿用 `update`、Phase 7 或已完成分支。
- 开始前保存 Git 状态、日常 Compose project/volume、`.run` 进程、端口和插件根快照，不停止、删除、暂存或提交其他任务资源。

### 3.2 权威批次、版本与开发分支

Phase 8 使用 `1.5.x` 版本线，`1.5.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-08-01 | `1.5.1` | `develop/1.5.1` | 未开始 |
| Phase-08-02 | `1.5.2` | `develop/1.5.2` | 未开始 |
| Phase-08-03 | `1.5.3` | `develop/1.5.3` | 未开始 |

执行规则：

- 同一批次全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-08/Phase-08-XX-*.md`，只记录实际改动、验证、偏差、失败和限制。
- Phase-08-01 交付可查询且可安全合入的最小真实纵向闭环：正式 Consumer、严格第二次校验、确定性指标转换、VictoriaMetrics 基本写入/查询、generation ownership fencing、安全 commit 和最小生命周期；不把真实上游、手动 offset、永久异常继续或消费正确性推迟到后续批次。
- Phase-08-02 是可独立合入的第二实现批次：在已正确的 ownership/commit 基线上完成真实 rebalance、Kafka/VM/进程恢复、确定性重放和日常运维生命周期，不执行完整社交业务收口。
- Phase-08-03 只在已合入的最终能力上执行跨组件矩阵、业务/访问隔离、资源安全、文档、版本和 Milestone 2 远程收口；除真实复现的阻断问题外不增加产品能力。
- 已推送分支不得静默改名或重新编号；若批次数量或顺序在实施前变化，先更新本表并重新计算尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 独立 `marshaller` Go module、常驻 Marshaller 进程、结构化日志、健康/就绪接口和有界退出。
- `gopulse-observability-v1` 的正式 Kafka consumer group、手动 offset 提交、有限 poll/处理边界和 at-least-once 恢复语义。
- Kafka record key 与 Envelope `message_id` 一致性、Envelope v1 顶层及完整 metrics payload 的第二次严格校验。
- `metrics/redis` 到 Prometheus text import 的确定性字段映射、标签转义、时间戳转换、异常样本/消息过滤和固定输出上限。
- 固定版本的单节点 VictoriaMetrics、持久 volume、loopback 发布、内部 Basic 身份、健康检查和重复样本去重窗口。
- 只有封闭校验/转换完成、VictoriaMetrics HTTP 接受导入且 partition ownership 仍有效后才提交合法 record offset；永久无效 record 被安全记录、跳过并提交，存储临时故障、commit 失败或 lost ownership 不提交。
- 通过 VictoriaMetrics 受控查询接口验证 `up`、连接数、命令请求总数、CPU、内存和 keyspace 等真实 Redis 指标。
- Kafka、Marshaller、VictoriaMetrics、Monitor/Router 的 Bash 启停、只读验证、隔离端到端验收和安全资源清理。
- Marshaller unit/race、真实 Kafka/VictoriaMetrics 集成、CI 门禁、README、版本元数据和实施记录。

### 4.2 明确不做

- 不实现 LogMonitor、EventMonitor、logs/events Envelope、Elasticsearch 日志/事件写入或额外 Topic。
- 不实现 Backend Metrics Query API、Frontend 指标页面、Dashboard 或普通用户/管理员浏览器直连 VictoriaMetrics；这些产品入口留给 Phase 11。
- 不计算 rate、ratio、聚合、派生指标、录制规则、降采样、告警或长期容量/保留策略。
- 不建设 Schema Registry、DLQ Topic、重放 API、人工消费管理 UI、offset 管理 API或消息审计数据库。
- 不宣称端到端 exactly-once，不引入 Kafka transaction、分布式事务或本地持久去重数据库。
- 不把 `message_id`、`plugin_version`、Kafka partition/offset 或错误文本写成 VictoriaMetrics 标签，避免高基数与版本升级造成时序膨胀。
- 不实现 VictoriaMetrics cluster、vmagent、vmauth、多租户、远程存储、高可用、TLS 或公网暴露；本地阶段使用受控 loopback 和内部 Basic 身份。
- 不把 VictoriaMetrics 加入 Backend readiness，不让 Kafka/Marshaller/存储故障改变已成立的社交 API 响应。
- 不创建 Marshaller 应用容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 组件与运行架构

### 5.1 独立 Marshaller module

`marshaller` 建立独立 Go module，建议结构：

```text
marshaller/
├── go.mod
├── go.sum
├── cmd/marshaller/
├── internal/config/
├── internal/envelope/
├── internal/consumer/
├── internal/metrics/
│   ├── validate/
│   └── transform/
├── internal/victoriametrics/
├── internal/httpserver/
└── README.md
```

- module path 固定为 `github.com/Ray-ymq/GoPulse/marshaller`，Go 版本与实施时仓库基线一致。
- Kafka 客户端优先与 Phase 7 Router 使用同一 `github.com/twmb/franz-go` 已锁定版本；开工时以 Phase 7 实际 `router/go.mod` 为权威，不在文档阶段猜测升级。
- 不新增根 `go.work`，不导入 `monitor/internal/*`、`router/internal/*`、`backend/internal/*` 或 Exporter internal 包；跨进程契约只通过 Kafka record、HTTP 和 JSON 表达。
- Consumer、Envelope Decoder/Validator、Transformer、VictoriaMetrics Writer、offset Committer 和 partition ownership lease 通过最小接口解耦，使坏消息、写入失败、提交失败、rebalance/lost ownership 和取消可在不启动全部基础设施时定向且确定性测试；这些测试接口不得形成生产 HTTP 故障开关。
- 复用项目 Schema v1 结构化日志字段语义但不跨 module 导入 internal 实现；固定 `service=marshaller`，module 至少包含 `lifecycle`、`consumer`、`transform`、`storage` 和 `http`。

### 5.2 职责与所有权

- MetricsMonitor 继续负责采集、第一次基础校验、基础结构化、Envelope 封装和 HTTP 发布；不理解 Kafka offset 或 VictoriaMetrics 行格式。
- Message Router 继续只校验路由所需顶层字段、选择 Topic 并保持原始 bytes；不解析 metrics payload，不写存储。
- Kafka 保存可观测传输事实；不理解指标、不参与用户认证，也不替代 RabbitMQ 业务任务。
- Marshaller 是 metrics record 的第二次处理所有者：完整解码、严格契约校验、字段/标签映射、目标格式构造、写入确认和 offset 决策。
- VictoriaMetrics 只保存可查询指标时序；不保存 Envelope、`message_id`、Kafka metadata、原始 JSON 或错误记录。
- Phase 8 查询是受控内部验收能力，不形成 Backend/Frontend 产品 API。后续 Phase 11 必须经 Backend 实时 admin 授权代理，浏览器不能复用 Phase 8 内部凭据。

### 5.3 启动与关闭顺序

日常启动顺序固定为：

```text
Compose 基础设施（含 Kafka、VictoriaMetrics）
→ 显式核对 gopulse-observability-v1
→ Message Router
→ Marshaller（正式 consumer group）
→ Monitor 与 Redis Exporter
→ Backend / Workers / Frontend
```

关闭顺序固定为：

```text
Monitor / Exporter（停止产生新 metrics）
→ Marshaller（停止 poll，完成或取消当前有界写入并提交已确认 offset）
→ Router（完成在途 produce）
→ Backend / Workers / Frontend
→ Compose 基础设施
```

- 配置合法时 Marshaller 可以在 Kafka或 VictoriaMetrics 暂不可用时保持进程存活；`/health` 只表达进程存活，`/ready` 反映两项依赖和 Topic 是否可用。
- 依赖恢复后 Marshaller 必须自动重新连接和继续消费，无需重启 Monitor、Router 或 Marshaller。
- Backend readiness、业务 worker 和社交 API 不依赖 Marshaller/VictoriaMetrics readiness。

## 6. Marshaller HTTP、内部身份与日志契约

### 6.1 Marshaller HTTP 接口

| Method | Path | 身份 | 语义 | 成功状态 |
| --- | --- | --- | --- | --- |
| `GET` | `/health` | 无，但只允许受控监听 | 仅表达 Marshaller 进程存活 | `200` |
| `GET` | `/ready` | Marshaller Bearer token | 有界检查 Kafka/Topic 与 VictoriaMetrics 可用 | `200` |

- `MARSHALLER_API_TOKEN` 必须至少 32 bytes 且不得包含 CR/LF；Bearer 使用常量时间比较。
- Cookie、JWT、普通用户/admin 会话、query token 和浏览器 Origin 均不能替代内部服务身份。
- `/health` 不访问 Kafka 或 VictoriaMetrics，固定返回 `{"status":"ok","service":"marshaller"}`。
- `/ready` 在任一依赖不可用时返回 `503` 和有限组件状态，不返回 broker、Topic metadata、VictoriaMetrics URL、凭据、offset 或底层错误。
- Phase 8 不提供接收消息、手工重放、查询指标、改变 offset 或管理 consumer group 的 HTTP 接口。

### 6.2 VictoriaMetrics 内部身份

- 单节点 VictoriaMetrics 使用固定内部 Basic Auth username 和至少 32 bytes password；Marshaller 写入与隔离验收查询使用该内部身份。
- 这是本地 MVP 的单一内部数据面身份，不授予 Backend、Frontend、用户 Cookie 或浏览器；日志、进程输出、HTTP 错误和验收证据不得打印 password 或 Authorization header。
- VictoriaMetrics 端口只发布到 `127.0.0.1:${VICTORIAMETRICS_PORT}`；非本机部署时只能通过内部网络访问。
- Phase 8 不建立读写分权代理。Phase 11 若提供产品查询，必须由 Backend 使用独立受限服务身份并执行数据库实时 admin 授权；届时不能把本阶段 Basic 凭据发送给浏览器。

### 6.3 安全日志

- Marshaller 日志只记录固定 reason code、message ID 的有限相关性摘要、Topic 固定名称、partition、offset、attempt 和状态转换；不得记录完整 record value、指标标签值集合或导入正文。
- 不输出 Kafka broker URL、VictoriaMetrics URL/凭据、token、Cookie/JWT、用户内容、服务器绝对路径、调用栈或未经清洗的客户端错误。
- 对单条永久无效消息记录一次有限 `message_rejected`；同一存储故障按状态变化或受控节流记录，避免每次退避形成日志风暴。

## 7. Kafka Consumer、offset 与交付语义

### 7.1 固定消费契约

| 项目 | 固定值/规则 |
| --- | --- |
| Topic | `gopulse-observability-v1` |
| Consumer group | `gopulse-marshaller-metrics-v1` |
| 初次无 committed offset | 从 `earliest` 开始，避免部署晚于生产端时丢失已有消息 |
| 自动提交 | 禁止 |
| 处理顺序 | 每 partition 顺序处理；首版本地 Topic 为 1 partition |
| 最大 record bytes | 不高于 Phase 7 Router 的 1 MiB 上限，并在 Consumer 再次强制 |
| offset 提交 | 合法消息写入确认后提交；永久无效消息记录并跳过后提交 |

- 不复用 Phase 7 验收 Consumer 的 group ID、offset 或临时读取实现。
- record key 必须存在、为 32 位小写十六进制且逐字等于 Envelope `message_id`；不一致是永久无效消息。
- Consumer 不依赖 Kafka record timestamp 作为业务时间；唯一业务时间为 Envelope `timestamp`。
- 一个进程同一 partition 只处理一条在途 record，不建立额外无界 channel、goroutine fan-out 或本地磁盘 spool。
- commit 失败不能伪装成功；当前 record 允许在 rebalance/重启后重放，写入转换必须因此保持确定性。
- 每个在途 record 必须绑定当前 partition assignment 的 ownership lease。`OnPartitionsRevoked` 或 `OnPartitionsLost` 发生时立即取消对应写入、退避和提交；失去 ownership 后即使 VictoriaMetrics 随后返回成功也不得提交旧 generation 的 offset，`OnPartitionsLost` 路径始终禁止提交。
- 不得用 `BlockRebalanceOnPoll` 跨越 VictoriaMetrics 写入或无限退避。实现必须选择能够在存储故障期间及时让出 rebalance 的 poll/pause/ownership 方案，并保证只从最后 committed offset 重新取得未确认 record；不得为了维持 poll 而丢弃客户端已缓冲但未处理的后续 record。
- 收到 VictoriaMetrics HTTP acceptance 后先重新确认 ownership lease 仍有效，再同步、有界提交当前 record；commit 失败、ownership 已失效或结果不确定时停止推进该 partition，触发安全重取并允许确定性重放，不继续处理或提交其后的 record。

### 7.2 三类处理结果

| 结果 | 代表条件 | VictoriaMetrics | Kafka offset |
| --- | --- | --- | --- |
| 成功 | record、Envelope、payload、映射均合法且 VictoriaMetrics HTTP 接受导入 | HTTP 请求已接受；代表性存储结果由查询与 invalid-row 指标验证 | ownership 有效时同步/有界提交 |
| 永久无效 | 超限、坏 JSON、未知字段、key/ID 不符、不支持 schema/type/source、非法 payload/sample | 不调用写入端 | 记录有限拒绝原因后提交，继续后续消息 |
| 暂时失败 | Kafka 读取/提交失败、VictoriaMetrics 网络/超时/非成功响应 | 未确认或结果不确定 | 不提交；有界退避后重试或由重启重放 |

- Phase 8 不创建 DLQ。永久无效消息的跳过事实以安全结构化日志和验收统计表达；原始 payload 不落盘。
- 暂时失败采用带抖动的指数退避，默认从 `500ms` 增长到 `30s` 上限；每次 HTTP 尝试仍受独立 timeout 约束。退避次数可以持续到依赖恢复，但内存中只保留当前 record，不形成无界队列。
- 收到 shutdown 时停止新的 poll，取消退避/在途请求；只提交已经收到 VictoriaMetrics HTTP acceptance、ownership 仍有效，或已经完成永久拒绝分类的 offset。
- franz-go 的 poll、pause、rebalance callback、ownership lease 和 commit 组合必须以当前锁定版本的公共 API 为准；定向测试至少覆盖处理期间 revoke、lost、HTTP acceptance 与 revoke 竞态、commit 失败和 broker 重启后从 committed offset 重取。

## 8. Envelope v1 第二次校验

Marshaller 不能因为 Monitor 和 Router 已校验就信任 Kafka bytes。record value 必须是唯一 JSON object、有效 UTF-8、无尾随 token。实现必须先用 `json.Decoder.Token` 等等价词法扫描递归检查**每一层 JSON object**（顶层、payload、每个 sample 和 labels）的重复 key，再用启用 `DisallowUnknownFields` 的 typed decoder 检查有固定 schema 的 object；只使用普通 Go struct 反序列化不满足本契约，因为 `encoding/json` 默认接受重复 key 和未知字段。

### 8.1 顶层字段

- `schema_version` 必须是整数 `1`。
- `message_id` 必须是 32 位小写十六进制，并与 record key 完全一致。
- `type` 固定为 `metrics`，`source` 固定为 `redis`；其他 schema/type/source 当前均永久拒绝，不误写成 Redis 指标。
- `timestamp` 必须是 UTC RFC3339Nano，可转换到 Unix 毫秒，且不得晚于 Marshaller 当前时间的允许偏差，默认最多超前 5 分钟。合法历史消息不得仅因 Kafka/存储故障造成的积压而永久拒绝；其最终可写范围由 Kafka 与 VictoriaMetrics 实际保留窗口决定。
- `payload` 必须是非 null object；完整 JSON bytes 上限为 1 MiB。

### 8.2 payload 与 sample

- payload 必须且只能包含 `plugin_id`、`plugin_version`、`target_id`、`scrape_status` 和 `samples`。
- 当前固定接受 `plugin_id=redis-exporter`、三段稳定 SemVer `plugin_version`、`target_id=redis-exporter-local`。
- `scrape_status` 只能是 `success` 或 `target_unavailable`。
- samples 必须是非 null、非空数组；`success` 固定 11 项，`target_unavailable` 固定 1 项，且始终不超过 1024 项。每个 sample 必须且只能包含 `name`、`kind`、`labels`、`value`，其中 labels 必须是非 null object。
- Phase 8 的完整 Redis 指标契约固定如下，不以跨文档的“完整 family”表述替代：

| metric family | kind | 唯一允许标签 | `success` sample 数 |
| --- | --- | --- | --- |
| `gopulse_redis_up` | `gauge` | 无 | 1，值必须为 `1` |
| `gopulse_redis_uptime_seconds` | `gauge` | 无 | 1 |
| `gopulse_redis_connected_clients` | `gauge` | 无 | 1 |
| `gopulse_redis_used_memory_bytes` | `gauge` | 无 | 1 |
| `gopulse_redis_commands_processed_total` | `counter` | 无 | 1 |
| `gopulse_redis_keyspace_hits_total` | `counter` | 无 | 1 |
| `gopulse_redis_keyspace_misses_total` | `counter` | 无 | 1 |
| `gopulse_redis_cpu_seconds_total` | `counter` | `mode` | 2，`user`、`system` 各 1 |
| `gopulse_redis_db_keys` | `gauge` | `db` | 1 |
| `gopulse_redis_db_expiring_keys` | `gauge` | `db` | 1 |

- `mode` 只能是 `user|system`；`db` 必须是可由 `strconv.ParseUint(value, 10, 31)` 接受的十进制字符串，并且两个 keyspace family 的 `db` 完全相同。
- value 必须是可解析为有限 `float64` 的 JSON number；counter 不得为负。名称最长 128 bytes、标签 value 最长 256 bytes，每个 sample 最多 16 个标签；更严的 family 标签白名单仍适用。
- `target_unavailable` 必须只包含唯一无标签 gauge `gopulse_redis_up=0`。
- 拒绝重复 `name+规范化 labels`、未知 family/kind/label、缺失/额外样本、`NaN`/`Inf` 表达、标签冲突和不符合状态的部分数据。

第二次校验可以在独立 module 中重复表达固定公共契约，但不得通过导入 Monitor internal 包形成编译耦合。后续 Envelope 版本必须显式新增 decoder/validator，不允许宽松接受未知字段以“向前兼容”。

## 9. 指标映射与 VictoriaMetrics 写入契约

### 9.1 时序映射

每个合法 sample 确定性映射为一条 Prometheus text exposition import 行：

```text
<sample.name>{source="redis",target_id="redis-exporter-local",<sorted sample labels>} <finite value> <envelope timestamp unix_ms>
```

固定规则：

- metric name 原样保留，例如 `gopulse_redis_up`、`gopulse_redis_connected_clients`、`gopulse_redis_commands_processed_total`。
- 固定增加低基数 provenance 标签 `source` 和 `target_id`；sample 自带标签按 key 排序后追加。若上游 label 与保留标签冲突则整条消息永久拒绝。
- 不增加 `message_id`、`plugin_id`、`plugin_version`、`scrape_status`、Kafka partition/offset 或 kind 标签；指标类型由名称/查询语义表达，VictoriaMetrics 不保存 Prometheus metadata。
- label name/value 使用 Prometheus text 规则转义；数值固定使用 Go `strconv.FormatFloat(value, 'g', -1, 64)`，允许该函数为极大/极小有限值选择科学计数法。禁止 locale、自定义精度或 map 遍历顺序改变重放输出。
- Envelope RFC3339Nano 时间向 Unix 毫秒转换；同一 Envelope 的全部样本使用完全相同时间戳。纳秒被截断到毫秒的行为必须有单元测试和 README 说明。
- 一个 Envelope 形成一个有界导入请求，按 canonical sample key 稳定排序，每行以 `\n` 结束；输出上限固定为 2 MiB，超限在调用存储前永久拒绝。

### 9.2 写入 HTTP

- 单节点写入路径固定为 `POST /api/v1/import/prometheus`，`Content-Type` 固定为 `text/plain; version=0.0.4; charset=utf-8`，携带内部 Basic Auth。
- 默认写入 timeout 为 `3s`，允许配置 `100ms..30s`；请求正文、响应正文读取和连接复用均有固定上限。
- 对该非 Pushgateway 路径，运行时只把 `204 No Content` 且空 body 视为 HTTP transport acceptance；任一网络错误、timeout、认证失败、redirect、其他状态或非空/无法完整读取的响应都不得提交 Kafka offset。
- VictoriaMetrics import API 以流式方式处理数据，官方文档明确其可能不把逐行解析错误返回客户端，因此 `204` 不得表述为逐 sample 持久化证明。Phase 8 的 offset 决策依赖封闭白名单、写入前完整校验和确定性生成保证正文合法；真实验收还必须在本批窗口前后核对 `vm_rows_invalid_total` 未增加，并查询全部预期时序。参考：<https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#how-to-import-data>。
- 写入客户端不跟随跨 host redirect，不接受 URL credentials/query/fragment，不在错误中暴露响应 body；base URL 只能来自受信配置。
- 不在 Marshaller 外建立后台批处理队列。Phase 8 的原子处理单位是单个 Envelope，而不是跨 record 批量写入。

### 9.3 重复投递与幂等边界

- Kafka→Marshaller→VictoriaMetrics 的交付语义是 at-least-once，不是 exactly-once。
- 相同 record 重放必须生成相同 metric name、labels、value 和 Unix 毫秒 timestamp；`message_id` 不进入时序标签。
- 单节点 VictoriaMetrics 固定启用 `-dedup.minScrapeInterval=1ms`，用于折叠同一时序在相同毫秒的确定性重复样本。正常 Monitor 周期远大于 1ms；验收必须证明两次投递同一 Envelope 不产生双倍查询点。
- 该存储去重不解决不同 `message_id` 但业务值相同、时间不同的重复采集，也不能把 HTTP 写入和 Kafka commit 组合为事务；文档、日志和实施记录不得宣称 exactly-once。

## 10. 查询契约与可验证指标

### 10.1 内部查询入口

- Phase 8 只使用单节点 VictoriaMetrics 的 Prometheus-compatible 查询接口：即时查询 `POST /prometheus/api/v1/query`，范围查询 `POST /prometheus/api/v1/query_range`。
- 查询由 `scripts/verify-marshaller.sh` 或运维人员在受控本机环境使用内部 Basic Auth 发起；不向浏览器返回 URL 或凭据。
- 查询必须带固定、受控表达式和时间窗口；验收脚本不接受用户提供的任意 MetricsQL，不成为通用代理。
- 本阶段不新增 Backend `/api/v1/metrics*`。未来若添加，必须由 Backend 先执行现有数据库实时 admin 中间件：未登录 `401 authentication_required`，普通用户 `403 permission_denied`，且请求被拒绝时不触达 VictoriaMetrics。

### 10.2 固定验收查询

至少验证以下真实时序，全部带 `source="redis"` 和 `target_id="redis-exporter-local"`：

- `gopulse_redis_up`：正常为 1，停止 Redis 后为 0，恢复后重新为 1。
- `gopulse_redis_connected_clients`：与同一隔离 Redis 的当前连接事实一致。
- `gopulse_redis_commands_processed_total`：执行代表性 Redis 命令后增加。
- `gopulse_redis_cpu_seconds_total{mode="user|system"}`：两条合法 counter 时序可查询。
- `gopulse_redis_used_memory_bytes`：有限非负 gauge 可查询。
- `gopulse_redis_db_keys{db="<configured>"}` 与 `gopulse_redis_db_expiring_keys`：写入/过期 key 后与 Redis `INFO keyspace` 对应。

查询只证明实际采集、转换和存储，不把瞬时值硬编码成跨环境常量。验收通过 message ID 对应的 Envelope timestamp 建立窄时间窗，避免把旧 volume 数据误判为当前运行结果。

## 11. VictoriaMetrics Compose 与配置

### 11.1 固定本地服务

- `deploy/compose.yaml` 增加 `victoriametrics/victoria-metrics:v1.151.0` 单节点服务；该版本是方案编写时已核对的最新稳定版，实施开始前必须确认镜像仍可解析且未被项目安全基线替代，任何升级都需更新方案/锁定值和实施记录。
- HTTP 只发布到 `127.0.0.1:${VICTORIAMETRICS_PORT}:8428`，数据保存到独立 `victoriametrics_data:/victoria-metrics-data` volume。
- 固定 storage path、Basic Auth 和 `-dedup.minScrapeInterval=1ms`；不修改默认长期 retention、磁盘容量或性能参数。
- 健康检查使用携带内部 Basic Auth 的本地 `/health`，不得把任意查询成功当作容器健康；container、network、volume 和端口继续受 Compose project label 归属保护。

### 11.2 Phase 8 最小配置

```text
VICTORIAMETRICS_PORT=8428
VICTORIAMETRICS_URL=http://127.0.0.1:8428
VICTORIAMETRICS_USERNAME=gopulse-marshaller
VICTORIAMETRICS_PASSWORD=<minimum-32-bytes>

MARSHALLER_HTTP_HOST=127.0.0.1
MARSHALLER_HTTP_PORT=9093
MARSHALLER_API_TOKEN=<minimum-32-bytes>
MARSHALLER_SHUTDOWN_TIMEOUT=10s
MARSHALLER_KAFKA_BROKERS=127.0.0.1:9092
MARSHALLER_KAFKA_TOPIC=gopulse-observability-v1
MARSHALLER_KAFKA_GROUP=gopulse-marshaller-metrics-v1
MARSHALLER_MAX_MESSAGE_BYTES=1048576
MARSHALLER_MAX_OUTPUT_BYTES=2097152
MARSHALLER_WRITE_TIMEOUT=3s
MARSHALLER_RETRY_MIN=500ms
MARSHALLER_RETRY_MAX=30s
MARSHALLER_MAX_FUTURE_SKEW=5m
```

- Marshaller host 必须是 IP，日常默认 loopback；port 为 `1..65535` 且不能与既有服务冲突。
- brokers 是非空、去重、有限 `host:port` 列表；Topic 和 group 固定符合 Kafka 安全命名，不接受消息或 HTTP 请求覆盖。
- message bytes 允许 `1 KiB..1 MiB`，不得高于 Router 限制；output bytes 允许 `1 KiB..2 MiB` 且不小于 message bytes。
- shutdown timeout 允许 `1s..60s`；retry min/max 必须为有限正值、min≤max≤5m；未来时间偏差必须为有限正值且不超过 5m。
- VictoriaMetrics URL 只接受无 credentials/query/fragment 的 HTTP(S) base URL；username/password/token 均拒绝 CR/LF，password/token 至少 32 bytes。
- `.env.example` 只提供开发用示例凭据；真实环境必须更换。配置解析错误在连接或监听前安全退出，不打印完整环境。

## 12. 故障、恢复和安全边界

- **Kafka 暂不可用**：Marshaller `/health=200`、`/ready=503`，持续有界重连；不影响 Backend、RabbitMQ、Monitor 采集或已成立的社交请求。
- **VictoriaMetrics 暂不可用**：当前合法 record 不提交，按上限退避重试；Consumer 不继续吞入无界后续 record。恢复后同一进程继续写入并推进 offset。
- **永久坏消息**：不调用 VictoriaMetrics；以固定 reason code 记录后提交该 record，继续下一条。验收必须在坏消息后放置合法消息证明无永久阻塞。
- **HTTP 接受但 commit 失败**：在 ownership 仍有效时 commit 失败则停止推进并安全重取；record 可重放，确定性输出与 1ms 去重保证查询稳定，但仍记录 at-least-once，不伪造 commit 或逐行持久化成功。
- **进程在写入结果不确定时退出**：不提交，重启后重放；不通过本地文件猜测成功。
- **Consumer rebalance**：revoke/lost 立即取消旧 ownership lease；未完成 record 不提交，旧 generation 不得在延迟响应后提交；重复处理可接受，不能越过前一未确认 offset 提交后续消息。
- **异常时间/高基数字段**：时间窗口、固定 family、标签白名单和保留标签冲突检查在写入前拒绝。
- **内部暴露**：Kafka、Marshaller、VictoriaMetrics 均为 loopback/内部网络；普通/admin Cookie 无权调用 Marshaller，浏览器无底层凭据。
- **业务隔离**：VictoriaMetrics 不进入 Backend readiness，Marshaller 不导入 Backend/RabbitMQ 业务代码；可观测故障不得回滚 MySQL 事实或改变用户响应。

## 13. Bash 生命周期与隔离验收

- `scripts/dev.sh` 将 VictoriaMetrics 加入 Compose 基础设施，等待 Kafka/Topic 与 VictoriaMetrics 健康，构建/启动 Router 和 Marshaller并确认各自 readiness，再启动 Monitor。
- Marshaller PID 记录继续包含 cwd、绝对 executable、start ticks 和 command marker；启动前拒绝占用端口或不匹配遗留记录。
- `scripts/verify.sh` 保持只读：验证 Kafka/VictoriaMetrics container、volume、project、端口，Router/Marshaller/Monitor PID 与 readiness，以及固定指标的有限查询；不得生产测试消息、修改 offset、删除时序或修复资源。
- `scripts/down.sh` 按第 5.3 节顺序停止受管进程，再通过既有 Compose project 停止基础设施；不得按名称单独删除未知 VictoriaMetrics container/volume。
- 新增 `scripts/verify-marshaller.sh`。`--self-test` 只执行无 Docker 的 token、URL、PID、project/container/volume、port、Topic/group、查询表达式白名单和清理目标负向测试。
- 默认验收使用随机独立 Compose project、Kafka/Redis/VictoriaMetrics/Backend/Monitor/Router/Marshaller 端口、数据库、插件根、进程目录、consumer group 和 volume。
- 验收通过受控 Kafka producer fixture 注入坏消息/重复消息仅用于异常边界；阶段成功主链路仍必须由真实 Redis、Exporter、Monitor 和 Router 产生，不能被 fixture 替代。
- 成功、故障、脚本失败和信号中断路径都只清理本次强归属资源，并对比日常栈前后快照。

## 14. 跨批次依赖与摘要

| 批次 | 纵向交付 | 关键输入 | 关键输出 |
| --- | --- | --- | --- |
| Phase-08-01 | Marshaller 与 VictoriaMetrics 最小指标闭环 | Phase 7 Topic/record、Envelope v1、真实 Redis metrics | 正式 Consumer、严格转换、VM 基本写入/查询、generation fencing/安全 commit、最小生命周期与 CI |
| Phase-08-02 | 可靠消费、故障恢复与运维闭环 | 已合入的 `1.5.1` 安全纵向闭环 | 真实 rebalance、Kafka/VM/进程恢复、生命周期与重放证据，`1.5.2` 可靠性基线 |
| Phase-08-03 | 集成验收与 Milestone 2 收口 | 已合入的 `1.5.2` 最终实现能力 | 完整矩阵、业务/访问/资源隔离证据，`1.5.3` 阶段交接 |

- 08-01 必须自身可运行、可查询且消费正确，不能把真实 Monitor 输入、手动 offset、永久异常继续、generation fencing、安全 commit 或 VictoriaMetrics 查询推迟到后续批次。
- 08-02 在不改变映射和存储公共契约的前提下实现真实故障恢复和运维增量；其 rebalance、Kafka/VM/进程恢复和资源归属验收必须在本批通过。
- 08-03 不重新设计映射或状态机，只在最终构建和干净隔离资源上验证跨组件事实；只允许修复已复现的阻断问题。
- Phase 9 只能依赖已记录的 Marshaller 生命周期、扩展边界、metrics 消费/写入并存能力和存储故障语义，不能依赖验收 fixture 的临时 group、凭据或查询实现。

详细方案：

- [Phase-08-01：Marshaller 与 VictoriaMetrics 最小指标闭环](Phase-08-01-Marshaller与VictoriaMetrics指标闭环.md)
- [Phase-08-02：可靠消费、故障恢复与运维闭环](Phase-08-02-可靠消费故障恢复与运维闭环.md)
- [Phase-08-03：集成验收与 Milestone 2 收口](Phase-08-03-集成验收与里程碑收口.md)

## 15. 测试策略与固定验收矩阵

### 15.1 执行效率与停止规则

- 每批首次探索仅限 Marshaller、Phase 7 Kafka record、Monitor Envelope、Compose、Bash 生命周期和直接验收代码；不进行一般依赖审计、VictoriaMetrics 源码阅读或全仓覆盖率活动。
- 单元测试只保护新公共契约和失败边界；一个代表性成功与一个代表性失败优先放在最低有效层，不跨 unit/integration/end-to-end 重复证明相同行为。
- 验证按 Marshaller package → fake writer/consumer → 真实 Kafka+VictoriaMetrics → 真实全链路逐级执行；只有具体失败或共享基础设施风险才扩大范围并记录原因。
- 已成功命令在代码、配置、依赖和环境未变化时不得因上下文压缩机械重复；最终固定门禁通过且无阻断失败后立即记录并停止。

### 15.2 批次验证边界

- Phase-08-01：Marshaller format/unit/vet/race，严格 Envelope/transformer，手动 offset、generation ownership/commit 确定性状态机，真实 Kafka+VictoriaMetrics 集成，Compose 渲染，脚本语法/自检，以及真实 Redis 到查询的最小纵向闭环。
- Phase-08-02：真实 rebalance、Kafka/VM/Marshaller 故障恢复，重复重放，日常生命周期、资源归属和直接门禁；未改变的 08-01 确定性结果可直接引用。
- Phase-08-03：最终构建的完整指标矩阵、代表性永久坏消息、跨组件故障恢复、内部访问负向、社交业务回归、资源清理、版本/分支治理和远程门禁。

### 15.3 阶段级封闭端到端矩阵

1. **真实成功链路**：改变隔离 Redis 状态并执行命令；真实 Envelope 经 Router/Kafka/Marshaller 写入，在 Envelope timestamp 窄窗口查询到 up、connection、request、CPU、memory 和 keyspace 指标。
2. **目标故障与恢复**：停止 Redis 后查询到同 target 的 `gopulse_redis_up=0`；恢复 Redis 后不重启链路即重新查询到 `up=1` 和完整指标。
3. **映射完整性**：metric name/value 保持，`mode`/`db` 标签正确转义并增加固定 source/target；无 message ID、plugin version、offset 等高基数标签。
4. **永久异常隔离**：Marshaller unit/fake-writer 层覆盖超限、坏 JSON、重复/未知字段、key/ID 不符及非法 schema/type/source/status/sample/label/value/time；真实 Kafka 链路只注入一个结构错误、一个 key/ID 错误和一个 payload 契约错误作为代表，均不写 VM、offset 被安全跳过且随后真实合法消息仍写入。
5. **VictoriaMetrics 故障恢复**：停止 VM 后当前合法 record 不提交且内存/日志有界；Backend 社交 API 可用。恢复原 VM 后不重启 Marshaller即写入并提交。
6. **Kafka 故障恢复**：停止 Kafka 后 Marshaller health 存活、ready 失败，业务不受阻；恢复同一 Topic/group 后继续消费，无手工 offset 修复。
7. **重复/commit 失败**：真实链路重放同一 Envelope；注入 Committer 的定向集成测试确定性模拟 HTTP 接受后 commit 失败，证明不推进后续 offset、重取后输出完全相同，查询窗口中同一时序/毫秒只有一个有效点；文档仍标记 at-least-once。
8. **进程恢复**：真实链路在 VictoriaMetrics 不可用且当前 record 明确未提交时终止 Marshaller，恢复存储并重启后从 committed offset 重取，不越过消息、不丢合法时序，并只产生允许的确定性重放；其他精确终止窗口由注入 ownership/Committer/Writer 的定向测试覆盖。
9. **访问边界**：无/错 Basic、普通/admin Cookie、无/错 Marshaller token 均不能获得内部 readiness 或 VM 数据；端口只绑定 loopback。
10. **职责隔离**：Monitor 无 Kafka/VM，Router 无 payload 清洗/存储，Marshaller 不采集 Exporter、不处理 RabbitMQ，VictoriaMetrics 不保存 Envelope JSON。
11. **社交业务隔离**：Kafka/Marshaller/VM 任一故障期间，代表性注册/登录/帖子/评论/点赞及 RabbitMQ 必要链路保持既有契约，Backend readiness 不新增 VM 检查。
12. **资源隔离**：正常、失败和中断清理不误杀日常进程、不删除非归属 container/network/volume/plugin root，不遗留随机端口、group fixture 或临时凭据文件。

以上是封闭矩阵。除非真实失败证明当前契约不足，不追加多 partition 压测、长时容量、通用 MetricsQL、安全代理、多租户、告警、聚合或日志/事件排列。

## 16. CI 与固定完成门槛

CI 增加独立 `Marshaller` job：Marshaller format/test/vet/race、注入 Committer/Writer/ownership lease 的确定性状态机测试，以及 `scripts/verify-marshaller.sh` 的适合 CI 的真实 Kafka/VictoriaMetrics 代表性验收；`Scripts and Compose` 加入 Marshaller 文件 LF、脚本语法/自检、VictoriaMetrics loopback、固定镜像、volume、Basic Auth、Topic/group 和端口检查。现有 Backend、Monitor、Router、Exporter、Frontend 和 Integration 职责不被替代。

阶段最终门槛至少包括：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
(cd backend && go test -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

未改变的 Phase-08-01/02 package 与组件故障结果可按实施记录引用，不在 Phase-08-03 因收口机械重跑；若 Consumer、Writer、Compose、恢复脚本、相关依赖或执行环境变化，只重跑受影响的检查。Phase-08-03 必须实际完成阶段主矩阵、业务/访问隔离、资源快照和治理门禁。

`verify-marshaller.sh` 是 Phase 8 Kafka 消费、转换、VictoriaMetrics 写入/查询、异常/重复及存储恢复的唯一主验收入口；`verify-router.sh` 保护原始 record 交接，`verify-business.sh` 证明可观测故障没有破坏身份、社交和 RabbitMQ 必要能力。不得用手工 curl 截图、VictoriaMetrics UI、直接 import 或源码阅读替代真实闭环。

完整验收只在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得用 mock Kafka、内存 writer、静态 Envelope 或直接 VictoriaMetrics import 代替阶段主证据。

## 17. 实施记录规则

每批在提交前创建：

```text
dev/logs/Phase-08/Phase-08-01-Marshaller与VictoriaMetrics指标闭环.md
dev/logs/Phase-08/Phase-08-02-可靠消费故障恢复与运维闭环.md
dev/logs/Phase-08/Phase-08-03-集成验收与里程碑收口.md
```

每份记录必须包含：

- 实际完成的行为和实际变更文件。
- 实际执行的验证命令、环境、结果和必要有限输出摘要。
- Kafka offset 范围、关联 message ID、VM 查询时间窗等不含敏感值的验收证据。
- 相对方案的偏差、实施中真实失败及其最小修复。
- 未完成限制、后续事项、Pull Request、远程检查和合入状态。

规划阶段不创建空日志，不得把计划命令写成已通过，不得在未推送、未观察远程 checks 或未合入时把批次/阶段标记为完成。

## 18. Phase 8 验收、完成与 Phase 9 交接

### 18.1 阶段验收标准

- Marshaller 是独立 Go module 和正式常驻 Consumer，不复用 Phase 7 验收身份，可有界启动、重连、处理和关闭。
- Kafka key/Envelope ID、schema/type/source/timestamp、payload、状态、family、kind、label、value 和样本集合均经过第二次严格校验。
- 合法 sample 确定性映射到固定 metric name、低基数 source/target labels、原 sample labels、有限 value 和 Envelope Unix 毫秒时间。
- 合法 record 只有在封闭转换完成、VictoriaMetrics HTTP 接受且当前 ownership lease 仍有效后提交；永久坏消息被安全跳过且不阻塞后续合法消息；VM 故障、commit 失败或 lost ownership 不提交并可自动恢复。
- 相同 record 重放产生相同输出，VictoriaMetrics 1ms 去重下查询无双点；系统仍明确为 at-least-once。
- 真实 Redis success、target unavailable 和恢复指标可通过受控查询看到，至少覆盖状态、连接、命令请求、CPU、内存和 keyspace，且验收窗口内 `vm_rows_invalid_total` 不增加。
- Kafka、Marshaller、VictoriaMetrics 均不向浏览器/普通用户开放；Phase 8 未新增 Backend 产品查询 API，社交身份和授权无变化。
- Kafka/Marshaller/VM 故障不会阻断既有社交业务、RabbitMQ 任务或放宽权限；Backend readiness 不依赖 VM。
- 日常与隔离生命周期不误杀、不误删、不泄密、不遗留资源；职责边界和 Phase 0～7 必要能力无回归。
- 三批实施记录真实完整，固定本地/远程门禁通过，根与 Frontend 版本均为 `1.5.3`。

### 18.2 完成与停止条件

只有第 18.1 节全部满足、Phase-08-03 已合入主远程 `main`、远程门禁成功且三份实施记录与真实提交一致，Phase 8 与 Milestone 2 才完成。任一真实上游输入、offset、永久异常继续、重复重放、VM 查询、故障恢复、业务隔离、内部身份、资源安全或远程状态证据缺失时，不得标记完成。

阶段验收通过后立即停止。Backend/Frontend Metrics Query 产品能力、Dashboard、告警、聚合、长期容量、cluster、多租户、读写分权、DLQ/重放、logs/events 均作为后续 Phase，不继续占用 Phase 8。

### 18.3 Phase 9 交接

- 可独立运行的 Marshaller 生命周期、内部 readiness、配置、日志、Bash 归属和 CI 模式。
- 正式 `gopulse-marshaller-metrics-v1` consumer group、手动 offset、永久无效/暂时失败/重放语义。
- metrics Envelope v1 的独立 decoder/validator 和基于 type 的显式处理分派边界；Phase 9 可以新增 logs handler，但不能放宽 metrics 契约。
- VictoriaMetrics writer 与 metrics transformer 独立于未来 Elasticsearch logs writer，存储失败互不混淆。
- Kafka、Marshaller、VictoriaMetrics 继续仅限内部网络；Phase 9 的日志查询若进入 Backend，必须继承数据库实时 admin `401/403` 边界。
- 真实 Metrics 全链路与故障恢复证据；Phase 9 添加 logs 时必须证明 metrics consumer/write/query 继续运行且不改写既有时序。
