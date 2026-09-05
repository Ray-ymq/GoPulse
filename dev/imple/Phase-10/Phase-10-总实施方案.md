# Phase 10：EventMonitor 与事件链路总实施方案

> 当前状态：待实施。本方案于 2026-09-05 以 `upstream/main` 提交 `da6c7d6` 与产品版本 `1.6.4` 为规划基线；Phase 10 使用 `1.7.x` 版本线，共分 3 个执行批次。

## 1. 实施目标

在 Phase 8 已交付 Metrics 链路、Phase 9 已交付 Logs 链路的基础上，补齐第三类可观测数据 Events，用真实的 Redis Exporter 插件生命周期、采集失败/恢复与异常退出事件形成端到端闭环：

```text
Plugin Manager / MetricsMonitor 真实状态转换
  → Monitor 进程内 EventMonitor 被动记录
  → 有界非阻塞队列与固定 events Envelope v1
  → Message Router → gopulse-observability-v1
  → Marshaller events 二次校验与确定性转换
  → Elasticsearch 独立事件索引
  → Backend 实时 admin 授权查询
```

阶段完成必须同时证明：事件由真实插件操作或采集状态变化产生，不是 fixture 或直接写 Elasticsearch；EventMonitor 不轮询插件状态，也不让可观测失败改变插件操作结果；Router/Kafka 接受后沿用正式 consumer group 的 at-least-once 与 ownership/offset 语义；相同 `message_id` 重放不形成重复事件文档；Metrics、Logs 和 Events 在同一 Topic/同一 Marshaller 中共存而不互相放宽契约或混写存储；未登录、普通用户和管理员分别获得 `401`、`403` 和受限查询结果。

只增加一个事件结构体、只在 stdout 打印“事件”、手工向 Kafka/Elasticsearch 写测试数据、绕过 Router 或 Marshaller，或将内部组件暴露给浏览器，均不构成 Phase 10 完成。

## 2. 当前真实基线与规划输入

本方案编写前已 fetch 主远程，当前 `upstream/main` 为 `da6c7d6`，根 `VERSION` 与 Frontend npm 元数据均为 `1.6.4`。该基线已包含 Phase-09-04 对日志 shipper 关闭线性化、退避 jitter、Elasticsearch template/mapping/alias 运行期重验证和日志查询词汇收敛的整改。Phase-10-01 开工前仍必须重新 fetch 并以最新 `main` 为准。

与 Phase 10 直接相关的已交付基线如下：

- `monitor` 是单一独立 Go 进程，内含 Plugin Manager、主动 Pull 的 MetricsMonitor、被动接收的 LogMonitor 和共享 Router HTTP Publisher；不存在 EventMonitor。
- Plugin Manager 已具备 install/start/stop/update、desired/observed state、安全错误、进程 ownership、异常退出 watch 与 Monitor 启动恢复；这些转换目前只更新内存状态和 API 响应，不产生 Events。
- MetricsMonitor 已将采集结果写回 Plugin Manager，区分成功、`target_unavailable` 和固定安全失败码；当前每次 scrape 可多次回调状态，Phase 10 不得直接据此产生失败/恢复震荡。
- Monitor 已使用 `MONITOR_ROUTER_TOKEN` 调用 Router `POST /internal/v1/messages`；LogMonitor 还使用独立 `LOG_MONITOR_INGEST_TOKEN`。Phase 10 事件源与 EventMonitor 同进程，不需要新的 HTTP 入口或第四个 token，但 EventMonitor 到 Router 仍只能使用既有服务身份。
- Router 只接受 `metrics/redis` 与 `logs/{backend|business-worker|search-indexer|search-reindex}` 显式组合，对公共 Envelope 执行严格校验，保持原始 bytes 写入固定单 Topic `gopulse-observability-v1`。
- Marshaller 使用正式 group `gopulse-marshaller-metrics-v1`、显式 typed target registry、禁用自动提交、generation ownership fencing、永久异常跳过和暂时存储错误顺序重试。Group 名是历史兼容标识，不代表只处理 metrics。
- Marshaller 已使用 VictoriaMetrics writer 和 Logs Elasticsearch writer；后者每次写前幂等确保 template，写后重验 strict mapping/read alias，只在外部合同成立后允许 offset 提交。
- Elasticsearch 已保存帖子搜索索引与 `gopulse-logs-v1-*` 日志索引；Backend 日志查询只读 `gopulse-logs-v1-read`，使用固定 query builder、PIT + `search_after`、HMAC 签名 cursor 和有界响应。
- Backend 已有认证中间件与数据库实时 `admin` 授权；日志和插件 API 已证明未登录 `401 authentication_required`、普通用户 `403 permission_denied`，且被拒绝请求不访问内部服务或 Elasticsearch。
- Bash 生命周期、验收脚本与 CI 已管理 Monitor、Router、Kafka、Marshaller、Elasticsearch、VictoriaMetrics、固定端口与强资源归属；Phase 10 必须扩展既有入口，不建立第二套弱清理流程。

若最新主远程改变上述 Plugin Manager 转换、MetricsMonitor 结果回调、Envelope/Topic/group、Elasticsearch 写后重验证、Backend admin 授权或生命周期契约，Phase-10-01 开工前必须先更新本总方案及所有尚未开始的拆分方案。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 9 最终版本 `1.6.4` 已位于最新主远程 `main`，相关远程门禁、实施记录和真实日志链路证据可核对。
- Metrics 与 Logs 的真实链路、混合消费、永久异常继续、Elasticsearch 故障恢复、同 ID 重放和 group ownership 证据保持有效。
- 实施、应用测试与集成验收必须在 Windows 宿主的 WSL2 Linux filesystem 中执行；Bash 是唯一维护的本地生命周期和验收入口。
- 每批开始前 fetch 主远程，从包含全部前置批次的最新 `main` 创建本方案分配的独立 `develop/x.x.x` 分支；不得在 `update`、Phase 9 分支或已完成分支上实施产品能力。
- 开始前保存 Git、日常 Compose project/container/network/volume、`.run` 进程、端口、插件根、Kafka group/offset 和 Elasticsearch 索引快照，不停止、删除、暂存或提交其他任务资源。
- 每批仅实现对应验收合同；无直接失败依据时，不开展通用事件总线设计、全仓 Review、覆盖率活动、依赖审计或 Elasticsearch 生产集群调优。

### 3.2 权威批次、版本与开发分支

Phase 10 使用 `1.7.x` 版本线，`1.7.0` 只作为阶段基线，不创建空批次。下表是本阶段执行顺序、目标版本与开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-10-01 | `1.7.1` | `develop/1.7.1` | 待实施 |
| Phase-10-02 | `1.7.2` | `develop/1.7.2` | 待实施 |
| Phase-10-03 | `1.7.3` | `develop/1.7.3` | 待实施 |

执行规则：

- 同一批次全部提交共享目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建与拆分方案同名的 `dev/logs/Phase-10/Phase-10-XX-*.md`，只记录实际改动、实际验证、偏差、失败和限制。
- Phase-10-01 以插件 install/start/stop/update 的真实成功转换交付 Events 端到端最小闭环，不将 EventMonitor、Router/Kafka、Marshaller/Elasticsearch 或 Backend `401/403/admin` 查询中任一环节推迟到后续批次。
- Phase-10-02 在已正确的纵向闭环上增加插件运行失败/异常退出、Metrics 采集失败/恢复与 target unavailable/recovered 状态转换，并完成事件去抖、源端有界性、故障恢复和 Metrics/Logs/Events 并存。
- Phase-10-03 只在已合入的最终实现上执行跨批次阶段矩阵、必要业务回归、文档、版本和远程状态收口；除真实复现的阻断问题外不增加产品功能。
- 已推送分支不得静默改名或重新编号。批次数量或顺序在实施前变化时，先更新本表并重新计算所有尚未创建的分支。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 在现有 Monitor 进程内增加被动 EventMonitor，通过进程内窄接口接收已完成的真实状态转换；不轮询 Plugin Manager，不创建重复守护进程。
- 真实成功事件覆盖 Redis Exporter 插件 install、start、stop 和 update 的最终状态转换；幂等 no-op 或在转换前被拒绝的请求不制造假事件。
- 运行故障事件覆盖有实际系统意义的插件终态操作失败、非预期进程退出、Metrics 采集失败/恢复与 Redis target unavailable/recovered。
- EventMonitor 在状态转换之后以非阻塞方式加入有界内存队列，生成可重试的 32 位小写十六进制 `message_id`，执行有界退避和有界关闭。
- 定义严格 Events payload v1，只允许固定 event name/source/severity/message 和按事件类型约束的 metadata；禁止任意扩展字段与原始异常。
- Router 增加 `events/monitor` 显式组合，保持原始 bytes、固定 Topic、Kafka key 和发送确认语义不变。
- Marshaller 增加独立 events validator/transformer/writer target，保持公共 Envelope、metrics validator、logs validator 和正式 offset/ownership 状态机不变。
- 建立 Events 独立 Elasticsearch template、按 UTC 日期物理索引、strict mapping 和固定 read alias，以 `message_id` 作为文档 `_id`。
- Backend 增加 `GET /api/v1/observability/events` 基础查询，复用现有 Cookie 认证与数据库实时 admin 授权，只接受固定时间范围、精确过滤和签名 cursor。
- 扩展 Bash 生命周期、独立 Events 验收、CI、README、版本与实施记录，并验证 Metrics/Logs/Events 三类消息在同一传输链路中共存。

### 4.2 明确不做

- 不实现 Phase 11 可观测页面、Dashboard、图表、插件管理界面或浏览器直连 Monitor/Elasticsearch。
- 不实现指标告警、事件通知、事件订阅、复杂事件关联、根因分析、聚合、全文搜索或任意 Elasticsearch DSL。
- 不全面采集 Kubernetes/Pod 事件、宿主机事件、容器事件、Kafka/Elasticsearch 内部事件或业务用户行为事件。
- 不把 Phase 2 的业务 Outbox event 当作可观测 Events，不改变 RabbitMQ 业务契约、通知状态机或搜索索引流程。
- 不为同进程事件源新增 HTTP ingest API、浏览器 Origin、Cookie/JWT 身份或通用第三方事件接入协议。
- 不引入持久化 spool、本地事件数据库、无限队列、零丢失或端到端 exactly-once 声明。
- 不拆分 Kafka Topic，不迁移正式 consumer group，不引入 Schema Registry、DLQ、重放 API、Kafka transaction 或跨类型优先级调度。
- 不实现 Elasticsearch ILM、自动删除、rollover、冷热分层、容量规划、多节点高可用、TLS 或公网入口。
- 不把 EventMonitor、Router、Kafka 或 Marshaller 加入 Backend readiness；Elasticsearch 继续保持 Phase 3 既有 readiness/search 语义。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收，不创建应用容器镜像。

## 5. 组件职责与运行架构

### 5.1 进程与模块边界

- Plugin Manager 和 MetricsMonitor 是真实事件源，只在已确定的终态转换点调用窄 `EventRecorder` 接口；它们不构造 Envelope、不调用 Kafka、不知道 Elasticsearch。
- EventMonitor 属于现有 `monitor` module，负责事件词汇校验、事件时间与消息 ID、失败剧集去抖、有界队列、Envelope 封装、Router 发布与关闭 drain。
- EventMonitor 不对外注册 HTTP 采集路由。其“被动”语义是消费同进程业务组件主动报告的已发生转换，而不是周期扫描状态。
- EventMonitor 的运输状态日志必须使用 stdout-only logger，不得反向生成 Events，否则 Router/Kafka 故障会形成递归放大。
- Router 只校验 Envelope 顶层与 `events/monitor` 组合，选择固定 Topic 并等待 Kafka delivery；不解析 metadata、不改写事件。
- Marshaller 负责公共 Envelope 二次校验和按类型分派；events handler 独立完成 payload 二次校验、Document 生成和 Events Store 调用。
- Elasticsearch 仅保存白名单 Events 文档，不保存原始异常、进程信息、HTTP header、完整 Envelope、Kafka metadata 或未知 metadata。
- Backend `eventquery` 只读固定 Events alias，经现有认证与实时 admin 授权后返回安全 DTO；Frontend 不获得内部 URL、token、索引或 PIT 细节。

### 5.2 启动与关闭顺序

日常启动顺序保持：

```text
Compose 基础设施
→ Message Router
→ Marshaller（Metrics + Logs + Events targets）
→ Monitor（EventMonitor → Plugin Manager/MetricsMonitor/LogMonitor）
→ Backend / Business Worker / Search Indexer / Frontend
```

- Monitor 内部先创建 EventMonitor 和共享 Router Publisher，再将 recorder 注入 Plugin Manager 与 MetricsMonitor；启动失败时不得留下 worker、ticker 或插件进程。
- Monitor 关闭时先停止 HTTP 新请求和 MetricsMonitor 新采集，再终止插件并在固定时限内 drain EventMonitor；最后停止后台 event worker。
- 因 Monitor 整体关闭而终止插件不等于管理员 stop 操作，不产生 `exporter_plugin_stopped`，避免在进程退出边界制造无法稳定投递的假业务事件。
- 依赖暂不可用不得阻止插件管理、MetricsMonitor 或社交业务启动；非法配置可安全启动失败，但不打印配置值。

## 6. 事件源、触发时点与去抖

### 6.1 插件生命周期成功事件

| `event_name` | 固定 `message` | 触发点 | 严重程度 | 规则 |
| --- | --- | --- | --- | --- |
| `exporter_plugin_installed` | `exporter plugin installed` | 安装、持久化和首次启动均成功，状态已是 `running` | `info` | 安装内部启动不再另发 `started` |
| `exporter_plugin_started` | `exporter plugin started` | 从非 running 终态真实转为 `running` | `info` | 已 running 的幂等 no-op 不发事件 |
| `exporter_plugin_stopped` | `exporter plugin stopped` | 管理员 stop 完成且最终状态为 `stopped` | `info` | Monitor shutdown 内部终止不发事件 |
| `exporter_plugin_updated` | `exporter plugin updated` | 新版本持久化成功，并已达到操作前 desired state | `info` | 一次 update 只发一个终态事件，内部 stop/start 不重复发送 |

- 记录点必须在主状态、registry 和 runtime 已成功更新之后；EventMonitor enqueue 成功或失败都不反向修改操作响应。
- 无效 package、不存在的 plugin ID、conflict、operation in progress、未授权和请求格式错误未进入系统转换，不作为可观测 Events 保存。

### 6.2 插件与采集故障事件

| `event_name` | 固定 `message` | 触发点 | 严重程度 | 关键 metadata |
| --- | --- | --- | --- | --- |
| `exporter_plugin_failed` | `exporter plugin operation failed` | start/stop/update/recover 进入系统转换后以固定安全码终止 | `error` | `operation`, `error_code`, `to_state` |
| `exporter_plugin_exited` | `exporter plugin exited unexpectedly` | ownership 匹配的运行插件非预期退出并已转为 `failed` | `error` | `error_code=process_exited`, `to_state=failed` |
| `metrics_collection_failed` | `metrics collection failed` | 从无活动失败转入第一个 scrape/parse/publish 失败 episode | `warn` | `operation`, `error_code` |
| `metrics_collection_recovered` | `metrics collection recovered` | 失败 episode 后首个完整采集且 metrics 发布成功 | `info` | `operation=scrape` |
| `metrics_target_unavailable` | `metrics target unavailable` | 从非 unavailable 转为合法 `scrape_status=target_unavailable` 且状态 metrics 已发布 | `warn` | `scrape_status=target_unavailable` |
| `metrics_target_recovered` | `metrics target recovered` | target unavailable episode 后首个合法 `scrape_status=success` 且 metrics 已发布 | `info` | `scrape_status=success` |

- 采集事件以“episode”去抖：持续同类失败只发一个 failed/unavailable，真实成功后发一个 recovered 并重置；不按 scrape 周期产生事件。
- MetricsMonitor 需对每次 scrape 向事件状态机报告一个最终结果。“采集成功、发布失败”是 failed，不得先发 recovered 再紧接 failed。
- `target_unavailable` 是符合 Metrics 契约且已成功传输的目标状态，与 scrape/parse/publish 失败 episode 分开记录。
- EventMonitor 自身的 queue full、Router 发布失败或 drain 超时只写受节流的 stdout 状态，不生成递归 Events。

## 7. Events payload v1 与 Envelope 契约

### 7.1 公共 payload

每个事件 payload 必须是唯一 JSON object，且只包含：

```json
{
  "event_schema_version": 1,
  "event_name": "exporter_plugin_started",
  "source": "monitor",
  "severity": "info",
  "timestamp": "2026-09-05T08:00:00.000000000Z",
  "message": "exporter plugin started",
  "metadata": {
    "plugin_id": "redis-exporter",
    "plugin_version": "1.0.0",
    "operation": "start",
    "from_state": "stopped",
    "to_state": "running"
  }
}
```

公共规则：

- `event_schema_version` 固定为整数 `1`；`source` 固定为 `monitor`；`timestamp` 必须为 UTC RFC3339Nano，最多超前校验时间 `5m`。
- `event_name`、`severity` 和 `message` 必须命中第 6 节定义的固定一对一词汇；不接受自由文本描述或自由 severity。
- `metadata` 必须是 object，不允许 `null`、array、嵌套 object 或未知字段。可用 key 固定为 `plugin_id`、`plugin_version`、`previous_plugin_version`、`operation`、`from_state`、`to_state`、`error_code` 和 `scrape_status`。
- `plugin_id` 固定为 `redis-exporter`；版本必须为三段 SemVer。`operation` 只允许 `install|start|stop|update|recover|scrape|publish`，终态 `from_state/to_state` 只允许 `not_installed|stopped|running|failed`，`scrape_status` 只允许 `success|target_unavailable`。
- `error_code` 只允许受版本控制的安全码：Plugin Manager 初始集合为 `start_failed|stop_failed|update_failed|rollback_failed|recovery_failed|recovery_invalid|process_exited`，MetricsMonitor 初始集合为 `scrape_timeout|network_failed|response_too_large|parse_failed|contract_invalid|content_invalid|http_invalid|scrape_failed|message_id_failed|publish_failed`。需要新码时必须同步修订三端 validator、查询词汇、测试与文档。
- 每个 event name 的 metadata 必填/可选/禁止组合由单一受版本控制的 contract table 维护；不以“所有白名单字段可任意组合”代替事件语义。
- 递归拒绝重复 key、未知 key、尾随 token、错误 JSON 类型、非 UTF-8、控制字符、超限字符串和敏感哨兵。
- 禁止 PID、内部地址/URL、token、Authorization/Cookie/JWT、服务器路径、命令行、底层错误、堆栈、响应 body、用户名、帖子/评论内容或任意用户可控文本进入 payload。

### 7.2 events Envelope v1

EventMonitor 校验并 canonicalize payload 后构造：

```json
{
  "schema_version": 1,
  "message_id": "0123456789abcdef0123456789abcdef",
  "type": "events",
  "source": "monitor",
  "timestamp": "2026-09-05T08:00:00.000000000Z",
  "payload": { "event_schema_version": 1 }
}
```

- `message_id` 使用 `crypto/rand` 生成 16 bytes 并编码为 32 位小写十六进制；同一队列项的全部 HTTP 重试复用同一 ID。
- Envelope `source` 必须与 payload `source` 完全相同，Envelope `timestamp` 必须与 payload 事件时间表达同一时刻。
- `payload` 不带 Monitor 接收时间、Router URL/token、队列状态、HTTP header、操作请求或用户身份。
- 同一事件对象与 `message_id` 必须生成确定性等价 JSON；Marshaller 重新校验，不信任 Monitor 已校验的 payload。

## 8. EventMonitor 有界性、传输与交付边界

- 默认队列容量 `256`，允许 `1..4096`；单条 canonical Events Envelope 最大 `16 KiB`，实际固定词汇事件应远小于此上限。
- `Record` 只做本地校验、message ID 生成和非阻塞 enqueue；不得在插件 install/start/stop/update、进程 watcher 或 metrics scrape 控制流中等待网络。
- 只有一个发送 worker 和一个队首在途请求；不为每个事件创建 goroutine，不无界并发重试。
- 只有 Router `202 Accepted` 才移除队首。网络、timeout、`429` 或 `5xx` 使用含可测 jitter 的有界指数退避；确定性 `4xx` 作为当前记录永久拒绝，不阻塞后继事件。
- 禁止 redirect；响应最多读取 `4 KiB` 后丢弃；状态日志不回显 URL、token、Envelope、metadata、响应 body 或底层错误。
- queue full 只丢弃远程事件副本，以“进入 full”和“恢复 available”状态转换节流 stdout；插件与采集结果不变。
- shutdown 先与 `Record` 建立可测的线性化边界，再在默认 `5s`、允许 `0..30s` 内 drain；超时后安全停止并记录未发送数，不修改 Monitor 原始退出结果。
- 队列仅在内存中。在 Router `202` 前，Monitor 崩溃、message ID 熵失败、队列溢出或 drain 超时允许丢失远程副本；Router/Kafka 接受后才进入既有 at-least-once 边界。

## 9. Router、Kafka 与 Marshaller 契约

### 9.1 Router 与 Kafka

- Router 支持组合扩展为 `metrics/redis`、四个已知 logs source 和 `events/monitor`；其他 schema/type/source 组合继续返回 `422 message_type_unsupported`。
- Router 不解析 Events payload，不添加 ingest 时间，不重写 severity/message/metadata，不从 source、query、header 或 payload 接受 Topic。
- `events` 继续路由到 `gopulse-observability-v1`，Kafka record key 等于 `message_id`，value 是 Router 收到的原始 Envelope JSON bytes。
- 保留 1 partition / replication factor 1、禁止自动创建和既有 retention；本阶段接受一类存储暂时失败会按顺序延迟后续类型的明确限制。

### 9.2 Marshaller 二次校验与分派

- 公共 Envelope decoder 增加 `events/monitor`，保留 key/ID、大小、UTF-8、唯一 JSON、顶层字段、schema、UTC timestamp 和 future skew 检查。
- `events/monitor` target 使用独立 events transformer 和 Events Store；不向 metrics/logs validator 增加 events 字段，不让未知类型落入通用 map 后直接写存储。
- events transformer 对 payload 执行第 7 节全部二次校验，并额外确认 payload/envelope 的 source 与 timestamp 一致；不一致是永久无效 record。
- 合法 record 只有 Events Store 确认文档写入、目标索引 strict mapping/read alias 成立且当前 lease 仍有效后才提交 offset。
- 永久无效 record 记录固定 reason code，不调用任何 writer，在 ownership 有效时安全提交并继续后续；网络、timeout、认证、非成功响应、template/index 合同错误和结果不确定均不提交。
- 保留 group `gopulse-marshaller-metrics-v1` 和现有单 partition 有序 backpressure；不以名称不准确为由迁移 offset。

## 10. Elasticsearch Events 存储契约

### 10.1 固定名称与隔离

| 用途 | 固定名称 |
| --- | --- |
| Index template | `gopulse-events-v1-template` |
| 物理索引 | `gopulse-events-v1-YYYY.MM.DD`（按 Envelope UTC 日期） |
| Backend 读 alias | `gopulse-events-v1-read` |
| Logs 读 alias | 保持 `gopulse-logs-v1-read` |
| 帖子搜索 alias | 保持 `gopulse-post-search-v1` |

- template 只匹配 `gopulse-events-v1-*`，自动附加固定 read alias；Marshaller 只能创建/写入当日精确 Events 索引，Backend 只读固定 alias。
- Events 代码禁止使用 `gopulse-*`、`*events*` 或客户端提供的 index；logs 与帖子 repository 不得访问 Events prefix。
- writer 以 `message_id` 作为 `_id`，相同 Kafka key/value 重放只能产生 `created|updated|noop` 受限结果，文档数不增加。
- 每次写前幂等确保 Events template，每次成功文档响应后验证实际目标索引的 strict mapping 与 read alias；不使用跨 Elasticsearch 集群生命周期的永久内存 ready 布尔值。

### 10.2 文档 mapping

Marshaller 只将 payload 映射为以下字段：

| 字段 | Elasticsearch 类型 | 说明 |
| --- | --- | --- |
| `@timestamp` | `date_nanos` | 由 payload `timestamp` 确定性改名 |
| `event_schema_version` | `integer` | 固定 `1` |
| `event_name` | `keyword` | 固定事件词汇 |
| `source` | `keyword` | 当前固定 `monitor` |
| `severity` | `keyword` | `info|warn|error` |
| `message` | `keyword` | 与 event name 绑定的固定文本 |
| `metadata` | strict object | 仅包含第 7.1 节白名单 keyword 字段 |

- 根 mapping 与 `metadata` 对象都必须 `dynamic: strict`；不保存 Envelope `message_id`、Kafka offset、原始 JSON 或未知字段。
- `message` 不用于全文搜索，metadata 不作 flattened 或任意 map，避免映射膨胀和敏感字段渗入。

## 11. Backend 事件查询契约

### 11.1 HTTP API 与授权

| Method | Path | 授权 | 成功响应 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/observability/events` | Authentication → 数据库实时 `RequireAdmin` | `200` 分页 Events DTO |

- 未登录返回 `401 authentication_required`，已登录普通用户返回 `403 permission_denied`；两者都必须在创建 PIT 或调用 Elasticsearch 之前终止。
- admin 继续使用现有注册、登录和 Cookie，不建立独立管理会话、JWT 或前端直连凭据。
- Elasticsearch 不可用、超限响应或不可信响应统一返回 `503 events_unavailable`；不回显 URL、alias/index、PIT、DSL、响应 body 或底层错误。
- alias 尚不存在表示尚无 Events，返回 `200` 空页，不把“无数据”误报为存储故障。

### 11.2 首页、过滤与 cursor

- 首页默认最近 `15m`，最大时间范围 `24h`，`limit` 默认 `50`、允许 `1..100`；`to` 不允许超前服务端时钟 `5m`。
- 可用 exact filters 仅为 `source`、`event_name`、`severity`、`plugin_id`、`operation` 和 `error_code`；每个值必须命中 Events v1 已知词汇，不接受模糊、通配、正则、任意 metadata key 或原始 DSL。
- 同一 query key 重复、未知/空参数、非 UTF-8、过长值、不可能的 event/metadata 组合或超出时间范围统一返回 `400 validation_failed`。
- 排序固定为 `@timestamp desc, _shard_doc desc`，通过 PIT + `search_after` 分页；`_shard_doc`、PIT 与索引信息不返回 DTO。
- cursor 使用与 Logs 不同的 `gopulse/backend/event-query-cursor/v1` HMAC domain key，固化实际 from/to、filters、limit、PIT、last sort 和最长 `2m` 到期时间。
- 续页请求只能携带单一 `cursor`；篡改、过期、超限、与其他参数混用或 PIT 失效都安全失败，不退回非 PIT 查询。

### 11.3 响应 DTO

每条记录只返回：

```text
timestamp, event_name, source, severity, message, metadata
```

- `metadata` 只返回该 event name 允许的已知字段，空字段保持省略；Backend 解析 Elasticsearch `_source` 时再次校验索引前缀、字段类型和事件契约。
- 不返回 Elasticsearch `_index/_id/_score`、Envelope message ID、Kafka metadata、原始 JSON、进程信息、用户身份或内部错误。
- 响应是可观测记录而非业务事实源；因源端 best-effort 边界，查询不到事件不能被 Phase 11 解释为操作绝对未发生。

## 12. 配置、生命周期与运维入口

新增或冻结的配置边界：

| 配置 | 默认/固定值 | 校验 |
| --- | --- | --- |
| `MONITOR_EVENT_QUEUE_CAPACITY` | `256` | `1..4096` |
| `MONITOR_EVENT_RETRY_MIN` | `250ms` | `100ms..5s` |
| `MONITOR_EVENT_RETRY_MAX` | `5s` | `retry_min..30s` |
| `MONITOR_EVENT_SHUTDOWN_TIMEOUT` | `5s` | `0..30s` |
| `MONITOR_EVENT_MAX_BYTES` | `16384` | 固定 `16384` |
| `MARSHALLER_EVENT_TEMPLATE` | `gopulse-events-v1-template` | 固定等值 |
| `MARSHALLER_EVENT_INDEX_PREFIX` | `gopulse-events-v1-` | 固定等值 |
| `BACKEND_EVENT_READ_ALIAS` | `gopulse-events-v1-read` | 固定等值 |
| `BACKEND_EVENT_QUERY_DEFAULT_RANGE` | `15m` | 固定等值 |
| `BACKEND_EVENT_QUERY_MAX_RANGE` | `24h` | 固定等值 |

- 配置经 `.env.example`、Monitor/Marshaller/Backend config、`scripts/dev.sh` 和验收脚本对齐；不修改冻结 `scripts/*.ps1`。
- Events 不需要新的服务 token；Monitor 到 Router 继续使用 `MONITOR_ROUTER_TOKEN`，Backend 到 Elasticsearch 继续使用既有受控内部连接。任何事件字段都不得承载 token。
- `scripts/dev.sh` 启动现有进程并配置 Events 能力；`scripts/verify.sh` 仅读检查 event template/alias/API 路由和进程健康，不制造事件、不写 Kafka/ES、不提交 offset。
- 新增 `scripts/verify-events.sh`，支持 `--self-test` 与真实隔离验收；使用随机 Compose project、loopback 端口、凭据、plugin root、Kafka group 观测窗口和可强归属临时文件。
- 脚本正常、失败、signal 和中断路径都只清理本批强归属资源；日常 Elasticsearch/Kafka/VM volume 必须保留。

## 13. 故障语义、安全与兼容性

- EventMonitor 失败是可观测降级：不改变插件 API HTTP 状态、registry/desired/observed state、Metrics 采集结果、Monitor health/ready 或社交业务。
- `Record` 返回失败仅用于受节流的 stdout 可观测；不向 admin 操作返回“插件成功但事件失败”的混合结果。
- 相同真实状态转换只记录一次；同 ID HTTP/Kafka 重试允许重复传输，由 Elasticsearch `_id` 幂等收敛，不宣称跨崩溃精确一次。
- 单 Topic 上 Events Store 故障可按顺序阻塞后续 Metrics/Logs；Marshaller 不得越过未写成功事件或错提 offset。独立 Topic/group 是需要真实负载证据的后续架构项。
- Events 索引与查询不改变帖子搜索和 Logs 合同；对共享 Elasticsearch transport/readiness 的修改必须有直接回归证明旧 alias、mapping、PIT/cursor 和空索引语义不变。
- EventMonitor、Router、Marshaller 和 Elasticsearch 端口仅 loopback/受控网络；普通用户、admin Cookie、JWT 与 Monitor 管理 token 都不是 Router 服务身份。
- 从生成点、Monitor stdout、Kafka/ES、Backend 响应到验收制品全链扫描敏感哨兵；即使查询者是 admin，也不允许返回内部字段或原始异常。

## 14. 测试与验收策略

### 14.1 最低有效测试层

- EventMonitor 单元测试覆盖固定事件契约、message ID 重试不变、非阻塞 queue full、退避/jitter、永久拒绝继续、Record/Close 线性化和 drain 超时；不穷举等价 JSON 排列。
- Plugin Manager 每个变更转换使用一个代表性成功和必要失败/no-op 测试，确认只在最终状态记录事件且 recorder 失败不改变主结果。
- MetricsMonitor 用状态机测试覆盖持续失败去抖、发布失败不先恢复、成功恢复、target unavailable/recovered 和 shutdown 不产生假恢复。
- Router 测试只新增代表性 events 成功、unsupported 组合失败和原始 bytes 保留；不复制全部 metrics/logs 契约测试。
- Marshaller 测试覆盖 Events payload 成功/失败代表、source/timestamp 不一致、strict document、同 ID 重放、永久无效跳过、Events Store 暂时失败不提交和 ownership loss。
- Backend 测试覆盖参数词汇、PIT/cursor、空 alias、存储 `503`、响应 DTO、`401/403/admin` 与拒绝时 repository 零调用。

### 14.2 真实端到端证据

- 在全新随机 Elasticsearch/Kafka 资源中通过 Backend admin API 触发真实插件 install/start/stop/update，再通过 Backend Events API 查询，禁止直接写 ES 充当源证据。
- 以真实 Redis 中断/恢复与受控 Exporter 异常退出产生 target/collection/plugin 事件，证明持续故障不按 scrape 洪泛、恢复只发一次。
- 在同一有限 Kafka offset 窗口交替产生真实 Metrics、Logs 和 Events，验证 VM、Logs ES alias 和 Events ES alias 分别出现预期数据且不互写。
- 代表性故障窗口覆盖 EventMonitor 队列/Router/Kafka 暂时不可用、Events Store 不可用与恢复、Marshaller 重启/ownership，不机械穷举每个组件与每个事件的笛卡尔积。
- 在可观测故障窗口执行最小必要社交业务回归，确认新增 Events 失败不改变注册/登录、帖子、评论/点赞、通知、搜索的直接受影响事实。

## 15. 批次拆分与交付关系

### 15.1 Phase-10-01：插件生命周期事件端到端查询闭环

目标版本/branch：`1.7.1` / `develop/1.7.1`。

以 install/start/stop/update 成功转换为第一条真实纵向切片，一次性交付 EventMonitor 有界记录、Events payload/Envelope、Router/Kafka、Marshaller/Events Store、Elasticsearch 独立索引和 Backend admin 查询。本批保证架构从第一天就是可运行闭环，不交付只能存储无法查询的半成品。

### 15.2 Phase-10-02：采集故障事件与可靠性闭环

目标版本/branch：`1.7.2` / `develop/1.7.2`。

接入插件运行失败/异常退出、Metrics 采集失败/恢复和 target unavailable/recovered，实现 episode 去抖和最终采集结果语义；完成 queue full/短时传输故障、永久无效、Events Store 故障恢复、同 ID 重放、Metrics/Logs/Events 混合消费和日常生命周期。

### 15.3 Phase-10-03：集成验收与阶段收口

目标版本/branch：`1.7.3` / `develop/1.7.3`。

在前两批已合入的最终构建上执行封闭阶段矩阵，完成权限、脱敏、索引隔离、去抖、重放、故障恢复、三类数据并存、业务隔离、资源安全、固定门禁、实施记录和远程合入状态收口。本批不是第三个功能批。

## 16. 预计变更边界

```text
monitor/internal/events/**
monitor/internal/plugin/**
monitor/internal/metrics/collector/**
monitor/internal/config/**
monitor/cmd/monitor/**
monitor/README.md
router/internal/envelope/**
router/internal/routing/**
router/README.md
marshaller/internal/envelope/**
marshaller/internal/events/**
marshaller/internal/elasticsearch/**
marshaller/internal/config/**
marshaller/internal/httpserver/**
marshaller/cmd/marshaller/**
marshaller/README.md
backend/internal/eventquery/**
backend/internal/apperror/**
backend/internal/config/**
backend/internal/http/**
backend/cmd/server/**
README.md
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-events.sh
scripts/verify-monitor.sh（仅直接回归/契约扩展）
scripts/verify-router.sh（仅直接回归/契约扩展）
scripts/verify-marshaller.sh（仅直接回归/契约扩展）
scripts/verify-logs.sh（仅三类并存或阻断回归）
scripts/ci/**
.github/workflows/quality-gates.yml
dev/logs/Phase-10/**
dev/imple/Phase-10/Phase-10-总实施方案.md（只更新真实状态/偏差）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不是要求制造无意义修改。若实施发现必须越过该边界修改业务数据模型、RabbitMQ 契约、Frontend 产品页面、Topic/group 或公共认证体系，先停止并更新方案，不在当前批次隐式扩张。

## 17. 固定完成门禁

每批的精确命令以对应拆分方案为准。阶段收口在最终 diff 上至少包括：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-events.sh --self-test
scripts/verify-events.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

- 真实 Events 验收只能在 WSL2 Linux filesystem、真实 Kafka/Elasticsearch/VictoriaMetrics 和随机强归属资源中标记通过。macOS 可执行规划、静态检查和不需要应用验收的文档工作，不代替 WSL2 完成证据。
- mock、直接 Event Envelope、手工 Kafka produce 或直接 ES index 只用于最低层错误和故障注入，不代替真实插件/采集主链路。
- 一个检查成功后，只有相关代码、配置、依赖或环境发生可影响结果的变化才重跑；不因上下文压缩或收口需要而机械重复成功矩阵。
- 本地、push、Pull Request、远程 checks 和 merge 是不同状态；未亲自观察的远程结果不得写为已完成。

## 18. Phase 级验收、完成与交接

### 18.1 Phase 级验收标准

- 真实 Redis Exporter 安装、启动、停止或更新操作至少覆盖一个成功转换，能经 EventMonitor → Router → Kafka → Marshaller → Events ES alias 由 Backend admin API 查询。
- 真实采集失败与恢复、Redis target unavailable/recovered 和插件非预期退出能产生正确 event name、source、severity、timestamp、message 和有限 metadata。
- 持续故障按 episode 去抖，恢复事件只在真实成功后发生；幂等 no-op、被拒绝请求和 Monitor shutdown 不制造假生命周期事件。
- EventMonitor 是被动、非阻塞、有界的；queue/transport 失败不改变插件或采集主结果，短时故障容量内可恢复，长故障的 best-effort 限制被准确记录。
- Router 保持原始 bytes 与单 Topic；Marshaller 只在 Events 写入和索引合同成立后提交，永久坏事件不写存储且不阻塞后继，同 ID 重放只有一个文档。
- `gopulse-events-v1-*`/read alias/strict mapping 与 `gopulse-logs-v1-*`、帖子搜索索引完全隔离；空集群替换或 Events Store 故障恢复后不提前提交 offset。
- 未登录 `401`、普通用户 `403`、admin 受限查询成功；拒绝请求不访问 ES，时间范围、已知过滤、PIT/cursor、空结果和 `503` 符合契约。
- Metrics、Logs、Events 在同 Topic/正式 group/Marshaller 中交替运行，分别写入 VM、Logs ES 和 Events ES，既有 metrics/logs validator、mapping、alias、查询和恢复语义不回归。
- 事件链路故障不会为插件管理或社交业务新增 readiness 失败；最小必要业务回归、内部身份隔离、loopback 端口和敏感哨兵扫描通过。
- `dev.sh → verify.sh → down.sh`、Events 独立验收、失败/中断清理和 verify 只读性通过，不误杀/误删或遗留日常与其他任务资源。
- 三份实施记录真实完整，固定本地/远程门禁通过，根与 Frontend 版本均为 `1.7.3`。

### 18.2 完成与停止条件

只有第 18.1 节全部满足、Phase-10-03 Pull Request 已合入主远程 `main`、远程固定门禁成功，且三份 Phase 10 实施记录与真实提交一致，Phase 10 才完成。任一真实事件源、完整传输、offset/ownership、索引隔离、去抖、重放、admin 授权、三类数据并存、业务/敏感/资源安全证据缺失时，不得标记完成。

达到条件后立即停止。Frontend Events 页、告警、聚合、复杂关联、Kubernetes 事件、ILM、持久化 spool、Topic 拆分、容量和生产安全加固记录为后续，不继续占用 Phase 10。独立实现 Review 只在用户明确请求时执行，不作为默认阶段门禁。

### 18.3 Phase 11 交接

向 Phase 11 交付：

- `GET /api/v1/observability/events` 的实时 admin 授权、已知过滤、PIT 签名 cursor、安全 DTO 和错误契约。
- 固定 Events read alias、strict mapping 与日志/帖子索引隔离；Frontend 只能经 Backend 读取，不得直连 Monitor、Router、Kafka、Marshaller 或 Elasticsearch。
- event name/source/severity/message/metadata 的固定 v1 词汇和状态转换语义，UI 不得将 metadata 当作任意 JSON 渲染。
- Router `202` 前有界 best-effort、Kafka 接受后 at-least-once、同 ID ES 幂等和失败 episode 去抖的准确产品语义；UI 不得将缺失 Events 解释为系统行为绝对未发生。

Phase 10 完成后，Metrics、Logs、Events 三类后端数据链路齐备，但 Milestone 3 仍未完成；只有 Phase 11 统一管理员前端完成并通过验收，才能宣称“完整可观测 MVP”完成。
