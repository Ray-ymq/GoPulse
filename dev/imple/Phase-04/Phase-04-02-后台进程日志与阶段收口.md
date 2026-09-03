# Phase-04-02：后台进程日志与阶段收口实施方案

> 执行序号：2 / 2
>
> 前置批次：Phase-04-01 已完成、合入主远程并通过验收
>
> 总方案来源：[Phase-04-总实施方案.md](Phase-04-总实施方案.md)

## 1. 批次目标

在 Phase-04-01 已验证的 HTTP JSON 日志闭环上，将 Backend 生命周期与 Outbox Dispatcher、Business Worker、Search Indexer 和 search-reindex 全部迁移到相同 Schema，并用 `event_id` 关联异步发布、处理、retry/dead 和恢复过程。

本批同时把既有文本日志验收升级为 JSON 字段断言，在一次封闭的真实 Phase 0～3 业务矩阵中证明 HTTP request ID、异步 event ID、搜索重建、依赖故障和敏感信息边界可以共同运行。完成后 Phase 4 在 `1.1.2` 收口，不增加独立纯验收批次。

## 2. 前置条件

- Phase-04-01 已由 `develop/1.1.1` 合入配置的主远程 `main`，远程必要门禁成功，根与 Frontend 版本均为 `1.1.1`。
- 已 fetch 主远程并从最新 `main` 创建 `develop/1.1.2`；工作树、日常应用进程与基础设施资源状态已经记录。
- HTTP logger、request ID、access/recovery、业务动作和 `--logging-live` 已通过本批不重复建设；只在后台进程接入暴露阻断兼容问题时做最小修复。
- Phase 2/3 的 Outbox 原子性与清理、租约预算、RabbitMQ confirm/ack、Worker retry/dead/重连/退出、通知幂等、搜索增量/重建和脚本所有权是必须保持的回归边界。
- 实施、应用测试和阶段验收在 WSL2 Linux filesystem 中完成；Bash 是唯一维护入口，PowerShell 保持 `0.2.1` 历史能力。
- 初始发现只检查现有日志包、Outbox/Worker 日志接口、四个命令入口和日志相关脚本断言，并在 10 分钟内进入第一个 in-scope 改动。

## 3. 实施范围

### 3.1 Backend 生命周期与 Outbox

- 复用 Phase-04-01 已由 `cmd/server` 创建并传给 HTTP 的 `service=backend` logger，将其继续显式注入 Outbox 和生命周期边界；不通过 `slog.SetDefault` 或重定向全局标准 logger 隐式完成迁移。
- Backend 监听、正常关闭、异常退出和资源关闭失败使用 `module=lifecycle` 与固定 message；地址、配置值和原始错误不进入日志。
- Outbox Dispatcher 构造选项增加显式 `*slog.Logger`，生产注入 `module=outbox` child，测试注入 buffer/discard logger。
- 每条 event confirm 并成功标记 published 后记录 `outbox event published`，包含 `event_id`、`event_type` 和必要的数值 outbox ID；不记录 Envelope JSON、AMQP message/body/header 或连接 URL。
- claim/publish/release/cleanup 失败使用既有有限错误分类映射为固定 `reason`；不能把 `%v` 原始错误重新带入 message。
- 空 claim、空 cleanup 和正常 poll tick 不输出日志；同一失败循环保持有界频率，不因 logger 增加 busy-loop。
- 日志写入不改变 Outbox transaction、lease owner、整批租约预算、publisher confirm、失败释放、retention 或 cancellation 语义。

### 3.2 Worker 结构化日志接口

- 将 `RuntimeOptions.Logger` 和 `HandlerOptions.Logger` 从 `func(string, ...any)` 改为显式 `*slog.Logger`；Runtime 根据固定 profile 使用对应 service/module child。
- Business Worker 使用 `service=business-worker`，Search Indexer 使用 `service=search-indexer`；profile 不允许从任意环境变量覆盖 service。
- Handler 在以下已完成状态记录一次结构化日志：
  - Processor 成功且 ack 成功：`message="event processed"`。
  - retry publish confirmed 且原消息 ack 成功：`message="event retry scheduled"`。
  - dead publish confirmed 且原消息 ack 成功：`message="event dead lettered"`。
- 记录 `event_id`、`event_type`、`attempt` 和固定 `reason`；Search 事件可附 `post_id`，通知事件不得附正文、recipient 快照或 Payload。
- Business Worker 的 self-event 忽略成功 ack 后记录一次 `message="event ignored"`、`reason="self_event"`，只附 event ID/type/attempt；不得产生通知副作用或记录 recipient/Payload。
- retry/dead publish 失败、ack/nack 失败、connection/channel/delivery stream 中断和 shutdown timeout 使用固定 error/warn message，不输出 AMQP 原始错误或连接 URL。
- 重连日志只表达 `connection unavailable`、`session interrupted`、`connection restored` 等状态转换；保留现有指数退避与 jitter，不为每次等待 tick 输出记录。
- 任何日志路径都不得改变 secondary publish confirm 后再 ack、失败 requeue、有限 retry、dead queue 或在途 handler 回收顺序。

### 3.3 命令入口与 Processor 结果

- `cmd/business-worker` 与 `cmd/search-indexer` 在读取配置前建立各自固定 service logger；启动、停止与安全失败均输出 JSON。
- Worker/Indexer 的 MySQL close 失败与初始化失败只记录固定阶段/reason，不输出 DSN、凭据或原始第三方响应。
- Notification 与 Search Processor 不重复输出 Handler 已记录的相同成功结果；需要领域 ID 时通过安全结果/上下文传给单一成功日志点，不能复制 Payload。
- `cmd/search-reindex` 使用 `service=search-reindex,module=search` 输出开始、`--if-missing` 无需变更、成功和失败；保留参数、退出码、alias 原子切换和 MySQL 重建语义。
- reindex 日志可以记录安全的 `document_count`、`batch_size` 和固定 `result`，但不得记录 generation/物理索引名、文档、标题/正文、索引 DSL、连接地址、原始响应或 PIT。
- `cmd/migrate` 明确保留现有文本 CLI 输出和退出语义，不作为 Phase 4 日志验收对象。

### 3.4 验收脚本 JSON 化

- 将 `wait_log_contains` 的 Worker retry 文本 grep 替换为逐行 JSON 解析和字段匹配，固定断言 service、module、message、event ID/type、attempt、reason。
- JSON matcher 必须有 timeout、失败时只打印经过现有临时目录归属确认的有限 tail，并避免把敏感环境变量写入诊断命令。
- 扩展 `--logging-live` 覆盖 Outbox、Business Worker、Search Indexer 和 search-reindex；本批改动了该模式与后台实现，因此允许在最终 diff 上重新运行一次。
- 在默认完整 `scripts/verify-business.sh` 中保留 Phase 3 的业务/故障矩阵，并增加最终日志 Schema、相关键和敏感哨兵断言；不另建重复的第二套完整脚本。
- 每个应用日志文件只解析本应用产生的 JSON；`[gopulse-acceptance]`、Compose、Frontend/Vite 和脚本操作者输出不冒充应用日志 Schema。
- 对进程重启前后的同一日志文件持续解析，证明恢复后仍使用相同 service/schema，没有回退文本 `log.Printf`。

### 3.5 阶段集成收口

- 使用真实注册、认证、帖子、评论、点赞、通知、搜索、Redis fallback、RabbitMQ/Worker/Indexer/Elasticsearch 暂停恢复和 reindex 路径产生业务与日志。
- HTTP 继续以 request ID 关联同步请求；Outbox/Worker/Indexer 以 event ID 关联异步阶段。明确不要求两种 ID 跨 Envelope 连接。
- 对同一 event 的 Outbox published、Worker/Indexer processed/retry/dead 记录按现有至少一次语义解释；不得把日志条数当作数据库幂等事实。
- 最终业务正确性仍由 HTTP、MySQL、RabbitMQ 拓扑与 Elasticsearch 搜索结果断言；日志只证明可观测过程，不能替代业务验收。
- 完整矩阵通过且无阻断失败后更新 README、版本和实施记录，立即停止 Phase 4 扩展。

## 4. 接口与兼容性

### 4.1 内部 Logger 接口变化

- Worker Runtime/Handler 构造选项改为 `*slog.Logger`，所有 production caller 与现有 fake/test caller同步迁移。
- Outbox Dispatcher 增加显式 logger 依赖；未提供 logger 的测试必须获得确定性的 discard logger，而生产装配不得省略。
- 事件处理日志属性使用现有 Envelope 字段，不新增或改变 AMQP wire shape。
- 日志基础包仍位于 Backend module 的 internal 边界；未来独立组件复用 Schema 与构造模式，不越界导入。

### 4.2 外部兼容性

- HTTP `X-Request-ID` 与 JSON 业务响应保持 Phase-04-01 契约。
- RabbitMQ exchange/queue/routing key、Envelope、attempt header、delivery mode、confirm/ack 和 retry/dead 行为不变。
- MySQL、Redis 和 Elasticsearch Schema/Mapping/aliases 不变。
- `dev.sh`、`down.sh` 和完整验收的命令入口及进程所有权语义不变；只改变范围内 Go 应用输出格式和日志断言。
- search-reindex 参数与退出码不变；脚本不得依赖旧的人类文本完成消息。

## 5. 实施边界与非目标

- 不把 request ID 写入 Outbox、Envelope、AMQP headers、notifications 或 Elasticsearch 文档。
- 不改变通知/搜索 Processor 的业务结果，不重写 Worker 状态机或 Outbox Dispatcher 调度器。
- 不实现 log shipping、Kafka topic、LogMonitor、Marshaller、Elasticsearch 日志索引或查询 UI。
- 不增加文件 sink、轮转、保留、采样、动态 level、结构化 trace/span 或指标。
- 不把 RabbitMQ management 消息数或日志行数当作最终通知/搜索事实。
- 不迁移 `cmd/migrate`，不修改 Frontend 产品代码、Compose 拓扑或 PowerShell。
- 不重复 Phase-04-01 所有 route/status 单测；本批只回归公共 logger/context 接口和完整业务矩阵。
- 不开展日志吞吐、磁盘满、stdout backpressure、采集端中断或生产容量测试。

## 6. 预计文件与交付物

预计触达：

```text
backend/internal/observability/logging/
backend/internal/outbox/
backend/internal/worker/
backend/internal/notification/
backend/internal/search/
backend/cmd/server/
backend/cmd/business-worker/
backend/cmd/search-indexer/
backend/cmd/search-reindex/
scripts/verify-business.sh
scripts/ci/
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-04/Phase-04-02-后台进程日志与阶段收口.md
```

预计文件是允许触达的上限；不为迁移日志而改动数据库 migration、Frontend 源码、Compose 服务或冻结 PowerShell。

## 7. 详细实施步骤

1. 核对 Phase-04-01 实施记录、最新 `main=1.1.1`、JSON Schema 和尚未迁移的文本日志清单，提取本批最小门禁。
2. 为 Backend lifecycle 与 Outbox 注入 module logger，先保持控制流不变，再用测试固定 success/failure 字段与空轮询静默。
3. 将 Worker Runtime/Handler logger 改为 `*slog.Logger`，同步 Business/Search production 装配和现有测试 fake。
4. 在 ack/retry/dead 已完成边界增加一次日志，使用 Envelope 安全字段和固定 reason；通过现有状态机测试证明日志不改变 ack/nack 顺序。
5. 增加连接不可用、中断、恢复和关闭的有限结构化记录，保留重连退避与在途 handler join。
6. 迁移 business-worker/search-indexer 命令生命周期和资源关闭日志，清除范围内旧 `log.Printf`。
7. 迁移 search-reindex 的开始/无操作/完成/失败输出，保持 CLI 参数、退出码和重建正确性。
8. 把验收脚本文本 marker 改为 JSON matcher，扩展 `--logging-live` 的四服务与敏感哨兵断言。
9. 在隔离依赖上运行定向 Worker/Outbox/Search integration，再运行一次完整 Phase 0～3 业务与日志矩阵。
10. 核对范围内没有残留应用文本日志或手工 JSON 拼接，更新 README、版本和实施记录。
11. 远程门禁成功并合入后记录实际 PR/commit/status；没有证据时不得提前写成 Phase 4 已完成。

## 8. 风险与控制

- **日志接口改变 Worker 时序**：只在状态完成后记录，现有 ack/nack/confirm 顺序测试保持并做 race 回归。
- **日志失败影响消息处理**：日志不返回业务错误，不参与 ack、retry/dead 或数据库事务决策。
- **重复投递导致错误结论**：event ID 日志允许多次尝试，最终幂等仍由 notification unique key 和固定 search document ID 证明。
- **原始错误泄漏凭据/Payload**：Worker/Outbox 仅输出有限 reason；敏感哨兵和 URL/Payload 负面扫描进入固定矩阵。
- **重连刷屏**：按状态转换记录，不在每次 poll/backoff tick 输出；真实暂停恢复检查日志数量保持有界。
- **脚本 JSON 解析误判**：应用日志与脚本/Compose 输出分文件解析，按完整字段匹配，不用 substring 代表成功。
- **reindex 输出变化破坏生命周期**：脚本依赖退出码和最终 alias/Search API，不依赖 message 文本。
- **跨组件改造引入回归**：本批具有共享日志与 Worker 接口风险，因此运行 Backend 全量、race、integration 和一次完整业务矩阵；通过后停止扩展。

## 9. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败后只重跑受修复影响的项目：

```bash
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd backend && go test -count=1 -tags=integration ./internal/outbox ./internal/worker ./internal/notification ./internal/search)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-business.sh --logging-live
scripts/verify-business.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.1.2 --base-ref upstream/main
git diff --check
```

本批修改 Outbox、共享 Worker、两个消费者命令、重建命令和阶段验收脚本，属于明确的跨组件风险，因此执行 Backend 全量/race、受影响真实 integration、Frontend 固定门禁和一次完整业务矩阵。`--logging-live` 是本批直接日志证据，完整脚本是阶段业务回归；两者职责不同，不再追加第二套浏览器、Broker 或搜索故障全排列。

完整应用验收只能在 WSL2 Linux filesystem 和可确认归属的隔离资源中执行。远程 Pull Request 必须通过仓库实际配置的 Branch governance、Backend、Frontend、Scripts and Compose、Integration 与自动 PR 编排门禁。

## 10. 验收标准

- Backend lifecycle/Outbox、Business Worker、Search Indexer 和 search-reindex 的范围内输出逐行符合 Phase 4 JSON Schema，没有回退文本 `log.Printf` 或手工 JSON。
- service/module/message 固定且低基数；timestamp、level 和字段类型正确，不适用的 request/event 字段被省略。
- Outbox publish 与 Worker/Indexer 事件日志可按 event ID 关联，成功、retry、dead 和恢复记录出现在实际状态完成后。
- Worker logger 改造不改变 secondary confirm、ack/nack、requeue、有限 retry/dead、重连退避或有界退出。
- search-reindex 的结构化输出、退出码和 MySQL → 新物理索引 → alias 切换闭环正确，无操作路径可验证。
- 进程停止/恢复后仍输出同 Schema；连接故障日志数量有界，不在每次 poll tick 刷屏。
- HTTP request ID、业务日志、缓存 warn 与本批 event ID 日志能在同一次真实业务运行中共同解析，但不伪造跨异步 request ID。
- 日志不包含用户内容、凭据、Cookie/JWT、连接 URL、Envelope/Payload、DSL、原始响应、PIT 或验收敏感哨兵。
- Phase 0～3 业务、通知、搜索、降级与生命周期结果无回归；日志断言没有替代最终数据库、消息和 Search API 事实。
- 第 9 节固定门禁和远程门禁通过；版本元数据为 `1.1.2`，两批实施记录真实完整。

## 11. Phase 4 明确完成条件

只有以下条件全部满足，才可标记 Phase 4 完成：

1. Phase-04-01 和 Phase-04-02 均从总方案分配的独立分支完成并合入最新主远程 `main`。
2. HTTP request ID/业务/错误日志与后台 event ID/生命周期日志共同符合 Schema v1 和敏感信息边界。
3. `--logging-live` 与完整 `scripts/verify-business.sh` 在 WSL2 隔离资源上通过，没有阻断 Phase 0～3 业务的失败。
4. Backend、Frontend、治理、脚本/Compose、Integration 和自动 PR 编排远程门禁成功。
5. 根与 Frontend 版本为 `1.1.2`，两份实施记录与实际提交和验证一致。

满足后立即停止。动态日志级别、采样、日志传输、存储、索引和查询均为后续事项，不以“尚未形成日志平台”为理由增加 Phase-04-03。

## 12. 后续交接

向 Phase 5 提供：

- 四类 Go 进程已验证的 Schema、字段 vocabulary 和 `slog` 构造模式。
- 可持续产生真实业务、通知与搜索流量的 `1.1.2` 系统；Exporter 若使用独立 Go module，只复用契约，不导入 Backend internal 实现。

向 Phase 9 提供：

- 可由 LogMonitor 稳定逐行读取的 Backend、Business Worker、Search Indexer 和 search-reindex stdout JSON。
- request ID 与 event ID 的职责边界、固定 service/module/message 以及敏感字段禁止清单。
- 真实正常、错误、retry/dead、故障恢复与 reindex 样本的验收方法；Phase 9 再负责采集、校验、封装、传输和存储。
