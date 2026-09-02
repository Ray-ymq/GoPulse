# Phase 3-02：可靠增量索引与运行闭环实施方案

> 执行序号：2 / 3
>
> 前置批次：Phase-03-01 已完成并通过验收
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

在已验证的搜索读/重建闭环上，为新帖子增加可靠、最终一致的自动索引：帖子事实与 `post.created` Outbox 原子提交，经独立 RabbitMQ 搜索拓扑和 Search Indexer 回读 MySQL，幂等写入稳定 Elasticsearch 别名。

本批同时把 Search Indexer 纳入 WSL2/Bash 生命周期与隔离故障验收，并在最小参数化现有 Worker 基础时保持 Phase 2 已完成的通知、Outbox 清理、整批租约预算和 cancellation-safe shutdown 契约。

## 2. 前置条件

- Phase-03-01 已合入主远程最新 `main`，根版本为 `0.4.1`。
- 历史重建、alias、Search API 和 Frontend 搜索闭环已通过真实 MySQL/Elasticsearch 验收。
- 已 fetch 主远程并从最新 `main` 创建 `develop/0.4.2`，工作树与日常运行资源状态已记录。
- Phase 2 的 Outbox cleanup、租约交叉校验和 Worker 在途 handler 回收已经存在；实施以真实 `0.3.6+` 代码为准，不重复建设这些能力。
- 当前 bus Envelope、Publisher exchange、Worker queue/retry/dead 和 self-event 逻辑均是通知专用固定实现，参数化只能覆盖搜索进程所需的最小边界。

## 3. 实施范围

### 3.1 `post.created` 与原子帖子写入

- 扩展 Envelope v1 事件类型和按类型字段校验，新增 `post.created` 与 `post.created.v1`；现有评论/点赞消息的 JSON 与 metadata 输出保持兼容。
- 将通知专用的 `recipient_id` 调整为按事件类型可选：评论/点赞仍要求正整数，`post.created` 必须省略；`comment_id` 同样按事件类型严格校验。
- 新增 migration 扩展 `business_outbox.event_type` CHECK。down migration 先移除仅搜索用途的 `post.created` Outbox 行，再恢复旧约束，不删除对应 MySQL 帖子事实。
- 帖子 Repository 增加事务化生产构造器，在同一 `*sql.Tx` 写 posts、读取创建结果并插入 Outbox。
- 事件只携带 event/actor/post/time，不复制标题正文；Outbox 失败回滚帖子，RabbitMQ/Elasticsearch 不参与 HTTP 事务。
- 无 Outbox 构造方式只保留给直接测试或明确工具；生产 Server 必须注入 Outbox writer。

### 3.2 独立 Search RabbitMQ 拓扑与 Publisher 路由

- 新增 `gopulse.search.v1` 以及固定 main/retry/dead exchange/queue，只绑定 `post.created.v1`。
- 通知 queue 继续只绑定 `comment.created.v1`、`post.liked.v1`；不得依赖 Consumer 收到错误类型后再忽略来隔离职责。
- Outbox Publisher 按事件类型选择固定 exchange，连接/Channel 建立时声明所需持久拓扑，使用 persistent、mandatory 与 publisher confirm；未知类型不得发布。
- Search retry 使用独立 TTL、attempt header 和 dead queue；retry/dead 二次发布 confirm 后才 ack 原消息，publish 失败时 requeue。
- 拓扑名称、允许 routing key、invalid key 和 retry delay 契约集中定义，由 Server、两个 Worker 和测试共享；内部名称不开放为任意环境变量。

### 3.3 Worker 最小参数化与 Search Indexer

- 以固定 Business/Search profile 参数化现有 Worker runtime/handler：拓扑声明、消费 queue、consumer tag、retry/dead exchange、允许 routing key、日志前缀和 self-event 策略。
- Business profile 行为保持不变：只接受通知事件、保留 self-event 防御、断线重连、手动 ack、confirm-gated retry/dead 和在途 handler 回收。
- 新增 `cmd/search-indexer`，只加载 MySQL、RabbitMQ、Elasticsearch 和 `SEARCH_INDEXER_*`；使用 Search profile，不加载 Redis、Gin、JWT、Cookie 或 Frontend 配置。
- Search processor 严格只接受 `post.created`，按 post ID 回读 MySQL 文档，并以 `_id=post_id`、`require_alias=true` PUT `gopulse-post-search-v1`。
- 扩展 Worker 错误分类，使 Processor 明确永久错误可以直接进入 dead：非法/错路由消息、MySQL 不存在事实和确定 Mapping 4xx 为永久；临时 MySQL、网络、429、5xx 为可重试。
- alias 缺失不自动创建索引，按临时错误有限重试；耗尽后进入 search dead queue，运维恢复依靠 `search-reindex`，本批不自动重放 dead queue。

### 3.4 生命周期与故障闭环

- `dev.sh` 构建并启动 Search Indexer，使用独立二进制、进程组和 `.run/search-indexer.json`；启动失败只清理本次启动的应用进程。
- `down.sh`/`verify.sh` 使用与 Business Worker 同等级的 executable/cwd/start ticks/marker 归属校验，不能凭陈旧 PID 杀进程。
- `verify-business.sh --search-live` 覆盖正常增量、Indexer 暂停/恢复、Elasticsearch/RabbitMQ 停止恢复、重复投递和代表性重建并发路径。
- 以 Search API 或 Frontend 观察最终结果，不以 RabbitMQ management 中消息消失、Outbox `published` 或 Elasticsearch doc count 单独替代可搜索结果。
- CI 增加真实 Elasticsearch service 与必要配置，只扩展直接受影响的 integration 和脚本门禁。

### 3.5 Phase 2 契约保护

- 新增搜索事件后，published Outbox 继续按现有 retention/interval/batch 有界清理，pending/leased 不进入清理条件。
- Dispatcher 仍要求 `claim_batch × publish_timeout + 1s <= lease_duration`；不得因多 exchange routing 降低该安全预算。
- Runtime 参数化后，context cancellation、连接/Channel 中断和 shutdown timeout 均须等待当前 handler 退出后才返回。
- 评论与首次点赞仍原子写 Outbox，并只生成一条幂等通知；重复点赞、自操作和 unlike 语义不变。

## 4. 实施边界与非目标

- 不让 Business Worker 处理搜索事件，不让 Search Indexer 创建通知。
- 不把标题/正文放进 RabbitMQ Payload，不以 dead queue、已发布 Outbox 或 Elasticsearch 文档代替 MySQL 重建。
- 不实现帖子更新/删除事件、Outbox 通用多租户框架、动态拓扑、队列自动伸缩或 parallel bulk consumer。
- 不自动重放 dead queue，不提供索引运维 Web/API 后台。
- 不重写已通过 Review 的 Dispatcher/Worker 状态机；参数化只服务两个固定 profile。
- 不重跑与本批无关的 Phase 2 全排列故障测试，只回归通知代表性成功路径和实际触达的可靠性边界。
- 不调整 03-01 公共搜索语义，除非真实阻断缺陷需要最小兼容修复。
- 不修改 PowerShell、Kafka、日志/事件索引或后续 Phase 文件。

## 5. 预计文件与交付物

```text
backend/migrations/000004_*.up.sql
backend/migrations/000004_*.down.sql
backend/internal/bus/
backend/internal/outbox/
backend/internal/platform/rabbitmq_*.go
backend/internal/worker/
backend/internal/post/
backend/internal/search/
backend/internal/config/
backend/cmd/server/
backend/cmd/business-worker/
backend/cmd/search-indexer/
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-business.sh
scripts/ci/
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-03/Phase-03-02-可靠增量索引与运行闭环.md
```

预计文件只表示允许触达的边界；Frontend 主体未改变时不为了“对称”制造页面改动，实际变更以实现记录为准。

## 6. 详细实施步骤

1. 核对 Phase-03-01 实施记录、最新 `main`、搜索契约和 Phase 2 已关闭可靠性边界，提取本批最小验证清单。
2. 扩展严格 Envelope、metadata、routing key 与 Outbox CHECK；先用兼容 fixture 证明现有 comment/like wire shape 不变。
3. 将帖子创建改为 posts + Outbox 原子事务，覆盖代表性成功和 Outbox 失败回滚，生产装配禁止无 Outbox 路径。
4. 定义独立 Search topology，改造 Publisher 的 event-to-exchange 选择，证明通知/搜索绑定互不串流。
5. 将 Worker runtime/handler 参数化为两个固定 profile，保留 Business profile 的 self-event、retry/dead、重连和退出行为。
6. 为 Processor 增加可判定永久错误边界，实现 Search Indexer config、MySQL loader、Elasticsearch processor 和命令入口。
7. 接入 Bash 生命周期的 Indexer 身份记录、启动回滚、有界停止和只读诊断。
8. 在真实隔离依赖上验证发帖最终可搜索、Indexer 暂停/恢复、重复投递与 Elasticsearch/RabbitMQ 恢复；结果由 Search API 断言。
9. 代表性并发发帖期间执行重建，确认 H1/H2 补偿与 alias 写入协作不遗漏最终 MySQL 事实。
10. 回归评论和首次点赞各一条真实通知路径，确认搜索事件不进入通知 queue，通知事件不进入搜索 queue。
11. 更新 CI Elasticsearch service、README、版本和实施记录，只写入实际执行证据与接受限制。

## 7. 风险与控制

- **Envelope 兼容破坏通知**：按事件类型测试字段组合，以既有 comment/like fixture 固定 JSON 与 metadata 兼容。
- **拓扑串流**：使用独立 exchange/queue/binding，并在真实 RabbitMQ 断言两类事件只进入各自职责队列。
- **Worker 参数化引入回归**：使用固定 profile 而非任意配置；通知只回归一个评论和一个首次点赞代表性结果。
- **Elasticsearch 故障阻断发帖**：HTTP 事务只写 MySQL/Outbox，Indexer 独立重试，Search API 单独降级。
- **重复消息生成重复文档**：固定 `_id=post_id`，使用覆盖式 PUT 且要求 alias。
- **错误 alias 自动建索引**：所有增量写设置 `require_alias=true`；缺失时失败，最终从 MySQL 重建。
- **重建切换遗漏并发帖子**：切换后补偿 H1/H2，切换后的事件写稳定别名，以最终 Search API 与 MySQL 集合核对。
- **验收误伤用户资源**：随机 project/port/path/volume 与进程身份白名单，破坏前逐项确认。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败后只重跑受修复影响的项目：

```bash
(cd backend && go test ./internal/bus ./internal/outbox ./internal/platform ./internal/worker ./internal/post ./internal/search ./internal/config ./cmd/server ./cmd/business-worker ./cmd/search-indexer)
(cd backend && go vet ./internal/bus ./internal/outbox ./internal/platform ./internal/worker ./internal/post ./internal/search ./cmd/server ./cmd/business-worker ./cmd/search-indexer)
(cd backend && go test -race ./internal/outbox ./internal/worker ./internal/post ./internal/search)
(cd backend && go test -count=1 -tags=integration ./internal/outbox ./internal/worker ./internal/post ./internal/search)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
(cd frontend && npm run test:e2e -- --grep search-live)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-business.sh --search-live
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.2 --base-ref upstream/main
git diff --check
```

`--search-live` 只运行本批正常增量、拓扑隔离和代表性暂停/恢复矩阵。Frontend 未改变主体时仍运行固定门禁和一条搜索 E2E，因为它是最终搜索观察入口；不重跑 Phase 2 所有认证、retry/dead、Broker restart 组合。只有共享 runtime、事件或脚本的定向检查暴露跨组件回归时才扩展，并先在实施记录说明风险依据。

## 9. 验收标准

- 创建帖子时 posts 与 `post.created` 原子提交；Outbox 失败回滚，RabbitMQ/Elasticsearch 故障时已提交帖子不回滚。
- `post.created` Envelope 严格且不携带正文/标题；现有评论/点赞 JSON、metadata 与通知行为兼容。
- Publisher exchange 和 Search topology 版本化且固定，通知/搜索 queue 互不串流，未知事件不能被静默发布。
- Search Indexer 只回读 MySQL 并写要求 alias 的固定 `_id`，manual ack、有限 retry/dead、重连和退出符合契约。
- 正常新帖子无需重建即可最终搜索；Indexer、Elasticsearch、RabbitMQ 暂停/恢复后自动补齐或按明确重建路径恢复。
- 重复投递只保留一个逻辑文档；alias 缺失不生成错误动态索引，MySQL 重建仍能恢复全部事实。
- Outbox cleanup、整批租约预算和 Worker cancellation-safe shutdown 无回归；通知代表性闭环通过。
- Bash 安全管理 Backend、Business Worker、Search Indexer、Frontend 与四项基础设施，PowerShell 保持冻结。
- 第 8 节门禁通过，版本元数据为 `0.4.2`，实施记录真实完整。

## 10. 明确完成条件

只有新帖子增量链路、独立搜索拓扑/进程生命周期、代表性故障恢复与重建并发协作通过，且 Phase-03-01 搜索读闭环和 Phase 2 通知可靠性边界无回归，才可标记本批完成。完整 Milestone 1 矩阵与独立 Review 留给 Phase-03-03。

## 11. 下一批交接

- 历史重建与新帖子增量两条可共同运行的 MySQL → Elasticsearch 投影路径。
- 独立搜索消息、queue、Worker 和明确的至少一次、幂等、retry/dead 语义。
- 已保留的 Outbox 容量、租约和 Worker 退出安全证据，以及通知代表性回归结果。
- 可由真实浏览器观察的搜索结果和可执行阶段故障矩阵的 Bash 基线。
