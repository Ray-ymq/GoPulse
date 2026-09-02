# Phase-02-01：事务 Outbox 与消息契约基础开发记录

## 1. 执行信息

- 日期：2026-09-02
- 分支：`develop/0.3.1`
- 目标版本：`0.3.1`
- 起始基线：`origin/main` 的 `0d96c2b`（Phase 1 最终版本 `0.2.7` 与 Phase 2 实施方案已合入）
- 依据：`dev/imple/Phase-02/Phase-02-01-事务Outbox与消息契约基础.md`
- 执行环境：WSL2 Linux、Bash、Go、Node.js/npm、Docker、MySQL 8.4、Redis 7.2

## 2. 实际完成工作

### 2.1 版本化业务事件契约

1. 新增独立 `backend/internal/bus` 包，不依赖 Gin Handler DTO。
2. 定义 v1 `Envelope`、`comment.created` 与 `post.liked` 两种事件类型，以及 `comment.created.v1`、`post.liked.v1` routing key。
3. 构造器使用 `crypto/rand` 生成规范 UUID，统一将发生时间归一化为 UTC，并校验 actor、recipient、post、comment 等正整数标识及事件字段组合。
4. Encoder/Decoder 将消息体限制为 16 KiB；Decoder 拒绝未知字段、多个 JSON 值、未知类型、未知版本、非 UTC 时间、非法 UUID 和不符合事件类型的字段组合。
5. 集中定义 AMQP metadata 规则：`event_id` 作为 `message_id`、`application/json` 作为 `content_type`、稳定事件类型作为 `type`、UTC 发生时间作为 timestamp。
6. Envelope 仅包含稳定标识与发生时间，不包含正文、用户名、凭据、Cookie、JWT、连接 URL 或底层错误。

### 2.2 RabbitMQ 拓扑契约

1. 新增集中式 durable direct topology 声明：
   - `gopulse.business.v1` / `gopulse.business-worker.v1`
   - `gopulse.business.retry.v1` / `gopulse.business-worker.retry.v1`
   - `gopulse.business.dead.v1` / `gopulse.business-worker.dead.v1`
2. 主队列、retry queue 和 dead queue 均绑定两种版本化 routing key。
3. Retry queue 使用有界 TTL，并通过 `x-dead-letter-exchange=gopulse.business.v1` 保留原 routing key 返回主交换机。
4. 声明接口直接兼容 `amqp091-go` Channel；RabbitMQ 对不兼容的既有参数返回错误时，声明函数携带具体拓扑名称返回失败，不静默忽略。
5. Fake channel 测试两次执行相同声明，验证 exchange/queue 的 durable、auto-delete、exclusive、internal、no-wait 参数、retry arguments 和全部 binding。
6. 本批没有创建常驻 AMQP 连接、Producer、Dispatcher 或 Worker。

### 2.3 `business_outbox` Migration

1. 新增 `000002_business_outbox` 双向迁移。
2. Up migration 创建自增内部 ID、唯一 `event_id`、事件类型、schema version、JSON payload、状态、可用时间、尝试次数、租约 owner/expiry、发布时间、有限错误类别和审计时间字段。
3. 数据库约束限制事件类型为本批两种、schema version 为 1、状态为 `pending` / `leased` / `published`，并检查状态与租约/发布时间字段组合。
4. 新增 pending 扫描、过期 lease 回收和 published 清理索引。
5. Down migration 仅执行 `DROP TABLE business_outbox`；嵌入迁移测试明确拒绝对 Phase 1 四张事实表进行 ALTER 或新增通知表。

### 2.4 Outbox Repository 与租约状态机

1. `Insert` 接受 `Executor`，可传入 `*sql.DB` 或活动 `*sql.Tx`；integration 测试验证事务 rollback 时 Outbox 行同步消失。
2. `Claim` 使用 `READ COMMITTED` 短事务、稳定 ID 顺序、有限 batch 和 `FOR UPDATE SKIP LOCKED`；事务在记录返回前提交，不跨 RabbitMQ 网络操作持有数据库锁。
3. Claim 同时回收已过期 lease；未过期 lease 不会被其他 owner 领取。
4. `MarkPublished` 仅允许当前 owner 在租约有效期内把记录转为 published。
5. `ReleaseFailed` 仅接受有限、无凭据的 failure code，递增尝试次数、清除租约并按可注入退避计算新的 `available_at`。
6. 默认指数退避和最大 claim batch 有明确上限；时钟、退避函数、owner、batch 和 lease duration 均可确定性验证。
7. `CleanupPublished` 只按 published time 和有限 batch 删除 published 行，不删除 pending/leased 行。
8. Integration 并发测试使用两个 Dispatcher goroutine 同时 claim，确认没有重复 ID；随后覆盖错误 owner、失败退避、未到期排他、过期回收、陈旧 owner 失效和 published-only cleanup。

### 2.5 文档与版本

1. README 新增 v1 事件、拓扑、Outbox Repository 能力和本批非目标说明。
2. 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 同步更新为 `0.3.1`。
3. 明确保留边界：评论/点赞请求尚未写 Outbox，RabbitMQ 尚未发布业务消息，也没有通知表、Worker 或通知 API。

## 3. 实际变更文件

- `README.md`
- `VERSION`
- `backend/internal/bus/envelope.go`
- `backend/internal/bus/envelope_test.go`
- `backend/internal/outbox/model.go`
- `backend/internal/outbox/repository.go`
- `backend/internal/outbox/repository_test.go`
- `backend/internal/outbox/integration_test.go`
- `backend/internal/platform/rabbitmq_topology.go`
- `backend/internal/platform/rabbitmq_topology_test.go`
- `backend/migrations/000002_business_outbox.up.sql`
- `backend/migrations/000002_business_outbox.down.sql`
- `backend/migrations/embed_test.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `dev/logs/Phase-02/Phase-02-01-事务Outbox与消息契约基础.md`

未修改评论/点赞业务成功路径、Redis 缓存、Frontend 源码、Bash lifecycle 脚本、冻结的 `scripts/*.ps1`、用户 `.env` 或用户日常 MySQL 数据库。

## 4. 验证命令与结果

### 4.1 Backend 默认门禁

实际执行：

```bash
cd backend
test -z "$(gofmt -l .)"
go test -count=1 ./...
go vet ./...
go test -count=1 -race ./...
```

结果：全部通过。新增 bus、topology、migration 和 outbox 单元测试，以及现有 Phase 1 Backend 测试均通过；race detector 未发现数据竞争。

### 4.2 真实 MySQL integration

在当前本地 Compose MySQL/Redis 服务上只创建并使用明确归属的 `gopulse_integration` 数据库、`gopulse_integration` 用户和 Redis DB 15，配置 `INTEGRATION_TESTS=1`、`APP_ENV=test` 后实际执行：

```bash
cd backend
go run ./cmd/migrate up
go test -count=1 -tags=integration ./...
```

结果：全部通过。覆盖 Phase 1 既有认证、帖子、评论、点赞、HTTP 与 Redis integration，以及新增 Outbox Schema、事务插入、并发 claim、租约回收、mark-published、失败退避和 cleanup。

对同一个明确归属的隔离数据库实际执行最终迁移往返：

```bash
cd backend
go run ./cmd/migrate down
# 查询 information_schema，确认 business_outbox 不存在且 users/posts/comments/post_likes 仍存在
go run ./cmd/migrate up
# 再次确认 business_outbox 与 Phase 1 四张事实表同时存在
```

结果：通过。Down 只删除 `business_outbox`，Phase 1 四张事实表和 `schema_migrations` 保留；Up 恢复最终 Outbox Schema、索引和约束。

### 4.3 Frontend 元数据回归

实际执行：

```bash
cd frontend
npm test -- --run
npm run typecheck
npm run build
```

结果：通过。7 个 Vitest 文件、39 项测试全部通过，TypeScript typecheck 和 Vite production build 成功，npm 输出版本为 `gopulse-frontend@0.3.1`。未执行浏览器 E2E，因为本批没有改变 HTTP 或 Frontend 业务契约。

### 4.4 仓库治理与版本

实际执行：

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.3.1 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check
```

结果：通过。15 项治理测试全部通过；版本元数据一致；分支名、Phase 2 权威分配和目标版本一致；Bash 语法与 Compose 配置有效；无 whitespace error。

## 5. 实施偏差与问题处理

### 5.1 Integration 依赖复用

开始验证时仓库日常 Compose 服务已经运行，因此没有新建第二套竞争 Docker daemon 或覆盖日常 volume。本批仅在现有 MySQL 服务内创建白名单名称的独立 `gopulse_integration` 数据库和独立数据库用户，并只使用 Redis DB 15。验证结束后删除本批创建的数据库和用户；未停止或清理用户日常 `gopulse` 容器、数据库和 volume。

### 5.2 Down/Up 验收方式

CI 的 integration 用户只拥有 `gopulse_integration.*` 权限，不适合在并行 package 测试中创建额外数据库并切换全局 migration 状态。因此迁移结构由自动测试覆盖，真实 down/up 往返作为串行验收命令在明确归属的隔离数据库执行。该方式避免在并行 integration 期间临时删除共享表。

### 5.3 与原方案的功能差异

没有功能范围偏离。RabbitMQ 拓扑使用 fake/channel abstraction 验证，符合本批“不建立常驻连接循环”的边界；未提前接入评论、点赞、Dispatcher、Worker 或通知功能。除版本元数据验证外，没有扩大 Frontend 功能测试范围到浏览器 E2E。

## 6. 已知限制与后续项

- 当前评论和首次点赞仍只写 Phase 1 核心事实，不创建 Outbox 行；原子事实 + Outbox 事务属于 Phase-02-02。
- 当前没有 Backend Dispatcher，不进行 persistent publish、mandatory return 或 Publisher Confirm；这些属于 Phase-02-02。
- 当前没有 Business Worker、notifications 表、消费幂等、retry/dead 发布行为或通知 API；这些属于后续批次。
- Retry queue 的 TTL/DLX、dead exchange/queue 目前是声明契约；实际 retry count、消息转发、ack/nack 和最终死信行为要由 Phase-02-03 Worker 实现并做真实故障验收。
- 本批交付的是至少一次语义的持久化与消息契约基础，不构成异步通知完成，也不声明 exactly once。

## 7. Phase-02-02 交接

Phase-02-02 可直接使用：

- `bus.NewCommentCreated`、`bus.NewPostLiked`、严格 `Encode` / `Decode`、routing key 与 AMQP metadata 规则。
- `outbox.Repository.Insert` 的 `Executor` 边界，用于在评论/首次点赞核心事实事务内原子插入事件。
- `Claim`、`MarkPublished`、`ReleaseFailed`、过期 lease 回收和 published cleanup 状态机。
- `DeclareBusinessTopology` 及集中式 exchange、queue、routing key 常量。
- 真实 MySQL 并发租约、失败退避和迁移往返的通过证据。
