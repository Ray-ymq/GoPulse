# Phase-03-02：可靠增量索引与运行闭环开发记录

## 1. 执行基线

- 执行日期：2026-09-02。
- 目标版本与分支：`0.4.2` / `develop/0.4.2`；根 `VERSION`、Frontend `package.json` 与 lockfile 已统一更新为 `0.4.2`。
- 执行环境：WSL2/Linux 文件系统中的 `/home/ray/GoPulse`，使用 Bash 与单一 Docker daemon；冻结的 PowerShell 脚本未修改。
- 计划前置条件存在偏差：执行开始时 `origin/main` 仍是 `5fd6bd3`（版本 `0.3.6`），Phase-03-01 尚未合入远程 `main`。按批次依赖先从 `origin/main` 创建 `develop/0.4.2`，再 cherry-pick Phase-03-01，形成前置提交 `2897e3e feat: add rebuildable post search`，随后实施本批；未改写或推送既有远程分支。
- 开始时工作树干净；日常 `gopulse` 与既有 `gopulse-phase0203-integration` 容器、卷、`.env` 和 `.run` 内容均作为已有资源保留，隔离验收只清理其自行创建且通过归属校验的资源。

## 2. 实际完成内容

### 2.1 严格事件与原子帖子写入

- 为 Envelope v1 增加 `post.created` / `post.created.v1`，按事件类型严格校验字段组合；搜索事件仅携带 event、actor、post、time，JSON 省略 `recipient_id`、`comment_id`、标题和正文。
- 保持 `comment.created` 与 `post.liked` 的既有 wire shape、metadata、正整数 recipient 及 comment 约束。
- 新增 `000004_post_created_outbox` migration，扩展 `business_outbox.event_type` CHECK；down 仅删除无法满足旧约束的 `post.created` Outbox 行，不删除帖子事实。
- 为生产 Server 注入带 Outbox 的帖子 Repository。帖子插入、创建结果读取和 `post.created` Outbox 写入在同一 MySQL 事务中提交；Outbox 失败会回滚帖子，RabbitMQ 与 Elasticsearch 不进入 HTTP 事务。
- 增加集成验证，覆盖帖子与 Outbox 同时提交，以及强制 Outbox 失败时两者均不落库。

### 2.2 独立 RabbitMQ 搜索拓扑与 Publisher 路由

- 增加固定 `gopulse.search.v1`、`gopulse.search-indexer.v1` 及独立 retry/dead exchange/queue，只绑定 `post.created.v1`。
- 通知拓扑继续只绑定 `comment.created.v1` 与 `post.liked.v1`；搜索与通知隔离在 exchange/queue/binding 层完成。
- Publisher 根据事件类型选择固定 Business 或 Search exchange，连接建立时以各自 TTL 声明两套持久拓扑，并继续使用 persistent、mandatory 与 publisher confirm；未知事件类型直接拒绝。
- 拓扑名称、允许 routing key、invalid key、queue 与 retry/dead 契约集中在 `platform` 固定定义，没有开放任意环境变量覆盖内部名称。

### 2.3 Worker 固定 Profile 与 Search Indexer

- 将现有 Worker runtime/handler 最小参数化为固定 `BusinessProfile` 与 `SearchProfile`，分别定义拓扑、消费 queue、consumer tag、允许 routing key、retry/dead exchange 和 self-event 策略。
- Business profile 保留通知 self-event 防御、手动 ack、confirm-gated retry/dead、重连、在途 handler 回收与 cancellation-safe shutdown。
- 增加 Processor 永久错误类型。解码/路由错误、缺失 MySQL 帖子事实和确定的 Elasticsearch mapping 4xx 直接进入 search dead；MySQL 临时错误、网络错误、429、404/alias 缺失与 5xx 进入有限 retry，耗尽后进入 dead。
- 新增 `cmd/search-indexer` 及独立配置加载边界，仅读取 MySQL、RabbitMQ、Elasticsearch 与 `SEARCH_INDEXER_*`；无效 Redis、HTTP、JWT 等应用配置不会阻止 Indexer 启动。
- Search processor 收到 `post.created` 后按 post ID 回读 MySQL 权威文档，并使用稳定 `_id=post_id`、`require_alias=true` PUT 到 `gopulse-post-search-v1`，不会因 alias 缺失自动创建错误索引。

### 2.4 Bash 生命周期、隔离验收与 CI

- `scripts/dev.sh` 构建并启动独立 Search Indexer 二进制，写入 `.run/search-indexer.json`；启动回滚仅清理本次应用进程。
- `scripts/down.sh` 和 `scripts/verify.sh` 以 executable、cwd、start ticks 与 marker 校验 Search Indexer 进程归属，安全边界与 Business Worker 对齐。
- `scripts/verify-business.sh --search-live` 增加隔离增量搜索验收：拓扑绑定隔离、正常收敛、Indexer 暂停/恢复、重复投递、RabbitMQ 与 Elasticsearch 停止/恢复、代表性并发重建协作，以及真实 Chromium 最终观察。
- 验收使用随机 Compose project、端口、路径和卷，并在清理前校验 token、project、容器与端口归属；验收结束只删除本次资源。
- CI integration job 增加固定 Elasticsearch `9.5.2` service 与连接配置；README、示例环境、生命周期和故障语义同步更新。
- 为并行 integration package 门禁修正 MySQL advisory lock：由会超过生产连接 1 秒 read timeout 的阻塞式 `GET_LOCK(..., 30)` 改为 `GET_LOCK(..., 0)` 有界轮询。该变更源于固定集成门禁中的真实竞争，保持同一跨 package 串行化契约而不放宽生产连接超时。

## 3. 实际变更文件

- 配置、版本与文档：`.env.example`、`.github/workflows/quality-gates.yml`、`README.md`、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`。
- 事件、拓扑与发布：`backend/internal/bus/envelope.go`、`backend/internal/bus/envelope_test.go`、`backend/internal/platform/rabbitmq_publisher.go`、`backend/internal/platform/rabbitmq_topology.go`、`backend/internal/platform/rabbitmq_topology_test.go`。
- 帖子事务与 migration：`backend/internal/post/repository.go`、`backend/internal/post/integration_test.go`、`backend/migrations/000004_post_created_outbox.up.sql`、`backend/migrations/000004_post_created_outbox.down.sql`、`backend/migrations/embed_test.go`。
- Worker 与 Indexer：`backend/internal/worker/profile.go`、`backend/internal/worker/decoder.go`、`backend/internal/worker/handler.go`、`backend/internal/worker/handler_test.go`、`backend/internal/worker/runtime.go`、`backend/cmd/business-worker/main.go`、`backend/cmd/search-indexer/main.go`。
- 搜索与配置：`backend/internal/search/processor.go`、`backend/internal/search/processor_test.go`、`backend/internal/config/config.go`、`backend/internal/config/search_indexer.go`、`backend/internal/config/search_test.go`、`backend/cmd/server/main.go`。
- 集成测试基础设施：`backend/internal/integrationtest/mysql_lock.go`。
- 生命周期与验收：`scripts/dev.sh`、`scripts/down.sh`、`scripts/verify.sh`、`scripts/verify-business.sh`、`scripts/ci/test_verify_business.py`、`frontend/e2e/business.spec.ts`。
- 本记录：`dev/logs/Phase-03/Phase-03-02-可靠增量索引与运行闭环.md`。

## 4. 实际验证

### 4.1 实现期间定向检查

以下定向检查均通过：

- `go test ./internal/bus ./internal/search`（`backend`）。
- `go test ./internal/platform ./internal/worker ./internal/search ./internal/config ./migrations`（`backend`）。
- `go test ./...`（`backend`）。
- `bash -n scripts/dev.sh`、`bash -n scripts/down.sh`、`bash -n scripts/verify.sh`、`bash -n scripts/verify-business.sh`。
- `python3 -m unittest scripts.ci.test_verify_business`。
- `python3 scripts/ci/validate_versions.py`。

### 4.2 隔离增量搜索验收

- `scripts/verify-business.sh --search-live`：最终通过。
- 真实 RabbitMQ 断言通知和搜索 binding 互不串流；首个帖子正常收敛后，两类 queue 的允许与禁止 routing key 均符合固定拓扑。
- Search Indexer 停止期间的帖子在重启后补齐；重复投递仍只有一个逻辑 Elasticsearch 文档。
- RabbitMQ 与 Elasticsearch 分别停止、恢复后，有限 retry 或明确重建路径使最终搜索结果恢复；alias 写入使用 `require_alias=true`。
- 代表性重建并发路径验证 H1/H2 补偿与增量写入共同收敛到 MySQL 最终事实。
- 验收内执行 `npm run test:e2e -- --grep search-live`，真实 Chromium 用例 1 项通过。
- 最终输出确认 topology isolation、atomic Outbox、pause/restart、duplicate、broker、Elasticsearch、rebuild cooperation 和 browser observation 全部收敛；隔离资源随后按归属清理。

### 4.3 最终固定完成门禁

- Backend 定向 `go test`：通过。
- Backend 定向 `go vet`：通过。
- `go test -race ./internal/outbox ./internal/worker ./internal/post ./internal/search`：通过，未发现数据竞争。
- `go test -count=1 -tags=integration ./internal/outbox ./internal/worker ./internal/post ./internal/search`：在新建、迁移并最终清理的 whitelisted disposable MySQL/Redis/RabbitMQ 安全目标上通过；outbox、worker、post、search 四个 package 均通过，其中 Worker 集成场景回归了评论与首次点赞的代表性通知路径。
- `npm test -- --run`：通过，9 个测试文件、44 项测试。
- `npm run typecheck`：通过。
- `npm run build`：通过，完成 `vue-tsc --noEmit` 与 Vite production build。
- `npm run test:e2e -- --grep search-live`：未提供隔离验收 seed 环境变量时按设计跳过 1 项；同一 Chromium 场景已在 `--search-live` 中实际通过。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`：通过。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- `scripts/verify-business.sh --self-test`：通过；接受 1 个合法目标并拒绝 6 个不安全目标，未访问 Docker。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，19 项治理测试。
- `python3 scripts/ci/validate_versions.py`：通过，版本元数据一致为 `0.4.2`。
- `python3 scripts/ci/validate_branch.py --branch develop/0.4.2 --base-ref upstream/main`：通过；develop 分支校验不解析当前不存在的 `upstream/main`。
- `git diff --check`：通过。

## 5. 失败、修复与方案偏差

- `--search-live` 首次运行在 Bash `set -u` 下因 topology helper 的局部变量在同一 `local` 语句中提前展开而报 `queue: unbound variable`；拆分赋值后修复。
- 第二次 `--search-live` 在 Publisher 尚未因首个事件惰性声明拓扑前检查 binding；将 binding 断言移动到首个帖子收敛后。为覆盖 Elasticsearch 重启时长，隔离验收把 Indexer 最大重试设为 20、retry delay 设为 2 秒；产品示例默认仍为 3 次、30 秒。修复后只重跑受影响的隔离验收并通过。
- 最初单独运行新增帖子 integration 测试时未提供 `INTEGRATION_TESTS=1`，按安全设计失败；没有绕过 whitelist。
- 使用已有 `gopulse-phase0203-integration` 数据库重跑时，历史帖子污染了稳定排序断言；未清空该已有资源，改用随机 disposable project。
- 首个全新 disposable 环境上的固定多 package integration 命令暴露跨 package advisory lock 竞争：Business Worker 测试持锁超过生产 MySQL 连接的 1 秒 read timeout，Post 测试的阻塞式 `GET_LOCK` 因而得到 invalid connection。将共享 integration helper 改为非阻塞有界轮询后，在另一个全新 disposable 环境重跑相同固定命令并通过。
- 除远程 `main` 缺少 Phase-03-01 的前置偏差和上述由必需门禁触发的 test helper 修复外，没有扩大产品范围；未修改 PowerShell、帖子更新/删除语义或 Phase-03-03 内容。

## 6. 已知限制与后续项

- Search Indexer 提供至少一次投递、固定 ID 幂等覆盖和有限 retry/dead；本批不自动重放 dead queue。运维恢复依靠修复依赖后重建或后续明确的人工处理。
- 仅实现 `post.created` 增量索引；帖子更新和删除事件、索引投影及相关一致性策略仍属后续范围。
- MySQL 始终是权威事实源，Elasticsearch 是可重建投影；搜索暂时一致而非同步强一致。
- 本地 Elasticsearch 为单节点且关闭安全插件，仅适用于回环绑定的开发/验收环境；生产安全、HA、容量与监控不在本批范围。
- 完整 Milestone 1 跨阶段矩阵和独立 Review 留给 Phase-03-03。
- 本地实现、固定门禁和自动提交完成后，远程 push、PR、合并及远程 CI 仍需后续流程，不能预先声明完成。
