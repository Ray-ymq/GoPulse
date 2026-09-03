# Phase 4：业务日志基础总实施方案

## 1. 实施目标

在已经可独立运行的 GoPulse 业务系统上统一应用日志，使一次真实 HTTP 请求能够通过服务端生成的 `request_id` 关联访问结果与关键业务动作，同时让 Backend 内部后台任务、Business Worker、Search Indexer 和 search-reindex 使用同一份结构化日志契约。

本阶段按“先完成 HTTP 请求闭环，再统一后台进程并完成阶段收口”的顺序执行：

```text
HTTP 请求：
Client → Backend request-id middleware
       → Authentication / Handler / Service
       → 业务成功或安全错误日志
       → HTTP 完成日志 + X-Request-ID

异步处理：
Backend Outbox → RabbitMQ → Business Worker / Search Indexer
              event_id → 结构化处理、重试与 dead-letter 日志

维护命令：
search-reindex → 结构化启动、完成或失败日志
```

阶段完成必须用真实主要 API 证明统一 JSON 格式、服务端请求标识、正常与错误路径、业务动作日志和异步进程日志可以稳定读取。只增加日志库封装、只输出一条启动日志，或只编写字段说明均不构成完成。

## 2. 当前真实基线

Phase 4 规划以 `update` 分支提交 `7566c65` 及其后继规划提交为文档基线；执行必须以届时已经发布 `1.0.0` 的最新主远程 `main` 为代码基线：

- Phase 3 搜索、异步索引和 Review 整改已经合入主远程，当前根 `VERSION` 与 Frontend npm 元数据为 `0.4.4`。
- 自动 Pull Request 编排修复已经由 PR #52 合入 `main`，但独立的 Milestone 1 release-only 动作尚未把主远程产品版本更新为 `1.0.0`。
- Gin Backend 通过 `gin.New()` 和默认 `gin.Recovery()` 提供 health、readiness 与 15 条 `/api/v1` 路由；当前没有访问日志、`request_id` 或结构化 Recovery。
- Handler 已把 `context.Context` 传入业务 Service；认证中间件把 `user_id` 放入请求 context，具备增加请求级日志上下文的基础。
- Backend 生命周期、Outbox Dispatcher、缓存降级、Business Worker、Search Indexer 和 search-reindex 仍混用 `log.Printf`、`log.Fatal` 或 `fmt.Println` 文本输出。
- Worker 日志回调是 `func(string, ...any)`，retry/dead 日志把动态字段拼接进文本；现有验收脚本通过固定文本 grep 等待 retry 标记。
- `scripts/verify-business.sh` 已在独立临时目录捕获 Backend、Business Worker、Search Indexer 和 Frontend 输出，并具备真实 MySQL、Redis、RabbitMQ、Elasticsearch 与浏览器业务矩阵，可作为结构化日志验收入口。
- 现有安全边界禁止日志输出密码、连接凭据、JWT、Cookie、帖子标题/正文、评论内容、事件 Payload、查询 DSL、PIT ID 和原始 Elasticsearch 响应；Phase 4 必须保持并统一这些约束。

本阶段不重新设计业务模型、消息 Envelope、搜索事实来源或通知投递语义。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- `develop/1.0.0` release-only 分支已经完成、合入配置的主远程 `main`，远程必要门禁成功，且根 `VERSION` 与 Frontend npm 元数据均为 `1.0.0`。
- 每批开始前 fetch 主远程，确认前置批次已合入最新 `main`，再从该提交创建本方案分配的独立开发分支。
- 实施、应用测试和集成验收在 Windows 宿主机的 WSL2 Linux filesystem 中执行；Bash 是唯一维护的生命周期与验收入口。
- 开始前记录 Git 状态和日常运行资源状态，不覆盖、暂存或提交用户及其他任务的改动。
- 每批只实现对应验收合同；日志改造不得顺带调整业务错误、消息重试、搜索索引、数据库 Schema 或生命周期所有权语义。

Phase 4 方案可以在 `update` 上提前完成，但上述发布前置条件满足前不得创建或实施 `develop/1.1.1`。

### 3.2 权威批次、版本与开发分支

Phase 4 使用 `1.1.x` 版本线，`1.1.0` 只作为阶段基线，不创建空批次。下表是本阶段批次、顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-04-01 | `1.1.1` | `develop/1.1.1` | 已完成；PR #55 于 2026-09-03 合入 `main`（`fa7cdab`） |
| Phase-04-02 | `1.1.2` | `develop/1.1.2` | 已完成；PR #56 于 2026-09-03 合入 `main`（`4ce7feb`） |
| Phase-04-03 | `1.1.3` | `develop/1.1.3` | Review 整改本地完成；待远程门禁与合入 |

执行规则：

- 每批全部提交共享该批目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-04/Phase-04-XX-*.md`，只记录实际工作、实际验证、偏差和限制。
- 完成或已经打开 Pull Request 后不在原分支执行下一批；批次数量或顺序变化时先更新本表，已推送分支不得静默改名或重新编号。
- Phase-04-01 交付完整 HTTP 请求到日志闭环；Phase-04-02 交付后台进程日志并在相同契约下完成阶段级集成验收。
- Phase-04-03 是实现 Review 发现后的边界整改批次，只关闭 development JSON Lines、已提交响应 panic 语义和权威治理三项 P2，不扩展一般审计或日志平台能力。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 基于 Go 标准库 `log/slog` 的统一 JSON 日志构造、字段规范、context 传递和可注入输出。
- Backend 全局服务端 `request_id`、`X-Request-ID` 响应头、结构化访问日志和结构化 panic Recovery。
- 全部现有 `/api/v1` 路由的请求完成日志，以及注册、登录、退出、发帖、评论、点赞、取消点赞和通知已读的业务成功日志。
- 统一错误响应向访问日志提供安全 `error_code`，既有缓存降级日志携带当前请求上下文。
- Backend 生命周期、Outbox Dispatcher、Business Worker、Search Indexer 和 search-reindex 的结构化日志。
- 使用 `event_id` 关联异步处理成功、retry、dead-letter 和恢复过程，保持既有消息契约不变。
- Bash 验收脚本的 JSON 日志解析、字段匹配、敏感信息负面断言和真实跨进程验收。
- 必要的 README 使用说明、版本元数据和对应实施记录随实现批次完成。

### 4.2 明确不做

- 不实现 LogMonitor、Kafka 日志传输、Marshaller、Elasticsearch 日志索引、日志查询 API 或可观测前端。
- 不实现文件日志、轮转、压缩、保留策略、远程 sink、采样、动态日志级别或运行期重载。
- 不引入 OpenTelemetry、trace/span、分布式追踪或 HTTP `request_id` 跨 RabbitMQ 传播。
- 不改变业务事件 Envelope、AMQP headers、routing key、Outbox Schema、通知幂等或搜索索引契约。
- 不迁移 `cmd/migrate` 的人类可读 CLI 输出；该命令不属于本阶段真实业务日志源。
- 不记录 Frontend 日志，不改 Frontend 产品功能；Frontend 文件只按版本规则同步元数据。
- 不修改冻结的 `scripts/*.ps1`，不增加原生 Windows 验收。
- 不开展日志容量压测、生产日志平台选型、全量代码审计或与验收无关的机会性重构。

## 5. 结构化日志契约

### 5.1 输出与基础实现

- 在 Backend Go module 内建立单一日志基础包，封装 `slog.JSONHandler`、字段规范、context logger 和测试 writer；生产边界显式创建并注入 logger，不依赖散落的全局 `log.Printf`。
- 所有本阶段范围内的应用日志写入 stdout，每条记录只占一行并且是一个完整 JSON object；stderr 不作为第二套日志 Schema。
- 第一版固定最低等级为 info，不增加 `LOG_LEVEL` 环境变量。代码仍按 info、warn、error 发出正确等级，为后续阶段保留扩展点。
- Handler 必须把 `slog` 默认的时间、等级和消息键规范为本方案字段；时间统一使用 UTC RFC3339Nano，level 统一为小写。
- 动态标识、状态和原因只能进入独立属性；`message` 使用固定、低基数英文短语，不拼接 ID、错误文本或用户输入。
- 日志包必须允许测试注入 `io.Writer` 并逐行反序列化；测试不得依赖正则解析人类文本。

### 5.2 公共字段

| 字段 | 类型 | 适用范围 | 约束 |
| --- | --- | --- | --- |
| `log_schema_version` | integer | 所有记录 | 固定为 `1` |
| `timestamp` | string | 所有记录 | UTC RFC3339Nano |
| `level` | string | 所有记录 | `info`、`warn` 或 `error` |
| `service` | string | 所有记录 | 固定服务名，不使用任意配置 |
| `module` | string | 所有记录 | 固定模块名，低基数 |
| `message` | string | 所有记录 | 固定英文短语，不承载动态值 |
| `request_id` | string | HTTP 请求域记录 | 32 位小写十六进制；不得为空或由客户端指定 |
| `event_id` | string | 异步事件域记录 | 使用既有 Envelope event ID |
| `event_type` | string | 异步事件域记录 | 使用既有稳定事件类型 |
| `user_id` | integer | 已认证请求或业务动作 | 仅在已确认正整数时输出 |
| `method` | string | HTTP 完成记录 | 标准大写 HTTP method |
| `route` | string | HTTP 完成记录 | Gin route template 或固定 `unmatched` |
| `status` | integer | HTTP 完成记录 | 最终 HTTP status code |
| `duration_ms` | integer | HTTP 完成记录 | 非负整数毫秒 |
| `response_bytes` | integer | HTTP 完成记录 | 非负响应字节数 |
| `error_code` | string | HTTP 错误完成记录 | 只使用公共安全应用错误码 |
| `reason` | string | 可恢复或终止的内部状态 | 使用代码定义的有限原因，不记录原始 Payload/响应 |

后台或生命周期日志不伪造空 `request_id`；异步日志用 `event_id` 作为自身相关标识。缺少不适用的可选字段时直接省略，而不是写空字符串、零值或 `null`。

### 5.3 固定服务与模块

- `service=backend`：`lifecycle`、`http`、`auth`、`post`、`comment`、`like`、`notification`、`search`、`cache`、`outbox`。
- `service=business-worker`：`lifecycle`、`worker`、`notification`。
- `service=search-indexer`：`lifecycle`、`worker`、`search`。
- `service=search-reindex`：`lifecycle`、`search`。

模块和 message vocabulary 在日志基础包或所属组件中以常量/固定调用点维护，不开放为客户端输入或任意环境变量。

## 6. HTTP 请求关联与日志语义

### 6.1 `request_id` 契约

- 最外层 request-id middleware 使用 `crypto/rand` 生成 16 bytes，并编码为 32 位小写十六进制字符串。
- 无论客户端是否发送 `X-Request-ID`，Backend 都忽略该值并生成新 ID，防止伪造、冲突和跨用户日志污染。
- ID 在调用下游 Handler 前写入 request context 和 `X-Request-ID` 响应头；不修改现有 JSON 成功或错误 Envelope。
- 随机源失败时不回退时间戳、计数器或客户端值；请求以统一 `internal_error` 500 中止并输出不含伪造 `request_id` 的结构化 error 记录。
- health、readiness、API、404 和 Recovery 都经过同一最外层链路；每个已生成 ID 的请求只产生一条最终 HTTP 完成日志。

### 6.2 Middleware 顺序与完成日志

Gin 全局顺序固定为：

```text
request-id → access logger → structured recovery → route/authentication/handler
```

该顺序保证 Recovery 写入 500 后，access logger 能读取最终状态并记录正确等级。完成日志固定 `module=http`、`message="http request completed"`，并包含：

- `request_id`
- HTTP `method`
- Gin route template；未匹配路由使用固定 `unmatched`，不得记录原始 URL、query string 或路径参数原文
- 整数 `status`、`duration_ms` 和 `response_bytes`
- 认证成功后可附正整数 `user_id`
- 通过统一错误响应产生错误时附安全 `error_code`

等级规则固定为：2xx/3xx 使用 info，4xx 使用 warn，5xx 使用 error。Recovery 额外输出一次固定 `message="http panic recovered"` 的 error 日志，但不输出 panic 值、堆栈、请求正文或 headers。

### 6.3 业务动作日志

读取接口只依赖 HTTP 完成日志；状态变更接口在业务事实成功提交后增加一条同 `request_id` 的业务日志：

| 动作 | module | message | 允许的动态字段 |
| --- | --- | --- | --- |
| 注册成功 | `auth` | `user registered` | `user_id` |
| 登录成功 | `auth` | `user logged in` | `user_id` |
| 退出 | `auth` | `user logged out` | 已知时附 `user_id` |
| 发帖成功 | `post` | `post created` | `user_id`、`post_id` |
| 评论成功 | `comment` | `comment created` | `user_id`、`post_id`、`comment_id` |
| 点赞成功 | `like` | `post liked` | `user_id`、`post_id` |
| 取消点赞成功 | `like` | `post unliked` | `user_id`、`post_id` |
| 通知已读 | `notification` | `notification marked read` | `user_id`、`notification_id` |

业务验证失败、认证失败、未找到、依赖不可用和内部错误不伪造成功日志；由最终 HTTP 完成日志记录状态与安全 `error_code`。既有 Redis 缓存读取、回填或失效失败保持非阻断语义，但改为 warn 结构化记录，并从请求 context 继承 `request_id`。

## 7. 后台进程与异步事件日志

### 7.1 Backend 生命周期与 Outbox

- Backend 启动、监听、正常停止、异常终止和资源关闭失败使用 `service=backend,module=lifecycle`。
- Outbox 每条成功发布记录 `message="outbox event published"`，只包含 `event_id`、`event_type` 和允许的内部数值 ID；不得记录 JSON Payload、RabbitMQ URL 或消息 body。
- publish/release/cleanup 的失败继续遵循既有重试与非阻断规则，日志使用有限 `reason`，不因日志改造改变租约、confirm、清理或退出行为。
- 周期性空轮询不记录日志，避免无业务价值的高频噪声。

### 7.2 Business Worker 与 Search Indexer

- 将 Worker `func(string, ...any)` 格式化回调替换为显式结构化 logger 依赖；Runtime 与 Handler 测试注入 buffer 或 discard logger。
- 事件成功 ack 后记录 `message="event processed"`；retry confirm 并 ack 后记录 `message="event retry scheduled"`；dead publish confirm 并 ack 后记录 `message="event dead lettered"`。
- 以上记录包含 `event_id`、`event_type`、`attempt` 和固定 `reason`；Search Indexer 可附 `post_id`，Business Worker 不记录通知正文或完整 Envelope。
- 连接不可用、session 中断和恢复分别使用固定 lifecycle/worker message；重连循环不得在每次短轮询输出无界日志，同一现有重连周期只记录状态转换。
- 任何日志调用失败不得改变 ack/nack、retry/dead、重连或 cancellation-safe shutdown 的控制流。

### 7.3 search-reindex

- search-reindex 用 `service=search-reindex` 记录开始、无需重建、成功完成和安全失败。
- 保留 CLI 退出码和 `--if-missing` 语义；脚本不得通过解析 message 决定业务正确性。
- 不记录索引正文、查询 DSL、完整 Elasticsearch 响应、PIT、数据库连接信息或凭据。

`cmd/migrate` 继续保留现有面向操作者的文本结果，不纳入本阶段统一日志验收。

## 8. 敏感信息与基数边界

以下内容不得进入任何本阶段范围内的日志字段或 message：

- 用户名、密码、密码哈希、JWT、Cookie 名值、认证 header。
- 帖子标题/正文、评论内容、搜索词、请求/响应 body、原始 query string。
- IP、User-Agent、Referer 或其他客户端指纹。
- MySQL DSN、Redis/AMQP/Elasticsearch 完整 URL、用户名、密码和环境密钥。
- AMQP Payload/headers 原文、Elasticsearch DSL/原始响应、PIT ID、cursor 内容和底层 SQL。
- 原始 panic 值、未归一化第三方错误体或可能包含上述内容的 `%v` 输出。

允许的标识限于已验证的数值资源 ID、服务端 `request_id`、既有 `event_id`、稳定 `event_type`、HTTP route template、公共 `error_code` 和有限 `reason`。JSON Handler 负责正确转义；代码不得手工拼接 JSON。

## 9. 内部接口与数据流调整

- 新日志包负责创建带 `log_schema_version`、`service` 的 `*slog.Logger`，提供 module child logger、request context 写入/读取和 JSON writer 测试辅助边界。
- HTTP request-id middleware 的生成器允许测试注入失败或固定 ID；生产只使用 `crypto/rand` 实现。
- 统一响应层只向 Gin context 写入安全 `error_code` 元数据，公共 HTTP body、状态映射和 `apperror` 契约保持兼容。
- Handler 和已有降级日志点从 request context 取得 request-scoped logger；无法取得时使用显式注入的同 service/module fallback，不创建第二种文本格式。
- Outbox、Worker Runtime/Handler 和命令入口通过构造参数接收 `*slog.Logger`；不得依赖修改全局标准 logger 来隐式迁移。
- Phase 4 不新增持久数据结构、API JSON 字段、AMQP 字段或 Frontend DTO。

## 10. 跨批次依赖与摘要

```text
Milestone-01-Release（1.0.0）
  ↓
Phase-04-01 HTTP 请求链路与业务日志闭环（1.1.1）
  ↓
Phase-04-02 后台进程日志与阶段收口（1.1.2）
  ↓
Phase-04-03 Review 整改与阶段收口（1.1.3）
```

- [Phase-04-01：HTTP 请求链路与业务日志闭环](Phase-04-01-HTTP请求链路与业务日志闭环.md)：交付统一日志基础、服务端 request ID、所有主要 API 的访问/业务/错误日志和真实 HTTP 日志验收。
- [Phase-04-02：后台进程日志与阶段收口](Phase-04-02-后台进程日志与阶段收口.md)：统一 Outbox、Business Worker、Search Indexer 和 search-reindex，更新 JSON 日志断言并执行阶段级完整矩阵。
- [Phase-04-03：Review 整改与阶段收口](Phase-04-03-Review整改与阶段收口.md)：关闭默认 development Gin 文本输出、已提交响应后的 panic 混合 payload/错误等级以及 Phase 4 权威状态三项 Review 问题。

三个纵向批次符合阶段提纲的 2～3 批约束：第一批形成真实用户请求到可关联日志的最小闭环，第二批扩展为完整运行进程集并完成跨进程验收，第三批只关闭实现 Review 发现的三项明确缺口；没有按日志库、中间件、Handler 和测试层机械拆分。

## 11. 测试策略与固定矩阵

### 11.1 执行效率与停止规则

- 每批先从详细方案提取“新增日志行为 → 最低测试层 → 固定门禁”，只读直接受影响的入口、日志调用点和测试，并在 10 分钟内进入实现。
- 没有具体编译、运行或必需测试失败时不读取第三方依赖源码；新测试只证明本方案字段、关联、安全或直接改变的日志接口。
- 同一个字段或安全事实优先在日志包/中间件最低层测试一次，再由真实脚本证明跨组件闭环；不为所有状态码、路由与字段组合建立全排列。
- 最终 diff 上固定门禁各执行一次；失败后只重跑受修复影响的项目。上下文压缩不触发重新运行已经成功且环境未变化的检查。
- 固定门禁通过且没有阻断验收的失败后立即更新实施记录、版本并提交，不追加日志平台、压测、采样或机会性业务重构。

### 11.2 批次验证边界

| 批次 | 本批直接证据 | 固定必要回归 | 明确留后/不重复 |
| --- | --- | --- | --- |
| Phase-04-01 | JSON Handler、request-id/access/recovery、业务动作、错误码、真实 HTTP 日志与敏感哨兵 | 受影响 HTTP/业务 package、Backend 启停、Bash 日志模式、版本/分支治理 | 不改 Worker/Outbox 消息日志；不跑完整 Phase 3 故障矩阵 |
| Phase-04-02 | Outbox/Worker/Indexer/reindex JSON 日志、event_id、retry/dead、跨进程解析 | Backend 全量、Worker race/integration、Frontend 固定门禁、完整业务矩阵、远程 CI | 不实现 LogMonitor/Kafka/存储；不追加日志吞吐和全排列故障测试 |
| Phase-04-03 | development JSON Lines、已提交响应 panic 语义、权威分支/版本收口 | HTTP middleware/router、Backend 全量/race、脚本治理、focused logging | 不重复完整故障矩阵；不引入响应缓冲或公共 API 变化 |

### 11.3 阶段级端到端验收矩阵

`scripts/verify-business.sh` 在既有随机 token、独立 Compose project/数据库/端口/进程目录/volume 和归属校验下增加并固定覆盖：

1. 每个注册路由的代表性真实请求响应 `X-Request-ID`，ID 为 32 位小写十六进制，客户端提供的哨兵 ID 不被采用。
2. Backend 捕获文件中的应用记录逐行是合法 JSON，并包含固定 Schema 与基础字段；同一请求只有一条最终完成日志。
3. 注册、登录、退出、发帖、评论、点赞、取消点赞和通知已读成功日志与对应 HTTP 完成日志共享 request ID，资源 ID 正确。
4. 代表性 400、401、404、503 与测试层 panic 分别产生正确等级、状态和安全 `error_code`；不出现虚假业务成功日志。
5. Redis 降级的代表性 warn 继承 request ID，业务响应继续按既有 MySQL fallback 契约成功。
6. Outbox 发布与 Business Worker 通知处理通过同一 event ID 关联，重复投递仍只产生一个通知事实。
7. Business Worker 临时失败产生结构化 retry 日志，非法消息产生结构化 dead-letter 日志，恢复后的有效事件继续处理。
8. Search Indexer 正常、暂停/恢复和代表性 Elasticsearch/RabbitMQ 故障日志可解析，搜索最终一致性保持不变。
9. search-reindex 的开始/结果日志符合相同 Schema，精确重建仍由 MySQL 恢复搜索索引。
10. 日志中不存在验收注入的用户名、密码、标题、正文、评论、搜索词、Cookie/JWT、连接 URL、Payload、DSL 或 PIT 哨兵值。
11. Phase 0～3 注册、认证、帖子、评论、点赞、通知、搜索、缓存/消息降级与生命周期矩阵仍共同运行，日志改造不改变 HTTP、数据库或消息结果。
12. 成功、失败或中断清理只作用于验收归属资源；日志断言不读取或删除日常用户日志与数据。

以上是封闭矩阵。除非真实失败表明共享 logger、context、Worker 或脚本改变引入了具体回归，不增加日志压力、磁盘故障、stdout backpressure 或后续采集链路测试。

## 12. 实施记录规则

每批完成后创建同名镜像记录：

```text
dev/imple/Phase-04/Phase-04-XX-<名称>.md
dev/logs/Phase-04/Phase-04-XX-<名称>.md
```

记录必须包含实际完成工作、变更文件、验证命令与结果、相对方案偏差、已知限制和跟进项。规划阶段不提前创建空记录，也不得把本方案列出的命令写成已经通过。

## 13. Phase 4 阶段验收标准

- 所有范围内应用日志逐行输出统一 JSON，基础字段、类型、时间、等级、服务和模块满足 Schema v1。
- 全部主要 API 具有服务端 request ID 与完成日志；状态变更接口的业务成功日志可用同一 ID 关联，客户端不能控制该 ID。
- 正常、客户端错误、服务端错误和 panic 的状态、等级和安全错误码正确；响应未提交的 panic 返回统一 500，响应已提交的 panic 不追加混合错误 Envelope，并以 error 完成日志明确实际状态和提交标记。
- Outbox、Business Worker、Search Indexer 与 search-reindex 使用同一日志契约，并可通过 event ID 关联代表性发布、处理、retry/dead 和恢复过程。
- 日志不包含第 8 节禁止的用户内容、认证材料、连接凭据、Payload、DSL、PIT、原始 URL 或高基数字段。
- 日志改造不改变 Outbox 原子性/租约、RabbitMQ confirm/ack、Worker 重连/退出、通知幂等、搜索重建/增量或缓存降级语义。
- 第 11.3 节固定矩阵与远程 Branch governance、Backend、Frontend、Scripts and Compose、Integration 和自动 PR 编排门禁通过，没有使阶段验收不成立的失败。
- 三份实施记录与实际提交、命令和限制一致；Phase-04-03 完成后根与 Frontend 版本均为 `1.1.3`。
- 非阻断增强没有扩大当前阶段；动态日志级别、采样、传输、存储和查询均保留给明确的后续 Phase。

## 14. 完成、停止与后续交接

Phase-04-01 与 Phase-04-02 已从权威分支完成并合入主远程 `main`。Phase-04-03 的本地整改、固定矩阵和实施记录完成后，仍须由该权威分支通过远程门禁并合入最新 `main`，Phase 4 才可标记最终完成。达到条件后停止扩展，不因尚未建设日志平台而延长本阶段。

向 Phase 5 交付：

- 已验证的 JSON 日志 Schema、service/module/message 约束和 Go `slog` 构造模式，供首个 Exporter 保持一致的自身运行日志；若 Exporter 建立独立 Go module，则复用契约而不是直接越界导入 Backend `internal` package。
- 仍可产生真实业务、通知和搜索流量的 `1.1.3` 系统，不改变 Phase 5 的指标采集范围。

向 Phase 9 预留：

- Backend、Business Worker、Search Indexer 和 search-reindex 的稳定 stdout JSON Lines 数据源。
- request ID 与 event ID 两种明确的相关键，以及已验证的敏感信息和基数边界。
- LogMonitor 负责后续采集、校验和标准消息封装；不得回到 Phase 4 增加 Kafka、存储或查询职责。
