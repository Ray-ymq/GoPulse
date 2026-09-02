# Phase 3：Elasticsearch 与业务搜索总实施方案

## 1. 实施目标

在 Phase 2 已完成的业务系统上引入 Elasticsearch 9.5.2，使认证用户能够按帖子标题或正文关键词搜索，并保证 Elasticsearch 只是可删除、可重建的搜索投影，MySQL 始终保存帖子及其展示数据的最终事实。

本阶段采用“先交付可重建搜索读闭环，再接入可靠增量索引”的顺序：

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

阶段完成时必须同时证明历史数据重建、新帖子增量索引、标题/正文查询、Backend 代理、Frontend 展示和索引删除后恢复可用；只建立 Elasticsearch 连接、只写入孤立文档或只提供命令行查询都不构成完成。

## 2. 当前真实基线

Phase 3 方案以已合入主远程 `main` 的提交 `a7f4a6460ed4d21bb48f1091ae7bfc553c3290fe` 为设计基线：

- 根 `VERSION` 与 Frontend npm 元数据均为 `0.3.5`。
- Gin Backend 已提供认证、帖子、评论、点赞和通知 API；除健康接口外，现有业务路由均位于认证中间件后。
- `posts` 保存作者、标题、正文和时间；帖子没有编辑或删除能力。帖子创建当前是单条 `INSERT`，没有事务 Outbox。
- 帖子公共 DTO 由 MySQL 装配作者、评论数和点赞数，并按当前用户计算 `liked_by_me`；这些动态事实不应复制到搜索索引。
- 当前分页响应统一为 `{data, meta.next_cursor}`，帖子列表使用严格 opaque cursor，默认 `limit=20`、最大 `50`。
- `business_outbox`、Dispatcher、RabbitMQ Publisher 和 Business Worker 已实现至少一次通知链路，但事件只允许 `comment.created` 与 `post.liked`。
- Publisher 固定发布到 `gopulse.business.v1`；Worker runtime 固定声明并消费通知队列，不能直接承担搜索职责。
- Phase 2 Review 已记录三个与复用直接相关的 P2 风险：published Outbox 没有生产清理循环、批量串行发布的租约预算不足、Worker shutdown 后可能遗留处理 goroutine；另有 Phase 2 权威状态文字滞后的规划治理 finding。
- Compose 只有 MySQL、Redis、RabbitMQ；Backend `/ready` 也只检查这三项依赖。
- Bash 生命周期管理 Backend、Business Worker 和 Frontend；隔离验收已有端口、Compose project、volume 和进程归属保护。
- Frontend 尚无搜索入口，Backend 尚无 Elasticsearch 客户端；PowerShell 冻结在 `0.2.1`，不属于本阶段范围。

## 3. 前置条件、版本与分支

### 3.1 实施前置条件

- Phase 2 已合入主远程 `main`，根版本为 `0.3.5`，已完成能力可共同运行。
- 每批开始前 fetch 主远程并确认前置批次已合入最新 `main`，再创建本方案分配的独立分支。
- 实施、应用测试和集成验收在 WSL2 Linux filesystem 中执行；Bash 是唯一维护的本地入口。
- 开始前记录 Git 状态，不覆盖、暂存或提交用户及其他任务的改动。
- Phase 2 权威状态文字滞后应在 Phase-03-01 开始前于规划分支关闭，不改变产品版本，也不混入 Phase 3 开发批次。
- Phase-03-02 扩展 Outbox 与复用 Worker runtime 前，必须先关闭第 9.1 节三个直接复用风险。

### 3.2 权威批次、版本与开发分支

Phase 3 使用 `0.4.x` 开发版本线，`0.4.0` 只作为阶段基线。下表是本阶段唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-03-01 | `0.4.1` | `develop/0.4.1` | 未开始 |
| Phase-03-02 | `0.4.2` | `develop/0.4.2` | 未开始 |
| Phase-03-03 | `0.4.3` | `develop/0.4.3` | 未开始 |
| Phase-03-04 | `0.4.4` | `develop/0.4.4` | 预留（用户主动发起 Review） |

- 每批全部提交共享目标版本；完成时同步根 `VERSION` 与 Frontend npm 根包版本。
- 每批完成前创建同名 `dev/logs/Phase-03/Phase-03-XX-*.md`，只记录实际工作与实际验证。
- 完成或已打开 PR 后不在原分支执行下一批；批次变化时先更新本表，已推送分支不得静默改名。
- Phase-03-01 与 Phase-03-02 交付功能，Phase-03-03 完成 MVP 集成验收与候选收口；Phase-03-04 只预留给 MVP 之后由用户主动发起的独立 Review。
- 本阶段例外保留四批的原因是将“MVP 实现与集成验收”和“用户主动 Review”的权限、时点与边界隔离，不是按技术层次机械拆批。
- Phase-03-04 在用户明确发起前不创建拆分实施方案，不预设 Review 检查清单、finding 等级或整改边界；届时以用户指令与真实 Review 结果为执行依据。
- `1.0.0` 是四个 Phase 3 批次完成后的 Milestone 1 发布版本，不替代 `0.4.1`～`0.4.4` 的批次版本。

### 3.3 `1.0.0` 里程碑发布动作

Phase-03-04 由用户主动发起并完成、合入主远程 `main`、远程门禁通过且第 14 节满足后，执行一次不计入 Phase 批次数量的 release-only 动作：

| 发布动作 | 目标版本 | 发布分支 | 前置状态 |
| --- | --- | --- | --- |
| Milestone-01-Release | `1.0.0` | `develop/1.0.0` | Phase-03-04 已合入且用户确认 Review 完成 |

- 发布分支从 Review 后已验证的主远程 `main` 创建，只允许更新根/Frontend 版本元数据、里程碑状态、发布说明和直接需要的分支治理规则，不夹带产品功能或 Review 整改。
- Phase-03-04 是真实的预留 Review 批次，不是 release-only 动作；`develop/1.0.0` 仍按本节单独分配，也不得在 `update` 上修改 `VERSION`。
- `develop/1.0.0` 合入且远程门禁通过后，主远程 `main` 的根 `VERSION=1.0.0` 才表示业务系统 MVP 正式发布；tag/release 只按届时真实流程记录。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 固定版本的单节点 Elasticsearch、命名卷、健康检查、配置校验和 Backend readiness。
- 帖子搜索物理索引、稳定别名、strict Mapping、版本化文档结构和安全命名边界。
- 从 MySQL 有界扫描、Bulk 写入、校验、原子别名切换和失败清理组成的全量重建命令。
- 认证 Search API：标题/正文关键词、相关度排序、严格 limit/cursor、稳定错误和 MySQL hydration。
- Frontend 搜索导航与页面，覆盖提交、加载、空结果、分页、错误、重试、重置和详情跳转。
- `post.created` 事件、帖子与 Outbox 原子事务、独立 Search RabbitMQ 拓扑和 Search Indexer。
- Elasticsearch、RabbitMQ、Indexer 暂停/恢复及索引删除/重建的隔离验收。
- 与扩展异步基础直接相关的三个 Phase 2 Review 风险整改。

### 4.2 明确不做

- 不把 Elasticsearch 文档、命中总数或相关度作为帖子存在性和展示字段的事实来源。
- 不实现帖子编辑/删除同步；产品当前没有对应业务能力，后续增加时另行定义索引事件。
- 不实现推荐、自动补全、建议、纠错、同义词、词典管理、标签、聚合、复杂过滤、高亮或个性化排序。
- 不实现搜索历史、热门词、搜索分析、索引管理 HTTP API 或浏览器直连 Elasticsearch。
- 不承诺 exactly-once；增量链路是至少一次消息加固定文档 ID 的幂等覆盖。
- 不把搜索消息绑定通知队列，不让 Business Worker 写 Elasticsearch，不使用 Kafka 同步业务搜索。
- 不建设 ES 集群、ILM、快照、生产安全加固或容量压测，不提前实现日志/事件索引。
- 不修改 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 数据归属与一致性

### 5.1 搜索文档与查询装配

Elasticsearch 文档只保存检索和稳定排序所需字段：

```json
{
  "post_id": 123,
  "title": "Kubernetes 实践",
  "content": "...",
  "created_at": "RFC3339Nano UTC",
  "updated_at": "RFC3339Nano UTC"
}
```

- Elasticsearch `_id` 使用十进制 `post_id`，同一帖子重复索引是幂等覆盖。
- 作者、评论数、点赞数和 `liked_by_me` 不写入索引，避免评论/点赞引发搜索同步和陈旧展示。
- Search API 先取有序 ID，再以一次有界 MySQL 查询装配现有 `post.Post` DTO，并恢复命中顺序。
- MySQL 中不存在的命中视为陈旧投影并忽略；不得根据 Elasticsearch 伪造帖子。

### 5.2 新帖子写入语义

Phase-03-02 后，帖子创建在一个 MySQL 事务中完成：

```text
BEGIN
  INSERT posts
  INSERT business_outbox(post.created.v1)
COMMIT
```

- posts 或 Outbox 任一写入失败时整体回滚。
- MySQL 提交成功后，即使 RabbitMQ、Indexer 或 ES 不可用，发帖仍成功；依赖恢复后自动补索引。
- 发布/确认边界允许重复，固定 `_id=post_id` 吸收重复。
- 搜索可见性是有界等待下的最终一致，不把 ES refresh 延迟表述为同步成功。

## 6. Elasticsearch 索引与部署契约

### 6.1 服务基线

- Compose 固定 `docker.elastic.co/elasticsearch/elasticsearch:9.5.2`；Go 使用官方 `github.com/elastic/go-elasticsearch/v9 v9.5.0`，服务端与客户端锁定同一 9.5 minor 兼容线，禁止 `latest` 或浮动 major。
- 本地单节点使用 `1` shard、`0` replica、512 MiB heap 和命名卷 `elasticsearch_data`。
- HTTP 只绑定 `127.0.0.1:${ELASTICSEARCH_PORT}:9200`，不复用可放宽的 `PUBLISHED_HOST`；本地可关闭安全插件但不得外部暴露。
- healthcheck 等待 cluster 至少 `yellow`；Bash 端口预检、启动等待、失败清理与只读验证覆盖该服务。
- `dev.sh` 在迁移和 Elasticsearch 健康后执行 `search-reindex --if-missing`：别名缺失时从 MySQL 初始化，已存在时不重复全量重建。

### 6.2 索引名称与 Mapping

```text
查询/写入别名：gopulse-post-search-v1
物理索引前缀：gopulse-post-search-v1-
物理索引示例：gopulse-post-search-v1-20260902T120000Z-a1b2c3d4
```

- `dynamic: strict`；`post_id` 为 `long`，时间为 `date_nanos`，`title`/`content` 为使用内置 `standard` analyzer 的 `text`。
- 第一版不引入外部分词插件；中文专项分词质量是后续非阻塞增强。
- 搜索、重建和 Indexer 共用一处版本化 Mapping，不复制多套 JSON。
- Phase 9 日志/事件索引不得复用本别名、前缀或 Mapping。

## 7. 全量重建策略

`backend/cmd/search-reindex` 是唯一受支持的重建入口，支持强制重建和 `--if-missing`：

1. 通过专用 MySQL 连接取得固定 advisory lock，阻止两个重建命令并发；不阻塞发帖事务。
2. 创建带随机后缀的新物理索引并应用共享 Mapping；删除只接受程序枚举并校验过的精确前缀，禁止 wildcard。
3. 记录 `MAX(posts.id)=H1`，按主键升序和有界 batch 扫描 `id <= H1`，Bulk 写入并检查每个 item。
4. refresh，并以 `post_id <= H1` 的 MySQL/ES 计数一致作为切换门槛；失败时旧别名不变并清理本次未发布索引。
5. 单次 aliases API 原子移除旧目标、添加新目标；别名必须且只解析到一个物理索引。
6. 切换后记录 `MAX(posts.id)=H2`，补写 `H1 < id <= H2` 并验证；`id > H2` 由已指向新别名的增量事件处理。
7. 成功后删除旧物理索引；切换后补偿失败则非零退出并保留新旧精确索引供诊断，不做不安全自动回滚。
8. 成功、失败和中断均释放锁，且不得删除当前别名目标或用户其他 ES 索引。

重建只以 MySQL 为源，不依赖 Outbox 历史、RabbitMQ 或 dead queue。

## 8. Search API 与 Frontend

### 8.1 Backend API

```text
GET /api/v1/search/posts?q=<keyword>&limit=<1..50>&cursor=<opaque>
```

- 路由沿用现有认证中间件。`q` trim 后为 1～200 Unicode code point；重复/空/超长参数与非法 limit/cursor 返回 `400 validation_failed`。
- 默认 `limit=20`、最大 `50`；成功沿用 `{data, meta.next_cursor}`，无命中返回空数组。
- 使用 `multi_match best_fields` 查询 `title^2`、`content`，按 `_score DESC, created_at DESC, post_id DESC` 和 `search_after` 分页。
- cursor 保存版本、规范化查询 SHA-256、物理 generation 和 sort tuple，并严格 canonical Base64URL 编码；跨查询/跨 generation 使用返回 `validation_failed`。
- ES 失败、超时、别名缺失或拒绝查询统一为 `503 search_unavailable`，不泄漏 URL、DSL、索引名、score 或节点错误。

### 8.2 Frontend

- 新增受保护 `/search` 和导航入口；查询词进入 URL query，刷新和前进/后退可恢复意图。
- 显式提交、清空、加载更多和重试；新查询清空旧结果/cursor，重复请求期间禁用动作。
- 复用 `PostCard`；区分首次加载、空结果、参数错误、服务不可用、cursor 失效和分页失败。
- Frontend 只调用 Backend 相对 API，不读取 `ELASTICSEARCH_URL` 或访问 9200。

## 9. 增量事件、拓扑与复用前整改

### 9.1 Phase 2 Review 风险关闭门槛

- 增加运行时 cleanup 及 `OUTBOX_PUBLISHED_RETENTION=168h`、`OUTBOX_CLEANUP_INTERVAL=1h`、`OUTBOX_CLEANUP_BATCH=500`；只删超期 published。
- 默认 `OUTBOX_LEASE_DURATION=60s`，配置强制 `lease_duration >= claim_batch × publish_timeout + 5s`，危险组合启动失败。
- Worker 为在途处理创建可取消 context 并拥有 goroutine；shutdown 停止 delivery、超时 cancel，`Run` 返回前确保 handler 已退出。
- 整改只关闭已知复用风险，不进行通用消息框架重写或 Phase 2 功能扩展。

### 9.2 事件与独立拓扑

`post.created` 继续使用严格 Envelope v1，只包含 event/schema/type/time/actor/post；recipient/comment 必须缺失，标题正文不进 Payload。routing key 为 `post.created.v1`，Outbox CHECK 显式扩展。

```text
direct exchange: gopulse.search.v1
routing key:     post.created.v1
main queue:      gopulse.search-indexer.v1
retry exchange:  gopulse.search.retry.v1
retry queue:     gopulse.search-indexer.retry.v1
dead exchange:   gopulse.search.dead.v1
dead queue:      gopulse.search-indexer.dead.v1
```

- Search 与通知拓扑完全隔离；Publisher 按事件类型选 exchange 并在 mandatory publish 前声明对应持久拓扑。
- `backend/cmd/search-indexer` 只加载 MySQL、RabbitMQ、ES 和自身配置；每次按 post ID 回读 MySQL并 PUT 稳定别名。
- manual ack；临时 MySQL/网络/429/5xx 有限重试，非法消息、事实不存在或确定 Mapping 4xx 进入 search dead queue。
- Search dead queue 只供诊断；全量重建是增量缺口的最终修复路径。

## 10. 进程、配置与故障边界

- `/ready` 增加 `elasticsearch`；ES down 只使 search 不可用，既有 MySQL-backed API 保持自身边界。
- `search-reindex` 只加载 MySQL/ES；Search Indexer 不依赖 Gin、Redis、JWT、Cookie 或 Frontend。
- ES 配置固定为必填 `ELASTICSEARCH_URL`、默认 `ELASTICSEARCH_PORT=9200`、`ELASTICSEARCH_REQUEST_TIMEOUT=3s`、`SEARCH_REINDEX_BATCH=500`，均提供合理上下限且 URL 禁止携带 userinfo。
- Indexer 配置固定为 `SEARCH_INDEXER_PREFETCH=10`、`SEARCH_INDEXER_MAX_RETRIES=3`、`SEARCH_INDEXER_RETRY_DELAY=30s`、`SEARCH_INDEXER_PUBLISH_TIMEOUT=5s`、`SEARCH_INDEXER_SHUTDOWN_TIMEOUT=10s`、`SEARCH_INDEXER_RECONNECT_MIN=500ms`、`SEARCH_INDEXER_RECONNECT_MAX=30s`；Backend Publisher 与 Indexer 必须读取相同 retry delay 以声明一致队列参数。
- 第 9.1 节 Outbox cleanup/lease 参数同样定义默认、上下限与交叉关系；不把内部 exchange/index 名开放为任意环境变量。
- `dev.sh` 管理 Backend、Business Worker、Search Indexer、Frontend；两个 Worker 使用独立二进制、进程组和 `.run` 记录。
- `down.sh` 以 cwd、executable、start ticks、marker 验证归属；ES volume 默认保留。
- 日志不输出完整连接 URL、凭据、JWT、Cookie、正文、事件 Payload 或原始 ES 响应体。

## 11. 批次依赖与摘要

```text
03-01 可重建搜索闭环
  ↓
03-02 复用风险整改 + 可靠增量索引
  ↓
03-03 MVP 集成验收 + 候选收口（0.4.3）
  ↓
03-04 用户主动发起独立 Review（预留 0.4.4）
  ↓
Milestone-01-Release（非 Phase 批次）：1.0.0
```

- [Phase-03-01：可重建帖子搜索闭环](Phase-03-01-可重建帖子搜索闭环.md)：交付 ES、重建、Search API 和 Frontend 的完整历史搜索闭环。
- [Phase-03-02：可靠增量索引与运行闭环](Phase-03-02-可靠增量索引与运行闭环.md)：关闭复用风险并使新帖子自动、可靠、最终可搜索。
- [Phase-03-03：MVP 集成验收与候选收口](Phase-03-03-MVP集成验收与候选收口.md)：不新增功能，以封闭矩阵证明两批能力共同运行并形成 `0.4.3` MVP 候选版。
- Phase-03-04：仅在本总方案中预留 `develop/0.4.4`；用户主动发起后再确定 Review 范围与交付物，当前不创建文件驱动的 Review 方案。

两个纵向实现批次加一个 MVP 候选收口批次构成规划可执行范围；第四批是为用户主动 Review 保留的治理边界。除此之外不再按索引、Backend、Frontend 或测试机械增批。

## 12. 测试策略与固定矩阵

### 12.1 执行效率

- 每批先提取“新增行为 → 最低测试层 → 固定门禁”，只读直接受影响代码并在 10 分钟内开始实现。
- 没有具体失败时不读第三方依赖源码；新测试只证明验收、复现缺陷或保护已改变契约。
- 最终 diff 上固定门禁各一次；上下文压缩不触发重跑。可选调查 15 分钟无进展即停止。
- 门禁通过且无 P0/P1 后立即更新日志、版本并提交，不追加机会性重构、压测或搜索增强。

### 12.2 批次验证边界

| 批次 | 直接证据 | 必要回归 | 不重复 |
| --- | --- | --- | --- |
| Phase-03-01 | Mapping/Alias/Reindex、真实 MySQL/ES、Search API、搜索组件/E2E | 配置/readiness、帖子 hydration、Frontend 固定门禁、Compose/版本 | 不实现 RabbitMQ 增量；不跑 Phase 2 完整故障矩阵 |
| Phase-03-02 | 原子 post.created、独立 topology/Indexer、真实暂停/重复/恢复 | Outbox/Worker 改动边界、帖子创建、03-01 搜索读链路、Bash 身份安全 | 不重跑所有通知/认证组合；完整里程碑矩阵留 03-03 |
| Phase-03-03 | MVP 阶段矩阵一次、最终用户搜索闭环 | Backend/Frontend/脚本门禁、远程 CI、版本/文档 | 不执行独立 Review，不新增功能，不扩展封闭矩阵 |
| Phase-03-04 | 用户届时指定的 Review 证据 | 只按真实 finding 与实际风险确定 | 当前不预设清单、命令或整改范围，不自动执行 |

### 12.3 阶段级端到端矩阵

`scripts/verify-business.sh` 在随机 token、独立 Compose project/数据库/端口/进程目录/volume 和归属校验下固定覆盖：

1. 空环境迁移及 MySQL、Redis、RabbitMQ、ES、Backend、两个 Worker、Frontend 启动，health/readiness 正确。
2. 历史帖子分别标题命中、正文命中和不命中；重建后 API/浏览器得到正确且可跳转结果。
3. 排序来自 ES、展示来自 MySQL；评论/点赞后搜索结果显示最新计数和 `liked_by_me`，无需重写搜索文档。
4. 正常发帖后 posts 与 Outbox 原子存在，最终无需重建即可搜索。
5. 停止 Indexer 后继续发帖并恢复；消息保留，相同 event/post 重投只有一个逻辑文档。
6. 停止 ES 后发帖成功，search 返回 `search_unavailable`、readiness 降级；恢复后自动补齐。
7. 停止 RabbitMQ 后发帖成功且 Outbox 保留；恢复后自动投递和索引，无需客户端重发。
8. 精确删除验收搜索索引后从 MySQL 重建；历史、故障期间及代表性并发帖子均恢复。
9. 非法 q/limit/cursor、跨 query/generation cursor 和错误脱敏符合契约，Frontend 不直连 ES。
10. Phase 0～2 注册、登录、帖子、评论、点赞、通知和必要 Redis/RabbitMQ 降级共同运行；清理不影响日常资源。

以上为封闭矩阵；除非真实失败暴露 P0/P1，不追加分词排列、相关度黄金集、压力或节点故障组合。

## 13. 实施记录规则

每批完成后创建同名镜像记录：

```text
dev/imple/Phase-03/Phase-03-XX-<名称>.md
dev/logs/Phase-03/Phase-03-XX-<名称>.md
```

记录必须包含实际工作、文件、命令/结果、偏差、已知限制和跟进项；本次规划不提前创建空记录。

Phase-03-01～03 按已定义的拆分方案创建镜像记录。Phase-03-04 在用户主动发起前既不创建实施方案，也不创建空日志；发起后再依据实际 Review 工作确定报告、方案与记录之间的对应关系。

## 14. Phase 3 MVP 候选与 Milestone 1 验收标准

### 14.1 `0.4.3` MVP 候选完成条件

- 历史及新帖子均可按标题或正文搜索，并经 Backend/Frontend 形成真实闭环。
- 发帖事实与索引事件原子提交；RabbitMQ/Indexer/ES 故障不推翻已提交帖子。
- 搜索与通知消息、队列、Worker、retry/dead 隔离，至少一次与幂等语义如实记录。
- 搜索展示事实来自 MySQL；ES 只负责命中和排序。
- 精确删除搜索索引后可安全重建；切换前验证、原子 alias、无 wildcard 误删。
- Frontend 不直连 ES；Search API/readiness/降级和 cursor 契约稳定。
- Outbox cleanup、租约预算和 Worker ownership 三项复用风险关闭且有必要回归。
- 固定阶段矩阵和远程门禁通过，无 P0/P1；非阻塞问题已登记而未扩大实现。
- 三份实施记录与提交/命令一致，Phase-03-03 合入后根与 Frontend 版本均为 `0.4.3`。
- Phase 0～3 能共同运行，形成可供用户独立 Review 的完整 MVP 候选版。

满足以上条件即可标记“Phase 3 MVP 候选完成”，停止继续扩展实现；这不代表独立 Review 已执行，也不触发 `1.0.0` 发布。

### 14.2 `0.4.4` Review 与正式里程碑条件

- 用户已基于 `0.4.3` MVP 候选主动发起 Review，并明确当次范围。
- Review 的实际 finding、证据与结论已记录；需要整改的阻断项在 `develop/0.4.4` 内完成并通过相应验证。
- Phase-03-04 合入主远程后根与 Frontend 版本均为 `0.4.4`，远程门禁通过，且用户确认 Review 完成。
- 随后 release-only 动作使主远程唯一产品版本成为 `1.0.0`；未发生的 Review、合并、门禁或发布不得写成已完成。

## 15. 完成、停止与 Phase 4 交接

Phase-03-01～03 从权威分支完成并合入主远程、封闭矩阵在 WSL2/Bash 真实通过、远程门禁成功且记录齐全后，停止 Phase 3 MVP 功能实施，并把 `0.4.3` 标记为“待用户主动 Review”的 MVP 候选版。

只有用户随后发起并完成 Phase-03-04、`0.4.4` 合入且第 14.2 节满足，Phase 3 与 Milestone 1 才可正式收口并执行 `1.0.0` release-only 动作。规划代理不得自行启动、替代或扩大该 Review；未执行检查不得写成通过。

正式发布后向 Phase 4 交付已运行的 ES 基础设施与 readiness 模式、独立可重建的搜索索引契约、完整业务 MVP 和各进程的日志需求。Phase 4 只统一业务日志，不复用帖子搜索别名/Mapping，也不改写本阶段事实与消息语义。
