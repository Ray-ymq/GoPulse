# Phase 3-02：可靠增量索引与运行闭环实施方案

> 执行序号：2 / 3
>
> 前置批次：Phase-03-01 已完成并通过验收
>
> 总方案来源：[Phase-03-总实施方案.md](Phase-03-总实施方案.md)

## 1. 批次目标

在已验证的搜索读/重建闭环上，为新帖子增加可靠最终一致的自动索引：帖子事实与 `post.created` Outbox 原子提交，经独立 RabbitMQ 搜索拓扑和 Search Indexer 回读 MySQL，幂等写入稳定 Elasticsearch 别名。

本批同时关闭 Phase 2 Review 中与扩展 Outbox/复用 Worker runtime 直接相关的三个 P2 风险，并把 Search Indexer 纳入 WSL2/Bash 生命周期与隔离故障验收。

## 2. 前置条件

- Phase-03-01 已合入主远程最新 `main`，根版本为 `0.4.1`。
- 历史重建、alias、Search API 和 Frontend 搜索闭环已通过真实 MySQL/ES 验收。
- 已 fetch 主远程并创建本批权威分支，工作树与用户资源状态已记录。
- 现有 Outbox、Publisher 和 Worker runtime 的 Review 风险仍按真实代码处理，不假定已修复。

## 3. 实施范围

### 3.1 复用前最小整改

- 在 Backend Dispatcher 生命周期旁启动可取消、低频、有界的 published Outbox cleanup；增加 168h retention、1h interval、500 batch 默认值与上下限。
- cleanup 只调用现有 `CleanupPublished`，严格保留 pending/leased；失败记录收敛日志，不阻断 HTTP 或 Dispatcher。
- 配置强制 `lease >= claim_batch × publish_timeout + 5s`，默认 lease 改为 60s；覆盖默认值通过和危险组合拒绝。
- 修复 Worker runtime 的 processing context 与 goroutine ownership：停止拉取、等待、超时 cancel、确认 handler 退出后 `Run` 才返回。
- 只修改证明上述 finding 所需边界，不顺带重写 Dispatcher、通用调度器或通知业务。

### 3.2 `post.created` 与原子帖子写入

- 扩展 Envelope v1 事件类型和按类型字段校验，新增 `post.created.v1`；现有通知编码保持兼容。
- Migration 扩展 `business_outbox.event_type` CHECK；down migration 恢复原约束前安全处理新类型数据，不能留下不可执行回滚。
- 帖子 Repository 增加事务化生产构造器，在同一 `*sql.Tx` 写 posts、读取创建结果并插入 Outbox。
- 事件只携带 event/actor/post/time，不复制标题正文；Outbox 失败回滚帖子，Broker/ES 不参与 HTTP 事务。
- 无 Outbox 构造方式只保留给直接测试/工具；生产 Server 必须注入 Outbox writer。

### 3.3 独立 Search RabbitMQ 拓扑

- 新增 `gopulse.search.v1` 及固定 main/retry/dead exchange/queue，只绑定 `post.created.v1`。
- 通知 queue 继续只绑定评论/点赞；不得依赖 Consumer 收到错误类型后再忽略来隔离职责。
- Publisher 按事件类型选择固定 exchange，声明对应持久拓扑后使用 persistent、mandatory、confirm；未知类型不得发布。
- Search retry 使用独立 TTL/count；retry/dead 二次发布收到 confirm 后才 ack 原消息。
- 拓扑名称集中定义并由 Server、Indexer、测试共享，不通过管理 UI 手工创建。

### 3.4 Search Indexer

- 新增 `cmd/search-indexer`，只加载 MySQL、RabbitMQ、ES 和 `SEARCH_INDEXER_*`；Backend Publisher 与 Indexer 读取相同 `SEARCH_INDEXER_RETRY_DELAY`，防止 durable queue 参数漂移。
- 复用修正后的 Worker runtime 可取消消费骨架，以独立 topology/queue 参数运行；Business Worker 行为不变。
- 严格校验 AMQP metadata/Envelope，只接受 `post.created`；按 post ID 回读 MySQL document 并 PUT 稳定别名。
- 成功后 manual ack；网络/429/5xx/临时 MySQL 有限 retry，非法消息/不存在事实/确定 Mapping 4xx 进入 dead。
- 相同 event 或 post ID 重投收敛为同一 `_id`；日志只含 event ID、post ID、attempt 和有限 reason code。
- alias 缺失时不自动创建无 Mapping 索引，按临时不可用重试，最终恢复由 `search-reindex` 保证。

### 3.5 生命周期与故障闭环

- `dev.sh` 构建/启动 Search Indexer，使用独立二进制、进程组和 `.run/search-indexer.json`；启动失败只清理本次资源。
- `down.sh`/`verify.sh` 使用与 Business Worker 同等级的 executable/cwd/start ticks/marker 归属校验。
- 隔离验收覆盖正常增量、Indexer 暂停/恢复、ES/RabbitMQ 停止恢复、重复投递、Worker 重启和重建并发代表性路径。
- 以 Search API/Frontend 观察最终结果，不以 RabbitMQ management 中消息消失替代可搜索结果。

## 4. 实施边界与非目标

- 不让 Business Worker 处理搜索事件，不让 Search Indexer 创建通知。
- 不把标题/正文放进 RabbitMQ Payload，不以 dead queue 或 published Outbox 代替 MySQL 重建。
- 不实现帖子更新/删除事件、Outbox 通用多租户框架、队列自动伸缩或 parallel bulk consumer。
- 不自动重放 dead queue，不提供索引运维 Web/API 后台。
- 不重跑与本批无关的 Phase 2 全排列故障测试；只回归整改边界与通知代表性行为。
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

## 6. 详细实施步骤

1. 核对 03-01 实施记录、最新 `main` 和 Phase 2 Review 三项 finding 的当前证据。
2. 先接入 cleanup runtime 和安全配置，以最低层测试证明 pending/leased 不删、危险 lease 组合拒绝。
3. 修复 Worker context/ownership，以可取消阻塞 Processor 证明 timeout 后没有旧副作用，并回归正常通知。
4. 扩展严格 Envelope、routing key、Outbox CHECK 与兼容测试；现有 comment/like fixture 输出不变。
5. 帖子创建改为 posts + Outbox 原子事务，覆盖代表性成功和 Outbox 失败回滚。
6. 定义独立 Search topology，改造 Publisher 的 event-to-exchange 选择；证明通知/搜索绑定互不串流。
7. 将 Worker runtime 的 queue/topology 参数化到两个进程所需的最小程度，不建设插件框架。
8. 实现 Search Indexer config、MySQL loader、ES processor、错误分类和命令入口。
9. 接入 Bash 生命周期的 Indexer 身份记录、启动回滚、有界停止与只读诊断。
10. 在真实隔离依赖上验证发帖最终可搜索、暂停/恢复、重复投递与依赖恢复；结果由 Search API 断言。
11. 代表性并发发帖期间执行重建，确认 H1/H2 补偿和 alias 写入不遗漏事实。
12. 更新 CI Elasticsearch service、README、版本和实施记录，只写入实际证据与限制。

## 7. 风险与控制

- **扩展事件破坏通知**：按事件类型测试字段组合，拓扑独立，回归评论/点赞各一个代表性成功场景。
- **增量放大旧风险**：cleanup、lease budget 先于新事件启用，配置不安全时启动失败。
- **Worker 退出重复副作用**：timeout cancel 后等待 owned goroutine，消息可重投但旧 handler 不存活。
- **ES outage 阻断发帖**：HTTP 事务只写 MySQL/Outbox，Indexer 独立重试，Search API 单独降级。
- **重复消息生成重复文档**：固定 `_id=post_id`，使用覆盖式 PUT。
- **错误 alias 自动建索引**：禁用该依赖，alias 缺失明确失败并要求重建。
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
git diff --check
```

`--search-live` 只运行本批正常增量和代表性暂停/恢复矩阵。Frontend 未改变主体时仍运行固定门禁和一条 E2E，因为它是最终搜索观察入口；不重跑 Phase 2 全量浏览器旅程或所有 retry/dead 组合。

## 9. 验收标准

- published Outbox 有真实 cleanup，危险 lease/batch/publish 组合被拒绝，pending/leased 不被删除。
- Worker shutdown 后没有 owned handler goroutine；Business Worker 通知语义无回归。
- 创建帖子时 posts 与 `post.created` 原子提交；Outbox 失败回滚，Broker/ES 故障时已提交帖子不回滚。
- 事件、Publisher exchange 和 Search topology 严格版本化，通知/搜索队列互不串流。
- Search Indexer 只回读 MySQL 并写 alias，manual ack、有限 retry/dead、重连和退出符合契约。
- 正常新帖子无需重建即可最终搜索；Indexer、ES、RabbitMQ 暂停/恢复后自动补齐。
- 重复投递只保留一个逻辑文档；alias 缺失不生成错误动态索引，MySQL 重建仍能恢复事实。
- Bash 安全管理两个 Worker 与四项基础设施，PowerShell 保持冻结。
- 第 8 节门禁通过，版本元数据为 `0.4.2`，实施记录真实完整。

## 10. 明确完成条件

只有新帖子增量链路、三个复用风险整改、独立进程生命周期和代表性故障恢复通过，且 03-01 搜索读闭环无回归，才可标记本批完成。完整 MVP 集成矩阵留给 Phase-03-03；独立 Review 仅在 MVP 候选完成后由用户通过预留的 Phase-03-04 主动发起。

## 11. 下一批交接

- 历史重建与新帖子增量两条可共同运行的 MySQL → Elasticsearch 投影路径。
- 独立搜索消息/队列/Worker 和明确的至少一次、幂等、retry/dead 语义。
- 已关闭的 Outbox 容量、租约与 Worker 退出风险证据。
- 可由浏览器观察的搜索结果和可执行阶段故障矩阵的 Bash 基线。
