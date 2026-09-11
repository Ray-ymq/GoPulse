# Phase-14-05：自研组件基础运行指标闭环开发记录

## 1. 完成状态

**已完成，2026-09-11，产品版本 `1.11.5`，分支 `develop/1.11.5`。**

- 本批继续原任务，从已 fetch 的 `upstream/main` `3c57a73`（`1.11.4`）起步；早期基础实现提交 `2dfc8e5` 不是批次完成点。本次已补齐六组件真实运行、完整链路和所有固定门禁，不以局部测试替代验收。
- 最终真实门禁项目：`gopulse-p1401-b57c58613781`（复用既有强归属 harness 的项目命名，不表示执行 Phase-14-01）。命令正常返回 0，owned 容器/网络/卷已删除，开工前存在的资源逐一确认保留。
- 最终数值、基数、故障和退出证据位于 `.run/gopulse-p1401-b57c58613781/component-evidence.json`；该目录为本机验收产物，不提交凭据、镜像或原始业务内容。浏览器结果在同目录 `component-browser.log`，日志中的验收密码已脱敏。
- Root `VERSION`、Frontend package/lockfile、示例环境产品版本同步至 `1.11.5`。Phase 14 整体仍未完成，`Phase-14-06` 是下一批，不把本批通过当作 Phase 总验收通过。
- 原有未跟踪文件 `~` 未读取、未修改、未暂存。没有推送或打开 PR。

## 2. 实际实现与计数点

六组件通过独立内部 listener 输出固定 Prometheus text，经 Monitor 的六个非插件 targets → Router → Kafka → Marshaller → VictoriaMetrics，由 Backend 服务端固定目录查询，并在现有 Frontend Metrics 页展示。

共用代码移至仅依赖标准库的 `componentmetrics` module，四个应用通过本地 replace 引用；所有相关 Docker 构建 stage 显式复制它。生产者、Monitor、Marshaller、Backend 共用同一精确目录，Frontend 生成目录由 self-test 核对，避免四份白名单漂移。

- **Backend**：Gin middleware 在请求完成/恢复后以注册模板 `FullPath()` 记录请求次数和耗时；未知 route/method 进入预定义桶。Outbox 每 5s 执行一次 1s 超时的 pending/leased 聚合，只读 count/最早创建时间，不读 payload。没有成功快照或采样失败返回 503，不能输出假空队列。RabbitMQ publish 成功记录最近发布；MySQL 采样/检查、Redis 缓存/检查、Elasticsearch HTTP、RabbitMQ 交互记录依赖结果。
- **Business Worker**：Handle 的 success/retry/failure 与完整处理时间成对记录；成功 `Ack(false)` 的 ack 与调用耗时单独成对记录。成功处理（含按既有合同忽略 self event）推进最近成功，当前 Handle 计 in-flight，prefetch 为实际配置 credit。通知写入和 AMQP session/ack/retry 调用提供 MySQL/RabbitMQ 结果。
- **Search Indexer**：固定 create/update/delete 处理结果、耗时、in-flight、重试发布期间 retrying 和最近成功；MySQL 查询/事务、Elasticsearch 请求、RabbitMQ session/ack/retry 记录真实依赖结果。未知 operation 不产生新 tuple。
- **Monitor**：六插件和六组件的 scrape/publish 各自记录 result/count/duration，成功 scrape 更新对应 target 的 Unix 时间戳；队列长度和丢弃来自实际 enqueue/dequeue/full，running plugins 来自 manager 状态，Router 依赖来自真实 HTTP 发布。组件 collector 不引用 plugin Registry，不提供生命周期 API，失败只写安全日志和自身指标，不发布假业务零值。
- **Router**：严格 envelope/idempotency 验证后计 accepted；不可信拒绝使用 unknown/unknown；真实 Kafka callback 计 produced/rejected、对应耗时、last ack 和 Kafka 依赖。buffer records/bytes 使用锁定客户端的公开内存 getters，bytes 包含 key/header，不把 HTTP body 长度冒充 Kafka 缓冲。
- **Marshaller**：decode、每次存储、commit 分别记录固定 stage/result 与耗时；in-flight 覆盖 Handle，retrying 覆盖失败写入后的 backoff；真实写入推进固定 storage 的最近成功，拥有有效 partition lease 的成功提交推进最近 commit。永久非法记录沿既有跳过/提交路径处理，不写存储、不阻塞下一条。
- **查询/UI**：schema 2 六个固定 component source/producer/target 端到端保留；`scraped_*` 不与 producer 身份混用，消息来源为 `message_source`。保留 Redis 旧存储标签例外。Backend 依据 family tuple 数扩展组件 series/point 上限，插件原上限不变；Frontend 严格检查固定目录、标签和数值，不接受任意 PromQL。

## 3. listener、安全与生命周期

| source = producer ID | target ID | 端口 | Token 变量 |
| --- | --- | --- | --- |
| `backend` | `backend-local` | 19101 | `BACKEND_METRICS_TOKEN` |
| `business-worker` | `business-worker-local` | 19102 | `BUSINESS_WORKER_METRICS_TOKEN` |
| `search-indexer` | `search-indexer-local` | 19103 | `SEARCH_INDEXER_METRICS_TOKEN` |
| `monitor` | `monitor-local` | 19104 | `MONITOR_METRICS_TOKEN` |
| `router` | `router-local` | 19105 | `ROUTER_METRICS_TOKEN` |
| `marshaller` | `marshaller-local` | 19106 | `MARSHALLER_METRICS_TOKEN` |

- 六个 token 独立、至少 32 bytes，不复用管理 API/JWT/log-ingestion token；每个生产者只持有自身 token，Monitor 持有六个只读 token。
- 固定 `GET /internal/v1/metrics`。无/错/重复 Authorization 先返回 401；认证后按 path → method → query/body 返回 404/405/400。错误为固定安全文本，成功响应不含配置、凭据、路径/错误原文或 runtime dump。
- host mode 仅回环，container mode 为固定 service DNS + 内部网络；Compose 六个端口均未发布，Backend 公共端口访问该路径为 404，浏览器只调用 Backend API。
- 内部 listener 同步绑定，启动失败返回明确错误；runtime scrape 失败不取消业务 root、不改变业务 readiness/提交/ack。监听请求使用 root context，HTTP/消费者/outbox 共用退出 deadline，Worker/Indexer 的 log shipper 使用同一个剩余 deadline。
- 热路径仅更新固定内存槽和原子计数/耗时对，无指标网络 I/O、mutex 等待或随输入增长的 label map。

## 4. 冻结目录：family / kind / unit / labels / count

所有成功响应的无标签、dependency 和 last-success tuple 必须存在；无成功时 timestamp=0，dependency 初始 -1、真实失败 0、成功 1。带标签 counter tuple 惰性产生，count/duration 成对。每组件 body 上限均为 **262144 bytes**，sample 上限由下表精确 tuple 数求和，不接受预留任意 label。

### backend：6 families / 最大 547 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_backend_http_requests_total` | counter | count | `method`, `route`, `status_class` | 270 |
| `gopulse_backend_http_request_duration_seconds_total` | counter | seconds | `method`, `route`, `status_class` | 270 |
| `gopulse_backend_outbox_pending` | gauge | count | 无 | 1 |
| `gopulse_backend_outbox_oldest_age_seconds` | gauge | seconds | 无 | 1 |
| `gopulse_backend_outbox_last_publish_success_timestamp_seconds` | gauge | unix_seconds | 无 | 1 |
| `gopulse_backend_dependency_up` | gauge | state | `dependency` | 4 |

### business-worker：6 families / 最大 37 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_business_worker_messages_total` | counter | count | `event_type`, `result` | 16 |
| `gopulse_business_worker_message_processing_duration_seconds_total` | counter | seconds | `event_type`, `result` | 16 |
| `gopulse_business_worker_messages_in_flight` | gauge | count | 无 | 1 |
| `gopulse_business_worker_prefetch_limit` | gauge | count | 无 | 1 |
| `gopulse_business_worker_last_success_timestamp_seconds` | gauge | unix_seconds | 无 | 1 |
| `gopulse_business_worker_dependency_up` | gauge | state | `dependency` | 2 |

### search-indexer：6 families / 最大 24 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_search_indexer_messages_total` | counter | count | `operation`, `result` | 9 |
| `gopulse_search_indexer_message_processing_duration_seconds_total` | counter | seconds | `operation`, `result` | 9 |
| `gopulse_search_indexer_messages_in_flight` | gauge | count | 无 | 1 |
| `gopulse_search_indexer_retrying` | gauge | count | 无 | 1 |
| `gopulse_search_indexer_last_success_timestamp_seconds` | gauge | unix_seconds | 无 | 1 |
| `gopulse_search_indexer_dependency_up` | gauge | state | `dependency` | 3 |

### monitor：7 families / 最大 112 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_monitor_scrapes_total` | counter | count | `scraped_producer_kind`, `scraped_target_id`, `result` | 48 |
| `gopulse_monitor_scrape_duration_seconds_total` | counter | seconds | `scraped_producer_kind`, `scraped_target_id`, `result` | 48 |
| `gopulse_monitor_last_scrape_success_timestamp_seconds` | gauge | unix_seconds | `scraped_producer_kind`, `scraped_target_id` | 12 |
| `gopulse_monitor_event_queue_length` | gauge | count | 无 | 1 |
| `gopulse_monitor_plugins_running` | gauge | count | 无 | 1 |
| `gopulse_monitor_dependency_up` | gauge | state | `dependency` | 1 |
| `gopulse_monitor_event_queue_dropped_total` | counter | count | 无 | 1 |

### router：6 families / 最大 112 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_router_messages_total` | counter | count | `type`, `message_source`, `result` | 54 |
| `gopulse_router_produce_duration_seconds_total` | counter | seconds | `type`, `message_source`, `result` | 54 |
| `gopulse_router_buffered_records` | gauge | count | 无 | 1 |
| `gopulse_router_buffered_bytes` | gauge | bytes | 无 | 1 |
| `gopulse_router_last_kafka_ack_timestamp_seconds` | gauge | unix_seconds | 无 | 1 |
| `gopulse_router_dependency_up` | gauge | state | `dependency` | 1 |

### marshaller：7 families / 最大 260 samples

| family | kind | unit | labels | 最大 tuple 数 |
| --- | --- | --- | --- | --- |
| `gopulse_marshaller_records_total` | counter | count | `type`, `message_source`, `stage`, `result` | 126 |
| `gopulse_marshaller_record_processing_duration_seconds_total` | counter | seconds | `type`, `message_source`, `stage`, `result` | 126 |
| `gopulse_marshaller_records_in_flight` | gauge | count | 无 | 1 |
| `gopulse_marshaller_retrying` | gauge | count | 无 | 1 |
| `gopulse_marshaller_last_storage_success_timestamp_seconds` | gauge | unix_seconds | `storage` | 2 |
| `gopulse_marshaller_last_commit_success_timestamp_seconds` | gauge | unix_seconds | 无 | 1 |
| `gopulse_marshaller_dependency_up` | gauge | state | `dependency` | 3 |

### 精确 label allowlist 与相关性

- Backend method：注册路由的固定 method；额外 unmatched 桶仅 `GET POST PUT PATCH DELETE HEAD OPTIONS CONNECT TRACE unknown`。status_class 仅 `1xx 2xx 3xx 4xx 5xx`。dependency 仅 `mysql redis rabbitmq elasticsearch`。44 个 method/route 注册对如下，另加 `_unmatched`，sample 公式 `2 × 5 × (44 + 10) + 7 = 547`：

```text
GET /health
GET /ready
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET /api/v1/users/me
PATCH /api/v1/users/me/profile
PUT /api/v1/users/:userId/follow
DELETE /api/v1/users/:userId/follow
GET /api/v1/users/me/following
GET /api/v1/users/me/followers
GET /api/v1/posts/following
GET /api/v1/users/:username
GET /api/v1/users/:username/posts
GET /api/v1/search/users
POST /api/v1/posts
GET /api/v1/posts
GET /api/v1/bookmarks
GET /api/v1/posts/:postId
PATCH /api/v1/posts/:postId
DELETE /api/v1/posts/:postId
POST /api/v1/posts/:postId/comments
GET /api/v1/posts/:postId/comments
PUT /api/v1/posts/:postId/bookmark
DELETE /api/v1/posts/:postId/bookmark
PUT /api/v1/posts/:postId/like
DELETE /api/v1/posts/:postId/like
GET /api/v1/search/posts
GET /api/v1/notifications
PATCH /api/v1/notifications/:notificationId/read
GET /api/v1/observability/metrics
GET /api/v1/observability/metrics/catalog
GET /api/v1/observability/logs
GET /api/v1/observability/events
GET /api/v1/exporter-plugins
GET /api/v1/exporter-plugins/catalog
POST /api/v1/exporter-plugins/:pluginId/connection-test
POST /api/v1/exporter-plugins/:pluginId/install
PUT /api/v1/exporter-plugins/:pluginId/configuration
GET /api/v1/exporter-plugins/:pluginId
POST /api/v1/exporter-plugins/install
POST /api/v1/exporter-plugins/:pluginId/start
POST /api/v1/exporter-plugins/:pluginId/stop
POST /api/v1/exporter-plugins/:pluginId/update
```

- Worker event_type：`comment.created post.liked user.followed unknown`；result：`success retry failure ack`；dependency：`mysql rabbitmq`。
- Indexer operation：`create update delete`；result：`success retry failure`；dependency：`mysql rabbitmq elasticsearch`。
- Monitor result：`scrape_success scrape_failure publish_success publish_failure`；scraped 身份只允许 12 对：六个 `exporter_plugin / <redis|mysql|rabbitmq|kafka|elasticsearch|victoriametrics>-exporter-local`，六个 `component / <backend|business-worker|search-indexer|monitor|router|marshaller>-local`。dependency 仅 `router`。
- Router/Marshaller type/message_source 仅 18 对：metrics 对上述六插件 source + 六组件 source；logs 对 `backend business-worker search-indexer search-reindex`；events 仅 monitor；未知仅 `unknown/unknown`。不是任意笛卡尔积。
- Router result：`accepted rejected produced`；dependency 仅 kafka。
- Marshaller stage/result 仅七对：`consume/consumed validate/validated validate/rejected store/stored store/retried commit/committed commit/failure`。storage 仅 `victoriametrics elasticsearch`；dependency 仅 `kafka victoriametrics elasticsearch`。

Monitor 和 Marshaller 分别通过新增直接测试拒绝 extra family、额外/错误 label、重复 series、超限 sample 和非有限值；Monitor 还验证缺失 count/duration 对和合法无 tuple 初始态。Backend/Frontend 验证 component 来源、允许的 -1 dependency 和禁入业务 ID。新的共享目录/并发实现有直接 race 检查。

## 5. 最终真实数值、基数与故障证据

下表为最终 owned 项目实际返回的处理结果样本最大值及对应最近成功/进度最大值，**不是跨 result 求和**；完整结果数组保留在本机 evidence JSON。

| 组件 | 处理 family / 实测最大值 | 成功或进度 family / 实测最大值 | endpoint samples / 查询 series / 上限 |
| --- | --- | --- | --- |
| backend | `http_requests_total` = 7 | `outbox_last_publish_success_timestamp_seconds` = 1789137771 | 37 / 35 / 547 |
| business-worker | `messages_total` = 1 | `last_success_timestamp_seconds` = 1789137767 | 17 / 17 / 37 |
| search-indexer | `messages_total` = 2 | `last_success_timestamp_seconds` = 1789137771 | 12 / 12 / 24 |
| monitor | `scrapes_total` = 39 | `last_scrape_success_timestamp_seconds` = 1789137779 | 70 / 70 / 112 |
| router | `messages_total` = 84 | `last_kafka_ack_timestamp_seconds` = 1789137779 | 68 / 68 / 112 |
| marshaller | `records_total` = 84 | `last_storage_success_timestamp_seconds` = 1789137779 | 136 / 136 / 260 |

Backend endpoint 与查询 series 的少量差异来自不同采集时刻及惰性 tuple 的产生，并非要求两次观测原子一致。所有值均经真实业务/消费/传输/存储生成，没有向 VictoriaMetrics 人工补零或写入替代事实。

最终场景实际通过：

1. 两个用户、多帖的创建、评论、点赞、收藏、关注、通知、搜索、更新、删除；核对搜索目标 ID 和更新标题，而非把全文检索结果假定为唯一。
2. 六组件各两个代表性查询、所有 38 component families 的 Backend 查询，以及 Monitor 自身 `component/monitor-local` 和 Router/Marshaller 的 `metrics/backend` message_source/stage 标签；真实浏览器展示 Backend 指标与 Monitor scraped 标签，无访问 19101–19106 的浏览器请求。
3. 六个内部端点全部未认证/错 token/重复 Authorization 拒绝，path/method/query/body 次序正确，六端口未发布，Backend 公共路由 404；指标中无用户/帖子标识、正文、query 或 token，日志/API/Frontend 不泄露内部 token。
4. Redis 停止时 Backend 缓存读取降级但帖子详情仍 200，`dependency_up{redis}` 经完整链路为 0；恢复后为 1。
5. 仅 Backend metrics endpoint 经 owned 接受测试代理返回 503。Backend ready 和业务详情仍为 200；其他五组件以及六插件的成功采样时间均在故障发生后推进；plugin Registry 的 ID 集合仍只有六插件；解除故障后恢复发布。代理只改变验收 Monitor 内固定 backend 名称的解析，不是生产配置选项或认证绕过。
6. Worker SIGTERM 正常退出 0，实测 0.34s；Indexer 退出 0，实测 0.32s。两者旧 PID=0、端点不可达；替换后端点重开，新业务驱动新的消费事实和晚于替换时间的成功时间戳。
7. VictoriaMetrics 停止时，Marshaller 实际存储依赖为 0，Backend 指标查询 503，但业务 ready=200，新帖搜索投影和评论通知继续形成。恢复后两类消费者的故障期进度可查询，Marshaller VM dependency 回到 1。
8. 六插件代表性查询、Logs/Events、普通用户对 catalog/metrics 的 403、token 日志扫描均通过。

## 6. 实际验证命令与结果

| 命令 | 结果 |
| --- | --- |
| `(cd backend && go test ./...)` | 通过，包含 server/Worker/Indexer 与直接变更的 HTTP/platform/outbox/search/notification/query/logship 包 |
| `(cd monitor && go test ./...)` | 通过 |
| `(cd router && go test ./...)` | 通过；buffer getter 修正后重新执行受影响 module，其他 module 未因此重复验证 |
| `(cd marshaller && go test ./...)` | 通过；超限 fixture 校正后另执行 `go test ./internal/envelope` 通过 |
| `(cd componentmetrics && go test -race ./...)` | 通过，新共用状态与内部边界的直接检查 |
| `(cd frontend && npm test)` | 通过，72 tests |
| `(cd frontend && npm run typecheck)` | 通过 |
| `(cd frontend && npm run build)` | 通过；版本元数据升级后再构建 release 输出，源码逻辑未变，不重复完整业务验收 |
| `bash scripts/verify-component-metrics.sh --self-test` | 通过，无 Docker 访问；检查六身份、预算及 Frontend 生成目录一致性 |
| `bash scripts/verify-component-metrics.sh` | 最终运行通过，退出 0，完整场景与清理结果如上 |
| `python3 scripts/ci/validate_versions.py` | 通过，根/Frontend/示例环境版本一致 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.5 --base-ref upstream/main` | 通过 |
| `git diff --cached --check` | 通过，覆盖实现与收口记录的暂存改动 |
| `git diff --check upstream/main...HEAD` | 通过，已在实现提交后执行 |

实际镜像构建使用 `deploy/docker/backend.Dockerfile` 的 backend/business-worker/search-indexer targets、`deploy/docker/observability.Dockerfile` 的 router/marshaller/monitor targets 和 Frontend Dockerfile，标签为 `1.11.5`。共同 module/Docker stage 修正、Router getter修正后的受影响镜像均已重新构建，最终真实门禁使用修正后的镜像。版本升级后的前端 release build 单独核验，不将版本文本变化当作重跑业务场景的理由。

## 7. 偏差、故障定位与验证边界

### 实施中已发现并修复的直接问题

- Monitor plugin 状态的实际公共字段是 `ObservedState`，不是 `State`；初次编译错误已改正，相应 command/package 检查通过。
- Docker 的 exporter-package stage 也会编译 Monitor 的 package metadata CLI；初次 Monitor 镜像构建缺少本地 `componentmetrics` replace 目录，已在该 stage 和 acceptance package stage 显式 COPY 共用 module 后重新构建成功。
- 首次真实门禁完成六插件查询后，删除搜索断言超时。实际 Indexer 已处理删除；测试搜索词共享批次后缀，仍匹配其他帖子。只修正验收断言为目标 post ID 消失，并对创建/更新也核对目标 ID/标题；未改搜索业务语义。首次项目已强归属清理，原有资源保留。
- Router buffer 指标必须表示客户端实际缓存，bytes 包含 key/header 而不只是 envelope body。依据锁定 franz-go v1.21.0 的本地公开 `go doc`，改用 `BufferedProduceRecords() int64`/`BufferedProduceBytes() int64`，未读取第三方依赖实现源码；新增 adapter 直接断言并重跑 Router 受影响门禁。最终镜像和第三次完整真实门禁已包含此修正。
- 第二次真实门禁已通过六组件链路、浏览器、端点认证/基数、Redis 0→1、单端点故障和两个消费者替换；最后 Logs 回归误用了仅 Metrics API 接受的 `range` 参数。按现有 Logs/Events `from/to` 合同改为默认 15m 查询，未改业务 API；该次 owned 项目也已完整清理。
- 为满足 §7.3 的“metrics 存储故障不影响业务就绪/消费事实”，在同一聚焦脚本内增加一次 owned VictoriaMetrics 停止/恢复，仅检查 Backend ready、一个新帖的 Indexer 投影和 Worker 通知以及恢复后的进度写回；不扩展全依赖故障排列。


- 原计划未指定共用包的物理位置；为保证六生产者和双层白名单一致，采用独立标准库 module 而非复制目录，并同步相关 Docker stage。没有更换 exporter schema/业务存储/消费者职责。
- 新内部 listener 要与消费者及 log shipper 共用截止时间，实际修改共享 shutdown 路径，因此在固定聚焦命令内验证两个消费者的 SIGTERM 和替换。存储边界使用一次 VM 故障，未做全服务故障组合或全仓审计。
- 只读取了直接生产代码、测试和锁定依赖公开 API 文档，没有读取第三方依赖实现源码，没有新增默认 standalone review/severity gate。
- 首轮基础提交未完成计划的问题已在本次补齐，不将早期“局部已提交”解释为当时满足验收。

## 8. 实际文件范围

```text
.env.example
VERSION
backend/README.md
backend/cmd/business-worker/main.go
backend/cmd/search-indexer/main.go
backend/cmd/server/main.go
backend/go.mod
backend/internal/http/component_metrics_test.go
backend/internal/http/router.go
backend/internal/metricquery/components_test.go
backend/internal/metricquery/metricquery.go
backend/internal/notification/processor.go
backend/internal/observability/logship/runtime.go
backend/internal/outbox/metrics.go
backend/internal/platform/elasticsearch.go
backend/internal/platform/mysql.go
backend/internal/platform/rabbitmq.go
backend/internal/platform/rabbitmq_publisher.go
backend/internal/platform/redis.go
backend/internal/search/processor.go
backend/internal/worker/handler.go
backend/internal/worker/runtime.go
componentmetrics/backend.go
componentmetrics/backend_test.go
componentmetrics/catalog.go
componentmetrics/cmd/catalog/main.go
componentmetrics/config.go
componentmetrics/endpoint.go
componentmetrics/endpoint_test.go
componentmetrics/go.mod
componentmetrics/registry.go
componentmetrics/registry_test.go
componentmetrics/routes.go
componentmetrics/shutdown.go
componentmetrics/validation.go
deploy/compose.yaml
deploy/docker/acceptance.Dockerfile
deploy/docker/backend.Dockerfile
deploy/docker/observability.Dockerfile
dev/imple/Phase-14/Phase-14-05-自研组件基础运行指标闭环.md
dev/imple/Phase-14/Phase-14-总实施方案.md
dev/logs/Phase-14/Phase-14-05-自研组件基础运行指标闭环.md
docs/component-metrics.md
frontend/e2e/phase14-components.spec.ts
frontend/package-lock.json
frontend/package.json
frontend/src/services/componentMetrics.ts
frontend/src/services/observability.test.ts
frontend/src/services/observability.ts
frontend/src/types/observability.ts
frontend/src/views/ObservabilityMetricsView.vue
marshaller/README.md
marshaller/cmd/marshaller/main.go
marshaller/go.mod
marshaller/internal/consumer/kafka.go
marshaller/internal/consumer/processor.go
marshaller/internal/envelope/components_test.go
marshaller/internal/envelope/envelope.go
marshaller/internal/metrics/transform.go
monitor/README.md
monitor/cmd/monitor/main.go
monitor/go.mod
monitor/internal/events/monitor.go
monitor/internal/metrics/collector/collector.go
monitor/internal/metrics/collector/components.go
monitor/internal/metrics/collector/components_test.go
monitor/internal/metrics/envelope/envelope.go
monitor/internal/metrics/publisher/publisher.go
router/README.md
router/cmd/router/main.go
router/go.mod
router/internal/envelope/envelope.go
router/internal/httpserver/server.go
router/internal/kafka/producer.go
router/internal/kafka/producer_test.go
scripts/ci/testdata/component-probe.go
scripts/ci/verify_component_metrics.py
scripts/ci/verify_plugin_metrics.py
scripts/verify-component-metrics.sh
```

## 9. Phase-14-06 交接与限制

- 六个 source/producer/target 和 19101–19106 listener/token 合同固定，指标查询由 Backend catalog 生成，不提供任意 source 或 PromQL。
- 代表性查询沿用 §5 各组件的两个 family；额外固定断言为 Monitor `scraped_producer_kind=component,scraped_target_id=monitor-local`，Router `type=metrics,message_source=backend,result=produced`，Marshaller `type=metrics,message_source=backend,stage=store,result=stored`。
- WSL2/Linux + Bash + Compose 是本次实际平台。没有宣称 Windows/macOS 原生支持，也没有将 Kubernetes 作为条件。跨平台产品化、Phase 总验收仍由后续批次承担。
- 当前 scope 的必需能力没有已知阻断项。更多 runtime 指标、histogram、SLO、大屏、第三方插件、多实例或全故障矩阵不在本批范围，不顺手追加。
- 新 Go module 位于仓库内，源码开发需保留 sibling 目录布局；固定安全/目录自测及构建说明见 `docs/component-metrics.md`。


## 10. 提交收口

- 实现与版本提交：`9e8630f`，`feat(metrics): complete protected component metrics loop for 1.11.5`。
- 已在该提交后执行固定 `git diff --check upstream/main...HEAD`，通过；所有第 8 节固定门禁均有实际成功结果，没有剩余阻断项。
- 本次仅追加实际提交后检查记录，不再修改生产代码、配置、依赖或验收环境，也不重复已通过的业务/模块测试。
- 工作区只保留开工前即存在的未跟踪 `~`；没有推送。后续任务应按新批次生命周期从远程 main 起新版本分支，不自动复用已完成批次。

## 11. 推送后 CI 配置回归修复

- 远程运行 `34614679871` 的 Full-stack Compose acceptance 失败：Compose 插值时报 `SEARCH_INDEXER_METRICS_TOKEN is required`；自动创建 PR 步骤被跳过，其余八个门禁通过。之前的组件专项验收成功不能替代旧全栈入口的兼容性验证。
- 根因：本批将六个组件指标 token 纳入 Compose 必填合同，但遗漏更新两个 Bash 验收入口自行生成的临时环境文件。
- 修改 `scripts/verify-compose-observability.sh` 和 `scripts/verify-compose.sh`：为六个组件分别生成与本次验收 TOKEN 关联、彼此独立且不复用 API 凭据的指标 token；不放宽生产必填配置，不跳过远程全栈门禁。
- 新增 `scripts/ci/test_compose_acceptance_env.py`：从两个真实 heredoc 生成不继承开发者凭据的环境，验证 Compose 所要求的六个指标凭据均存在、长度足够且互不复用。由现有 CI unittest discovery 自动执行。
- 实际本地验证通过：`python3 -m unittest discover -s scripts/ci -p test_compose_acceptance_env.py`；`bash -n scripts/verify-compose.sh scripts/verify-compose-observability.sh`；`bash scripts/verify-compose.sh --self-test`；分别用两个真实 heredoc 在隔离环境下执行 `docker compose --env-file <临时环境文件> -f deploy/compose.yaml config --quiet`；`git diff --check`。
- 本次为同一批次 CI 回归修复，沿用 `develop/1.11.5` 和 `VERSION=1.11.5`。完整远程全栈门禁仍需修复推送后的实际运行结果确认，本段不将本地配置通过表述为远程全栈通过。
