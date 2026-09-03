# Phase 3：Elasticsearch 与业务搜索总实施方案

## 1. 实施目标

在 Phase 2 已完成的业务系统上引入 Elasticsearch，使认证用户能够按帖子标题或正文关键词搜索，并保证 Elasticsearch 始终只是可删除、可重建的搜索投影，MySQL 继续保存帖子及其展示数据的最终事实。

本阶段按“先交付可重建的搜索读闭环，再接入可靠增量索引，最后统一集成验收与收口”的顺序执行：

```text
历史帖子 / 手工恢复：
MySQL posts → search-reindex → 新物理索引 → 原子切换搜索别名

新帖子：
POST /posts → MySQL 事务（posts + post.created Outbox）
            → Outbox Dispatcher → 独立 Search Exchange / Queue
            → Search Indexer → 回读 MySQL → Elasticsearch 搜索别名

查询：
Frontend → Backend Search API → Elasticsearch 返回有序 post_id
                              → MySQL 装配当前帖子事实 → Frontend
```

阶段完成必须同时证明历史数据重建、新帖子增量索引、标题/正文查询、Backend 代理、Frontend 展示和索引删除后恢复可用。只建立 Elasticsearch 连接、只写入孤立文档或只提供命令行查询均不构成完成。

## 2. 当前真实基线

Phase 3 以已合入主远程 `main` 的提交 `6c86d6418613d48ef50a24c3d04adc63182d6604` 或其后继提交为实施基线：

- 根 `VERSION` 与 Frontend npm 元数据均为 `0.3.6`；Phase 2 已经完成。
- Gin Backend 已提供认证、帖子、评论、点赞和通知 API；除健康接口外，现有业务路由均位于认证中间件后。
- `posts` 保存作者、标题、正文和创建/更新时间；当前产品没有帖子编辑或删除能力。帖子创建仍是单条 MySQL `INSERT`，没有创建 `post.created` Outbox。
- 帖子公共 DTO 由 MySQL 装配作者、评论数和点赞数，并按当前用户计算 `liked_by_me`；这些动态事实不应复制到搜索索引。
- 当前分页响应统一为 `{data, meta.next_cursor}`，帖子列表使用严格 opaque cursor，默认 `limit=20`、最大 `50`。
- `business_outbox`、Dispatcher、RabbitMQ Publisher 和 Business Worker 已实现至少一次通知链路；事件只允许 `comment.created` 与 `post.liked`，Publisher 固定发布到 `gopulse.business.v1`。
- Outbox 已具备超期 `published` 行有界清理，配置同时约束整批 publish 预算小于租约；Worker 已在连接中断和有界退出时取消并等待在途 handler。Phase 3 必须保持这些 `0.3.6` 契约，不重复把它们规划为待修复能力。
- Business Worker 的 runtime、delivery decoder、retry/dead handler 和拓扑名称仍面向通知固定实现；不能直接让该进程承担搜索索引职责。
- Compose 只有 MySQL、Redis、RabbitMQ，Backend `/ready` 也只检查这三项依赖；仓库没有 Elasticsearch 客户端、搜索模块或索引命令。
- Bash 生命周期管理 Backend、Business Worker 和 Frontend；隔离业务验收已具备随机 Compose project、端口、数据库、volume 和进程身份保护。
- Frontend 尚无搜索路由、入口或页面；冻结的 `scripts/*.ps1` 保持 `0.2.1` 能力基线，不属于本阶段修改范围。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 2 已合入配置的主远程 `main`，根版本为 `0.3.6`，现有业务、通知和可靠性门禁通过。
- 每批开始前 fetch 主远程并确认前置批次已合入最新 `main`，再从该提交创建本方案分配的独立分支。
- 实施、应用测试和集成验收在 Windows 宿主机的 WSL2 Linux filesystem 中执行；Bash 是唯一维护的生命周期与验收入口。
- 开始前记录 Git 状态与日常运行栈状态，不覆盖、暂存或提交用户及其他任务的改动。
- 每批只实现对应方案的验收合同；Phase-03-02 复用异步基础时必须保持 Phase 2 通知、Outbox 清理、租约预算和 Worker 退出契约。

### 3.2 权威批次、版本与开发分支

Phase 3 使用 `0.4.x` 开发版本线，`0.4.0` 只作为阶段基线，不创建空批次。下表是本阶段批次、顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-03-01 | `0.4.1` | `develop/0.4.1` | 已合入 `main`（PR #49） |
| Phase-03-02 | `0.4.2` | `develop/0.4.2` | 已合入 `main`（PR #49） |
| Phase-03-03 | `0.4.3` | `develop/0.4.3` | 已合入 `main`（PR #50） |
| Phase-03-04 | `0.4.4` | `develop/0.4.4` | 已合入 `main`（PR #51）；push 门禁成功，重复 PR CI 已由 `update` 上的单一 push 门禁流程取代 |

截至 Phase-03-04 整改开始时，PR #49 已把 Phase-03-01 与 Phase-03-02 的最终 head `a2fb578` 合入 `main` 为 `3fa8230`。PR #50 又于 2026-09-02 16:59:12 UTC 把 Phase-03-03 head `e59b7d4d65ed431d6b44ecea121be68f5ba14f70` 合入 `main`，合并提交为 `f54f1a2175c1f508c3ecac775077387e5af29682`；Backend、Frontend、Branch governance、Scripts and Compose、Integration 与自动 PR/合并检查均成功。实现 Review 随后识别出 PIT 分页、阶段状态、批次分配和 Frontend 分页重试问题，因此增加 Phase-03-04 作为唯一的 `0.4.4` 整改批次；该批次已由 PR #51 合入，产品与治理 push 门禁成功。后续 PR #53 证明，使用默认 `GITHUB_TOKEN` 创建 PR 时，其 `pull_request` 工作流会进入 `action_required` 并等待人工批准；因此仓库改为单一权威 push 门禁，不再递归等待重复 PR CI。`develop/*` 保留完整产品门禁，`update` 只运行治理校验。

执行规则：

- 每批全部提交共享该批目标版本；批次完成时同步根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json`。
- 每批完成前创建同名 `dev/logs/Phase-03/Phase-03-XX-*.md`，只记录实际工作与实际验证。
- 完成或已打开 Pull Request 后不在原分支执行下一批；批次变化时先更新本表，已推送分支不得静默改名或重新编号。
- Phase-03-01 与 Phase-03-02 是两个可独立运行、可独立验证的纵向实现批次；Phase-03-03 固定用于跨批集成验收与阶段收口；Phase-03-04 只关闭实现 Review 报告中的 P2-01～P2-03 与 P3-01，不扩展搜索产品范围。
- `1.0.0` 是 Phase-03-01 至 Phase-03-04 完成并通过里程碑验收后的 Milestone 1 发布版本，不替代 `0.4.1`～`0.4.4` 的批次版本。

### 3.3 `1.0.0` 里程碑发布动作

Phase-03-04 合入主远程 `main`、远程门禁通过且第 14 节全部满足后，执行一次不计入 Phase 批次数量的 release-only 动作：

| 发布动作 | 目标版本 | 发布分支 | 前置状态 |
| --- | --- | --- | --- |
| Milestone-01-Release | `1.0.0` | `develop/1.0.0` | release-only 提交同步版本与发布说明；远程门禁通过并合入 `main` 后正式发布 |

- 发布分支从已验证的主远程 `main` 创建，只允许更新根/Frontend 版本元数据、里程碑状态、发布说明和直接需要的分支治理规则，不夹带产品功能或额外整改。
- Phase-03-04 保持分支校验器对该 release-only 分配的唯一识别；不得把发布伪装成新的 Phase 批次，也不得在 `update` 上修改 `VERSION`。
- `develop/1.0.0` 合入且远程门禁通过后，主远程 `main` 的根 `VERSION=1.0.0` 才表示业务系统 MVP 正式发布；tag 或 release 只按届时实际流程记录。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 固定版本的单节点 Elasticsearch、命名卷、健康检查、配置校验和 Backend readiness。
- 帖子搜索物理索引、稳定读写别名、strict Mapping、版本化文档结构和安全命名边界。
- 从 MySQL 有界扫描、Bulk 写入、逐项校验、原子别名切换和精确清理组成的全量重建命令。
- 认证 Search API：标题/正文关键词、相关度排序、严格 limit/cursor、稳定错误和 MySQL hydration。
- Frontend 搜索导航与页面，覆盖提交、加载、空结果、分页、错误、重试、重置和详情跳转。
- `post.created` 事件、帖子与 Outbox 原子事务、独立 Search RabbitMQ 拓扑和 Search Indexer。
- Elasticsearch、RabbitMQ、Search Indexer 暂停/恢复及索引删除/重建的隔离验收。
- Phase 0～3 必要业务回归、跨批集成验收、实施记录和 Milestone 1 发布准备。

### 4.2 明确不做

- 不把 Elasticsearch 文档、命中总数或相关度作为帖子存在性和展示字段的事实来源。
- 不实现帖子编辑/删除同步；产品当前没有对应业务能力，后续增加时另行定义索引事件。
- 不实现推荐、自动补全、建议、纠错、同义词、词典管理、标签、聚合、复杂过滤、高亮或个性化排序。
- 不实现搜索历史、热门词、搜索分析、索引管理 HTTP API 或浏览器直连 Elasticsearch。
- 不承诺 exactly-once；增量链路是至少一次消息加固定文档 ID 的幂等覆盖。
- 不把搜索消息绑定通知队列，不让 Business Worker 写 Elasticsearch，不使用 Kafka 同步业务搜索。
- 不建设 Elasticsearch 集群、ILM、快照、生产安全加固或容量压测，不提前实现 Phase 4 日志规范与 Phase 9 日志/事件索引。
- 不修改 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 数据归属与一致性

### 5.1 搜索文档与查询装配

Elasticsearch 文档只保存检索、幂等写入和稳定排序所需字段：

```json
{
  "post_id": 123,
  "title": "Kubernetes 实践",
  "content": "...",
  "created_at": "RFC3339Nano UTC",
  "updated_at": "RFC3339Nano UTC"
}
```

- Elasticsearch `_id` 使用十进制 `post_id`；同一事件或帖子重复索引收敛为同一逻辑文档。
- 作者、评论数、点赞数和 `liked_by_me` 不写入索引，避免评论/点赞引发搜索同步和展示字段陈旧。
- Search API 只从 Elasticsearch 取得有序 post ID 和分页 sort tuple，再以一次有界 MySQL 查询装配当前 `post.Post` DTO，并在 Go 中恢复命中顺序。
- MySQL 中不存在的命中视为陈旧投影并忽略，不得根据 Elasticsearch `_source` 伪造公共帖子；重建负责最终清除投影漂移。

### 5.2 新帖子写入语义

Phase-03-02 后，帖子创建在一个 MySQL 事务中完成：

```text
BEGIN
  INSERT posts
  INSERT business_outbox(post.created.v1)
COMMIT
```

- posts 或 Outbox 任一写入失败时整体回滚。
- MySQL 提交成功后，即使 RabbitMQ、Search Indexer 或 Elasticsearch 不可用，发帖仍成功；依赖恢复后自动补索引。
- 请求处理线程不直接访问 Elasticsearch，也不以 AMQP publish 或 refresh 成功作为帖子创建成功条件。
- 发布/确认边界允许重复，固定 `_id=post_id` 吸收重复；搜索可见性是有界等待下的最终一致。

### 5.3 重建与增量协作

- 重建只以 MySQL `posts` 为源，不依赖 Outbox 历史、RabbitMQ 队列或 dead queue。
- 重建原子切换别名前创建的帖子由高水位补偿覆盖；切换后创建的帖子由 Search Indexer 写入已经切换的新别名。
- Phase-03-01 尚无增量 Indexer，只承诺重建捕获的历史快照；命令完成后新建帖子需再次重建。该限制必须记录到 03-01 实施记录，直到 03-02 完成后才解除。

## 6. Elasticsearch 索引与部署契约

### 6.1 服务基线

- Compose 固定 `docker.elastic.co/elasticsearch/elasticsearch:9.5.2`；Go 使用官方 `github.com/elastic/go-elasticsearch/v9 v9.5.0`，禁止 `latest` 或浮动 major。
- 本地开发为单节点、`1` shard、`0` replica、512 MiB JVM heap，使用命名卷 `elasticsearch_data`。
- Elasticsearch HTTP 只绑定 `127.0.0.1:${ELASTICSEARCH_PORT}:9200`，不复用允许显式放宽的 `PUBLISHED_HOST`；本地单节点可关闭安全插件，但不得外部暴露。
- healthcheck 等待 cluster 至少达到 `yellow`；Bash 端口预检、启动等待、失败清理与只读验证覆盖该服务。
- `dev.sh` 在迁移和 Elasticsearch 健康后执行 `search-reindex --if-missing`：别名缺失时从 MySQL 初始化，别名已存在时不重复全量重建。

### 6.2 索引名称与 Mapping

```text
查询/写入别名：gopulse-post-search-v1
物理索引前缀：gopulse-post-search-v1-
物理索引示例：gopulse-post-search-v1-20260902T120000Z-a1b2c3d4
```

- Mapping 使用 `dynamic: strict`；`post_id` 为 `long`，时间字段为 `date_nanos`，`title`/`content` 为使用内置 `standard` analyzer 的 `text`。
- 第一版不安装分词插件；中文专项分词质量属于明确后续增强，不阻塞关键词闭环。
- 搜索、重建和 Search Indexer 共用一处版本化 Mapping、别名和前缀常量，不复制多套 JSON。
- Search Indexer 通过别名写入并要求 `require_alias=true`，别名缺失时不得自动创建无 Mapping 索引。
- Phase 9 日志/事件索引不得复用本别名、物理前缀或 Mapping。

## 7. 全量重建策略

`backend/cmd/search-reindex` 是唯一受支持的重建入口，支持强制重建和 `--if-missing`：

1. 通过专用 MySQL 连接取得固定 advisory lock，阻止两个重建命令并发；不阻塞正常发帖事务。
2. 创建带时间与随机后缀的新物理索引并应用共享 Mapping；删除只接受程序创建或枚举并校验过的精确前缀名称，禁止 wildcard。
3. 记录 `MAX(posts.id)=H1`，按主键升序和有界 batch 扫描 `id <= H1`，Bulk 写入并检查 HTTP 状态与每个 item 的结果。
4. refresh，并以 `post_id <= H1` 的 MySQL/Elasticsearch 计数一致作为切换门槛；切换前失败时旧别名保持不变，并只清理本次新建且未发布的索引。
5. 通过单次 aliases API 原子移除旧目标、添加新目标；别名必须且只解析到一个物理索引。
6. 切换后记录 `MAX(posts.id)=H2`，补写 `H1 < id <= H2` 并验证；Phase-03-02 完成后，`id > H2` 由写向新别名的增量事件覆盖。
7. 成功后仅删除经精确校验的旧物理索引；切换后补偿或旧索引清理失败时非零退出并保留足够诊断状态，不做不安全自动回滚。
8. 成功、失败和中断均释放 advisory lock，不删除当前别名目标或用户其他 Elasticsearch 索引。

所有网络调用、响应体读取、Bulk batch 和错误摘要均有上限；不得把完整正文、连接 URL 或原始 Elasticsearch 响应写入日志。

## 8. Search API 与 Frontend

### 8.1 Backend API

```text
GET /api/v1/search/posts?q=<keyword>&limit=<1..50>&cursor=<opaque>
```

- 路由沿用现有认证中间件。`q` trim 后为 1～200 Unicode code point；重复、空、超长参数与非法 limit/cursor 返回 `400 validation_failed`。
- 默认 `limit=20`、最大 `50`；成功沿用 `{data, meta.next_cursor}`，无命中返回空数组与空 cursor。
- 使用 `multi_match` 的 `best_fields` 搜索 `title^2` 和 `content`，按 `_score DESC, created_at DESC, post_id DESC` 排序并通过 `search_after` 分页。
- cursor 保存版本、规范化查询的 SHA-256、物理索引 generation 和 sort tuple，并使用严格 canonical Base64URL；跨查询、损坏或重建后失效均返回 `validation_failed`。
- Elasticsearch 超时、不可达、别名缺失、响应无效或拒绝查询统一返回 `503 search_unavailable`；公共响应不泄漏 URL、DSL、索引名、score、节点错误或底层 MySQL/Elasticsearch 错误。
- 请求超时、最大响应体和反序列化边界必须有固定上限；Backend 不接受客户端提供 index、DSL、字段或 sort。

### 8.2 Frontend

- 新增受保护 `/search` 路由和主导航入口；查询词进入 URL query，使刷新、前进和后退可以恢复搜索意图。
- 支持显式提交、清空、加载更多和失败重试；新查询清空旧结果与 cursor，重复请求期间禁用冲突动作。
- 复用 `PostCard` 展示 MySQL 装配结果，区分初始提示、首次加载、空结果、参数错误、服务不可用、cursor 失效和分页失败。
- 点击结果进入现有 `/posts/:postId`；Frontend 只调用 Backend 相对 API，不读取 `ELASTICSEARCH_URL` 或访问 9200。

## 9. 增量事件、拓扑与 Indexer

### 9.1 `post.created` 事件契约

- 扩展严格 Envelope v1，新增事件类型 `post.created` 和 routing key `post.created.v1`。
- `post.created` 只包含 schema/event/type/time/actor/post；`recipient_id` 与 `comment_id` 必须省略，标题、正文、用户名和连接信息不得进入 Payload。
- 现有 `comment.created` 与 `post.liked` JSON 形状、routing key、通知行为保持兼容；按事件类型执行严格必填/禁填校验。
- 新增 MySQL migration 扩展 `business_outbox.event_type` CHECK；down migration 只移除搜索事件并恢复旧约束，不删除帖子事实。

### 9.2 独立搜索拓扑

```text
direct exchange: gopulse.search.v1
routing key:     post.created.v1
main queue:      gopulse.search-indexer.v1
retry exchange:  gopulse.search.retry.v1
retry queue:     gopulse.search-indexer.retry.v1
dead exchange:   gopulse.search.dead.v1
dead queue:      gopulse.search-indexer.dead.v1
```

- Search 与通知拓扑完全隔离；Publisher 按事件类型选择固定 exchange，未知类型不得发布。
- 主、retry、dead exchange/queue 都是 durable；retry queue 使用固定 TTL 后 dead-letter 回主 exchange。
- retry/dead 二次发布必须 persistent、mandatory 且 confirm 成功后才 ack 原消息；失败时 requeue 原消息。
- 拓扑名称与允许 routing key 集中定义，由 Backend Publisher、Search Indexer 和测试共享，不通过管理 UI 手工创建，也不开放为任意环境变量。

### 9.3 Search Indexer

- 新增独立 `backend/cmd/search-indexer`，只加载 MySQL、RabbitMQ、Elasticsearch 和 `SEARCH_INDEXER_*` 配置；不依赖 Gin、Redis、JWT、Cookie 或 Frontend。
- 以固定 Worker profile 最小参数化现有断线重连、手动 ack、confirm、retry/dead 和有界退出骨架；Business Worker 保持通知专用 profile 与 self-event 防御语义。
- 严格校验 AMQP metadata、Envelope、事件类型与 routing key，只接受 `post.created`；按 post ID 回读 MySQL 当前文档并 PUT 稳定别名。
- 成功后 manual ack；临时 MySQL 错误、网络错误、429 和 5xx 有限 retry；非法消息、不存在的 MySQL 事实和确定的 Mapping 4xx 进入 search dead queue。
- 相同 event 或 post ID 重投写同一 `_id`；日志只含 event ID、post ID、attempt 和有限 reason code。
- alias 缺失按临时不可用处理，有限重试耗尽后进入 dead；最终恢复手段始终是从 MySQL 执行 `search-reindex`，而不是自动重放 dead queue。

### 9.4 现有异步契约保护

- 继续使用 Phase 2 已完成的 published Outbox 有界清理；pending/leased 搜索事件不得被清理。
- 保持 `claim_batch × publish_timeout + 1s <= lease_duration` 的启动校验，新增事件不能绕过租约预算。
- Worker runtime 参数化后仍须在 shutdown/连接中断时取消并 join 在途 handler，不能重新引入返回后 goroutine。
- 通知 queue 只绑定评论/点赞，Business Worker 不处理搜索；Search Indexer 不创建通知。

## 10. 进程、配置与故障边界

- `/ready` 增加 `elasticsearch`；Elasticsearch down 时全局 readiness 为 `503`，Search API 为 `503 search_unavailable`，但已启动 Backend 的 MySQL-backed 业务 API 仍按自身依赖边界运行。
- `search-reindex` 使用独立的 MySQL/Elasticsearch 配置加载入口，不被 HTTP、Redis、RabbitMQ 或认证配置绑架。
- 基础配置固定为 `ELASTICSEARCH_PORT=9200`、`ELASTICSEARCH_URL=http://127.0.0.1:9200`、`ELASTICSEARCH_REQUEST_TIMEOUT=3s`、`SEARCH_REINDEX_BATCH=500`；端口、URL、timeout 和 batch 有上下限，URL 禁止 userinfo。
- Indexer 配置固定为 `SEARCH_INDEXER_PREFETCH=10`、`SEARCH_INDEXER_MAX_RETRIES=3`、`SEARCH_INDEXER_RETRY_DELAY=30s`、`SEARCH_INDEXER_PUBLISH_TIMEOUT=5s`、`SEARCH_INDEXER_SHUTDOWN_TIMEOUT=10s`、`SEARCH_INDEXER_RECONNECT_MIN=500ms`、`SEARCH_INDEXER_RECONNECT_MAX=30s`。Backend Publisher 与 Indexer 读取相同 retry delay，避免 durable retry queue 参数漂移。
- `dev.sh` 管理 Backend、Business Worker、Search Indexer、Frontend；两个 Worker 使用不同二进制、进程组和 `.run` 身份记录。
- `down.sh` 继续以 cwd、executable、start ticks 和 marker 验证进程归属；日常停止保留 MySQL、Redis、RabbitMQ 和 Elasticsearch volume。
- `verify.sh` 保持只读；`verify-business.sh` 的破坏性动作只允许作用于随机且多重验证归属的验收资源。
- 日志不得输出完整连接 URL、凭据、JWT、Cookie、标题/正文、事件 Payload、查询 DSL 或原始 Elasticsearch 响应体。

## 11. 跨批次依赖与摘要

```text
Phase-03-01 可重建帖子搜索闭环（0.4.1）
  ↓
Phase-03-02 可靠增量索引与运行闭环（0.4.2）
  ↓
Phase-03-03 集成验收与里程碑收口（0.4.3）
  ↓
Milestone-01-Release（非 Phase 批次）：1.0.0
```

- [Phase-03-01：可重建帖子搜索闭环](Phase-03-01-可重建帖子搜索闭环.md)：交付 Elasticsearch、重建、Search API 和 Frontend 的完整历史搜索闭环；新帖子自动索引明确留给下一批。
- [Phase-03-02：可靠增量索引与运行闭环](Phase-03-02-可靠增量索引与运行闭环.md)：交付 posts + Outbox 原子写入、独立搜索拓扑与 Indexer，使新帖子在依赖故障恢复后最终可搜索。
- [Phase-03-03：集成验收与里程碑收口](Phase-03-03-集成验收与里程碑收口.md)：不新增范围，执行跨批矩阵、最小修复验收失败并准备 `1.0.0` 发布。

两个纵向实现批次加一个固定集成验收/收口批次符合阶段提纲的 2～3 批约束；每个实现批次都有用户可观察的完整闭环，不按 Elasticsearch、Backend、Frontend 或测试层机械拆分。

## 12. 测试策略与固定矩阵

### 12.1 执行效率与停止规则

- 每批先从详细方案提取“新增行为 → 最低测试层 → 固定门禁”，只读直接受影响代码并在 10 分钟内进入实现。
- 没有具体编译、运行或必需测试失败时不读取第三方依赖源码；新测试只证明验收、复现缺陷或保护实际改变的契约。
- 最终 diff 上固定门禁各执行一次；上下文压缩不触发重跑。可选调查连续 15 分钟无进展立即停止并记录为非阻塞跟进。
- 固定门禁通过且无阻断验收的失败后立即更新实施记录、版本并提交，不追加机会性重构、压测、分词排列或搜索增强。

### 12.2 批次验证边界

| 批次 | 本批直接证据 | 固定必要回归 | 明确留后/不重复 |
| --- | --- | --- | --- |
| Phase-03-01 | Mapping/Alias/Reindex、真实 MySQL/Elasticsearch、Search API、搜索页面/E2E | 配置/readiness、帖子 hydration、Frontend 固定门禁、Compose/版本 | 不实现 RabbitMQ 增量；不跑 Phase 2 完整故障矩阵 |
| Phase-03-02 | 原子 `post.created`、独立 topology/Indexer、真实暂停/重复/恢复 | Outbox/Worker 改动边界、帖子创建、通知代表性路径、03-01 搜索读链路、Bash 身份安全 | 不重跑所有通知/认证组合；完整里程碑矩阵留 03-03 |
| Phase-03-03 | 阶段矩阵一次、真实浏览器闭环 | Backend/Frontend/脚本固定门禁、远程 CI、版本/文档 | 不新增功能，不重复未受修复影响的定向测试 |

### 12.3 阶段级端到端验收矩阵

`scripts/verify-business.sh` 在随机 token、独立 Compose project/数据库/端口/进程目录/volume 和归属校验下固定覆盖：

1. 从空环境完成迁移，MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer 和 Frontend 启动；health/readiness 正确。
2. 历史帖子分别可被标题和正文命中，无关词无命中；重建后 Search API 与真实浏览器得到正确且可跳转的结果。
3. 排序来自 Elasticsearch，展示来自 MySQL；评论/点赞后结果显示最新计数和 `liked_by_me`，无需重写搜索文档。
4. 正常发帖后 posts 与 `post.created` Outbox 原子存在，最终无需手工重建即可搜索。
5. 停止 Search Indexer 后继续发帖并恢复；消息保留，相同 event/post 重投只产生一个逻辑文档。
6. 停止 Elasticsearch 后发帖成功，Search API 返回 `search_unavailable`、readiness 降级；恢复后自动补齐或通过明确重建恢复。
7. 停止 RabbitMQ 后发帖成功且 Outbox 保留；恢复后自动投递和索引，无需客户端重发。
8. 精确删除验收搜索索引后从 MySQL 重建；历史、故障期间以及代表性并发发帖均最终恢复。
9. 非法 q/limit/cursor、跨 query/generation cursor、响应上限和错误脱敏符合契约，Frontend 不直连 Elasticsearch。
10. Phase 0～2 注册、登录、帖子、评论、点赞、通知及必要 Redis/RabbitMQ 降级与 Phase 3 搜索共同运行；成功、失败或中断清理不影响日常资源。

以上是封闭矩阵；除非真实失败表明共享基础设施或直接受影响能力需要必要回归，不追加 analyzer 黄金集、压力测试、节点故障排列或与本阶段无关的回归。

## 13. 实施记录规则

每批完成后创建同名镜像记录：

```text
dev/imple/Phase-03/Phase-03-XX-<名称>.md
dev/logs/Phase-03/Phase-03-XX-<名称>.md
```

记录必须包含实际完成工作、变更文件、验证命令与结果、相对方案偏差、已知限制和跟进项；本次规划不提前创建空实施记录，也不把计划命令写成已通过。

## 14. Phase 3 与 Milestone 1 验收标准

- 历史及新帖子均可按标题或正文关键词搜索，并经 Backend/Frontend 形成真实用户闭环。
- 发帖事实与索引事件原子提交；RabbitMQ、Search Indexer 或 Elasticsearch 故障不推翻已提交帖子，恢复后索引最终收敛。
- 搜索与通知的消息、队列、Worker、retry/dead 完全隔离；至少一次与固定 `_id` 幂等语义如实记录。
- 搜索展示事实来自 MySQL；Elasticsearch 只负责命中、排序和分页边界。
- 精确删除搜索索引后可由 MySQL 安全重建；切换前验证、原子 alias、并发高水位补偿和禁止 wildcard 均有真实证据。
- Frontend 不直连 Elasticsearch；Search API 的认证、query/cursor、readiness、超时、降级和错误脱敏契约稳定。
- Phase 2 的 Outbox 清理、整批租约预算、Worker 有界退出、通知最终一致与 Redis 降级均无回归。
- 第 12.3 节固定矩阵和远程门禁通过，没有使阶段验收不成立的失败；非阻断改进项未扩大实现范围。
- 四份实施记录与实际提交、命令和限制一致；Phase-03-04 完成后根与 Frontend 版本均为 `0.4.4`。
- `_score` 分页使用同一 Elasticsearch PIT 快照；游标签名并绑定 query、generation、PIT、过期时间和完整 sort tuple，过期或篡改游标安全要求重新搜索。
- Frontend 临时分页失败重试原 cursor 并保留累计结果；快照游标失效时明确清空并受控重启第一页。
- Phase 0～3 能共同运行；随后 release-only 动作使主远程唯一产品版本成为 `1.0.0`。

## 15. 完成、停止与 Phase 4 交接

只有 Phase-03-01 至 Phase-03-04 均从权威分支完成并合入主远程、第 12.3 节封闭矩阵与 Phase-03-04 定向整改门禁在 WSL2/Bash 真实通过、远程门禁成功且实施记录齐全，Phase 3 才可标记完成。随后执行 `1.0.0` release-only 动作并停止功能扩展；未执行的检查、PR、合并、tag 或发布不得写成通过。

向 Phase 4 交付：

- 已发布的业务系统 MVP，以及可产生真实搜索、业务事件和通知流量的运行闭环。
- 独立、可重建的帖子搜索索引契约；未来日志索引使用不同前缀、Mapping 和查询入口。
- Backend、Business Worker、Search Indexer 与重建命令的日志点、reason code 和敏感信息边界。
- 故障恢复和资源归属验收证据；Phase 4 不重新定义搜索事实来源或增量消息语义。
