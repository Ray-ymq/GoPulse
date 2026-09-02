# Phase-02-03：Business Worker 与幂等通知开发记录

## 1. 执行信息

- 日期：2026-09-02
- 分支：`develop/0.3.3`
- 目标版本：`0.3.3`
- 起始基线：`origin/main` 的 `1d6a591`（Phase-02-02 与最新规划调整已合入）
- 依据：`dev/imple/Phase-02/Phase-02-03-BusinessWorker与幂等通知.md`
- 执行环境：WSL2 Linux、Bash、Go、Docker Compose MySQL 8.4 与 RabbitMQ 3.13

## 2. 实际完成工作

### 2.1 Notification Schema 与幂等 Repository

1. 新增 `000003_notifications` 双向迁移，保存唯一 `source_event_id`、通知类型、recipient/actor/post/comment 关系、创建时间和可空已读时间。
2. 为 `source_event_id` 建立唯一约束，为接收者按 `created_at DESC, id DESC` 的查询建立复合索引，并为 users、posts、comments 建立 `RESTRICT` 外键。
3. 使用类型和 comment 形状检查约束限制 `comment.created` 与 `post.liked` 的持久化结构。
4. 新增 Notification Repository 和 Processor。插入在 `READ COMMITTED` 事务中完成；`source_event_id` 唯一冲突作为已经处理的成功结果，其他数据库错误返回临时处理失败。
5. self event 在持久化前被防御性忽略；Repository 提供按 source event ID 的内部查验方法，供本批 integration 验收使用。

### 2.2 独立 Business Worker

1. 新增 `backend/cmd/business-worker` 独立进程，只加载 MySQL、RabbitMQ、共享 retry TTL 和 `BUSINESS_WORKER_*` 配置，不依赖 HTTP、Redis、JWT 或 Cookie 设置。
2. Worker 启动时复用集中式 RabbitMQ topology，配置有界 prefetch、manual ack、publisher confirm 和 mandatory secondary publish。
3. Delivery decoder 严格校验消息体大小/JSON Envelope、routing key、message ID、content type、event type、persistent delivery mode 和 timestamp。
4. 合法评论与点赞事件在通知事务提交后 ack；重复 event ID 和 self event 安全 ack，进程内不保存消费幂等状态。

### 2.3 Retry、Dead Letter 与确认语义

1. 新增受校验的 `x-gopulse-attempt` header，临时处理失败按有限次数发布到 retry exchange；TTL 到期后沿原 routing key 回到主队列。
2. 永久坏消息、非法 attempt 和重试耗尽消息发布到 dead exchange；未知 routing key 使用显式 `invalid.v1` dead binding，避免永久坏消息因无路由而热循环。
3. Retry/dead 消息保留原 message ID、type、routing key 和 body，并使用 persistent、mandatory、confirm-gated 发布。
4. 只有 secondary publish 确认且可路由后才 ack 原消息；publish、confirm 或路由失败时 nack/requeue 原消息。
5. 修复了重新执行期间发现的 attempt header 高于配置上限时可能解引用空 decode error 的问题，并增加直接回归测试。
6. 日志只记录 event ID、event type、attempt 和归一化 reason，不输出 Payload、连接 URL 或凭据。

### 2.4 连接恢复与关闭

1. Runtime 在 connection、channel 或 delivery stream 关闭后使用有上限、带抖动的指数退避重新连接，并重新声明 topology、QoS 和 consumer。
2. SIGINT/SIGTERM 首先取消 consumer，停止拉取新消息；当前 delivery 在 shutdown timeout 内继续处理，超时后关闭 channel，使未 ack 消息由 RabbitMQ 重投。
3. 真实 RabbitMQ integration 使用阻塞 Processor 验证：信号关闭时正在处理且尚未持久化的消息没有被确认，重新启动 consumer 后能够重投并生成唯一通知。
4. 独立编译并启动真实 `business-worker` 二进制，确认收到 SIGTERM 后正常退出。

### 2.5 配置、文档与版本

1. `.env.example` 增加 Worker prefetch、最大重试、发布超时、关闭超时和重连上下限默认值；`OUTBOX_RETRY_DELAY` 明确作为 Producer/Worker 共享 retry queue TTL。
2. README 增加独立 Worker 启动方式、通知持久化、至少一次 + 幂等副作用、retry/dead 和关闭边界说明，并明确本批不提供公共 Notification API/UI。
3. 根 `VERSION` 与 Frontend npm 元数据统一更新为 `0.3.3`。

## 3. 变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `backend/cmd/business-worker/main.go`
- `backend/internal/config/worker.go`
- `backend/internal/config/worker_test.go`
- `backend/internal/notification/integration_test.go`
- `backend/internal/notification/model.go`
- `backend/internal/notification/processor.go`
- `backend/internal/notification/repository.go`
- `backend/internal/notification/repository_test.go`
- `backend/internal/platform/rabbitmq_topology.go`
- `backend/internal/platform/rabbitmq_topology_test.go`
- `backend/internal/worker/decoder.go`
- `backend/internal/worker/handler.go`
- `backend/internal/worker/handler_test.go`
- `backend/internal/worker/integration_test.go`
- `backend/internal/worker/runtime.go`
- `backend/migrations/000003_notifications.up.sql`
- `backend/migrations/000003_notifications.down.sql`
- `backend/migrations/embed_test.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `dev/logs/Phase-02/Phase-02-03-BusinessWorker与幂等通知.md`

## 4. 实际验证

### 4.1 定向单元测试

实际执行：

```bash
(cd backend && go test ./cmd/business-worker ./internal/bus ./internal/config ./internal/notification ./internal/platform ./internal/worker ./migrations)
```

结果：通过。覆盖 Worker 专用配置、严格 delivery 校验、manual ack、幂等/自事件、有限 retry、永久 dead letter、secondary publish 未确认时 requeue、topology 与迁移结构。

### 4.2 Vet 与 Race

实际执行：

```bash
(cd backend && go vet ./cmd/business-worker ./internal/notification ./internal/worker)
(cd backend && go test -count=1 -race ./internal/notification ./internal/worker)
```

结果：全部通过；race detector 未发现数据竞争。

### 4.3 真实 MySQL/RabbitMQ Integration

在明确归属的 `gopulse-phase0203-integration` Compose 项目上，使用白名单数据库 `gopulse_integration`、用户 `gopulse_integration`、Redis DB 15 和动态 loopback 端口，实际执行：

```bash
(
  export INTEGRATION_TESTS=1 APP_ENV=test
  export MYSQL_HOST=127.0.0.1 MYSQL_DATABASE=gopulse_integration MYSQL_USER=gopulse_integration
  export REDIS_HOST=127.0.0.1 REDIS_DB=15
  export RABBITMQ_URL='<隔离 RabbitMQ URL>'
  cd backend
  go test -count=1 -tags=integration ./internal/notification ./internal/worker
)
```

结果：通过。Notification integration 用真实 `SHOW CREATE TABLE` 查验唯一键、接收者分页索引和四个外键，并验证两类通知、自事件及并发重复 event ID 只产生一条记录。Worker integration 验证两类正常消息、重复投递、永久坏消息进入 dead queue、临时失败两次后成功、达到重试上限进入 dead queue，以及关闭超时后的未确认消息重投。

### 4.4 独立进程信号 Smoke

实际执行等价步骤：

```bash
(cd backend && go build -o /tmp/gopulse-business-worker-phase0203 ./cmd/business-worker)
/tmp/gopulse-business-worker-phase0203
kill -TERM <worker-pid>
```

结果：真实二进制报告启动成功，收到 SIGTERM 后在 8 秒检查窗口内以状态 0 正常退出；临时二进制随后删除。

### 4.5 Producer 必要回归与版本

实际执行：

```bash
(cd backend && go test ./internal/comment ./internal/like ./internal/outbox)
python3 scripts/ci/validate_versions.py
```

结果：全部通过；评论、点赞和 Outbox Producer 定向回归无异常，根版本与 Frontend npm 元数据一致为 `0.3.3`。

### 4.6 Diff 检查

实际执行：

```bash
git diff --check
git diff --cached --check
```

结果：全部通过，无 unstaged 或 staged whitespace error。

## 5. 实施偏差与问题处理

1. 本次为重新执行。开始时从最新 `origin/main` 重置本地 `develop/0.3.3`，再恢复先前保存的本批 WIP；唯一冲突位于 `backend/internal/outbox/integration_test.go`。该 WIP 对 Outbox integration 的连接 helper 修改不属于本批验收范围，因此保留最新 `main` 版本，没有把无关测试改动带入提交。
2. 原 WIP 的 Notification 并发重复 integration 使用 24 个并发写入。依据执行效率规则和本批“代表性重复、不穷举并发时序”边界，收敛为两个并发重复写入，仍直接证明唯一 source event 幂等边界。
3. 实施中定向测试发现并修复 attempt 高于配置最大重试次数时的 nil error 解引用风险；只增加该已观察缺陷的代表性回归，没有扩展为 attempt 类型/数值全排列。
4. 没有运行 Frontend、Playwright、完整 Phase 1 浏览器验收或 Backend 全量测试，因为公共 HTTP/Frontend 契约未改变，实施方案明确限定为 Worker/notification 与 Producer 必要回归。

## 6. 已知限制与后续项

- 当前没有公共 Notification 查询、分页或已读 API，也没有 Frontend 通知页面；这些属于 Phase-02-04。
- 本批只执行实施方案要求的一条 shutdown/unacked redelivery smoke；RabbitMQ 容器重启、Backend 重启、MySQL 故障和完整进程恢复矩阵留给 Phase-02-05。
- 投递语义仍是至少一次；RabbitMQ confirm 后到原 delivery ack 前的进程失败可能导致重复消费，最终由 `notifications.source_event_id` 唯一约束吸收。
- Retry 使用共享固定 TTL，不提供按 attempt 单独递增的延迟、通知聚合、撤回、偏好或外部推送。
- Business Worker 目前需要单独启动；统一 Bash 生命周期编排属于 Phase-02-05。

## 7. Phase-02-04 交接

Phase-02-04 可直接复用：

- `notifications` 的稳定关系、唯一 source event、接收者排序索引和 `read_at` 字段。
- `notification.Repository.FindBySourceEventID` 已证明的内部持久化形状；公共查询与已读操作应继续限制为当前接收者。
- Worker 已产生 `comment.created` 与 `post.liked` 两类真实通知，Frontend 和 HTTP 层无需接触 RabbitMQ。
- 至少一次 + MySQL 唯一约束的幂等边界，以及 retry/dead queue 的可诊断事实。
