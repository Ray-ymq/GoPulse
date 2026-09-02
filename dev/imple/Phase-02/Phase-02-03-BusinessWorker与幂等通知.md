# Phase 2-03：Business Worker 与幂等通知实施方案

> 执行序号：3 / 5  
> 前置批次：Phase 2-01 至 Phase 2-02 已完成并通过验收  
> 总方案来源：[Phase-02-总实施方案.md](Phase-02-总实施方案.md)

## 1. 批次目标

新增独立 Business Worker，消费 Phase-02-02 投递的业务事件，以至少一次 + 幂等副作用语义生成 MySQL 通知，并建立有限延迟重试、死信、断线恢复和优雅关闭能力。

本批完成 Backend → RabbitMQ → Worker → MySQL 的服务端异步闭环，但尚不提供公共 Notification API 或 Frontend 页面。

## 2. 前置条件

- Phase-02-02 已合入远程 `main`，根版本与前一批目标一致。
- 两类 v1 消息、主队列和重复投递边界已有真实 RabbitMQ 证据。
- Outbox backlog 可在 Broker 恢复后补投。
- 已从远程最新 `main` 创建本批权威开发分支，工作树安全。

## 3. 实施范围

### 3.1 Notification Migration 与 Repository

新增顺序双向迁移创建 `notifications`：

- 唯一 `source_event_id`。
- `type`、recipient/actor/post ID、可空 comment ID。
- `created_at` 与可空 `read_at`。
- 现有 users/posts/comments 外键和接收者分页索引。

Repository 在事务内幂等插入：唯一冲突视为“已处理”，其他 SQL 错误分类为临时处理失败。查询/已读接口留到下一批接入，但本批可提供内部 Repository 方法和 integration 测试。

### 3.2 独立 Worker 进程

- 新增 `backend/cmd/business-worker`，只加载 MySQL、RabbitMQ 和 Worker 所需配置。
- 启动时声明与 Producer 相同的版本化 topology，设置有界 prefetch，使用 manual ack。
- 严格解码消息，验证 AMQP properties 与 Envelope 一致。
- 根据 `comment.created`/`post.liked` 写入对应通知；actor=recipient 时防御性忽略并 ack。
- MySQL 通知事务提交后 ack；唯一 event 已存在时直接 ack。
- Worker 启动不需要 Redis、HTTP、JWT 或 Cookie 配置。

### 3.3 重试与死信

- JSON 超限/非法、未知 schema/type、ID/属性不一致等永久错误直接投递死信并 ack 原消息。
- 临时 MySQL/连接错误按配置的最大次数进入 retry exchange/queue，使用延迟后回到主队列。
- 重试消息递增受校验 header，保留原 message ID/type/routing key；成功 confirm 重试消息后才 ack 原消息。
- 达到最大次数后进入 dead exchange/queue；成功 confirm 死信后才 ack 原消息。
- 重试/死信 publish 失败时 nack/requeue 原消息，防止静默丢失；同时依赖有界退避避免热循环。
- 错误日志只输出 event ID、事件类型、attempt 和归一化原因，不输出 Payload/凭据。

### 3.4 连接恢复与关闭

- RabbitMQ connection/channel 或 delivery stream 关闭后，Worker 使用有上限带抖动退避重连并重新声明 topology。
- MySQL 暂时不可用时不确认未持久化消息。
- SIGINT/SIGTERM 停止拉取新消息，在有界时间处理当前 delivery；超时后关闭 channel，让 RabbitMQ 重新投递未 ack 消息。
- 不依赖进程内内存记录已处理状态。

## 4. 实施边界与非目标

- 不在 Worker 内调用 Backend HTTP API。
- 不从 RabbitMQ Payload 复制用户名或正文到 notifications。
- 不提供通知查询/已读 HTTP 路由或 Frontend UI。
- 不实现通知删除、偏好、聚合、撤回、邮件/短信/推送。
- 不声明 exactly once，不用 Redis 做消费去重。
- 本批不修改冻结 PowerShell 生命周期。

## 5. 目标文件与交付物

预计涉及：

```text
backend/migrations/000003_*.up.sql
backend/migrations/000003_*.down.sql
backend/cmd/business-worker/
backend/internal/notification/
backend/internal/worker/
backend/internal/platform/rabbitmq*.go
backend/internal/config/
.env.example
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-02/Phase-02-03-BusinessWorker与幂等通知.md
```

## 6. 详细实施步骤

1. 编写 notifications up/down migration，并用迁移结构测试和一次真实 MySQL integration 证明必要索引、唯一约束与外键。
2. 实现通知幂等插入和内部查验 Repository。
3. 抽取 Worker command 专用配置边界，只覆盖默认值和代表性非法值，不枚举等价上下限组合。
4. 实现严格 delivery decoder 和永久/临时错误分类。
5. 实现通知 Processor 事务与唯一 source event 幂等处理。
6. 实现 manual ack、retry/dead publish confirm 和 header 递增。
7. 实现 topology/channel 恢复、consumer 重建和有界 shutdown。
8. 用 fake delivery/publisher 对成功、永久失败、临时失败和二次 publish 未 confirm 各保留一个代表性状态转换测试；不穷举 ack/nack/连接关闭排列。
9. 用真实 MySQL/RabbitMQ 验证两类正常消息、相同 event ID 重复、一次 retry 成功和一条永久坏消息进入 dead queue。
10. 对 Worker 做一次启动、信号关闭和未确认消息可重投 smoke；Broker/进程重启与完整故障恢复留到 Phase-02-05。
11. 更新 README、版本和本批实施记录。

## 7. 风险与控制

- **重复通知**：`source_event_id` 唯一约束是最终幂等边界，不依赖先查后插。
- **毒消息阻塞**：永久错误直接死信，合法后续消息仍能处理。
- **重试热循环**：使用 TTL retry queue、有界 attempt 和退避，不直接无限 nack/requeue。
- **二次发布丢失**：重试/死信收到 confirm 后才 ack 原消息。
- **关闭期间丢失**：未完成 delivery 保持 unacked，由 RabbitMQ 在 channel 关闭后重投。
- **配置耦合**：Worker 只要求实际使用的 MySQL/RabbitMQ 配置。

## 8. 验证命令与必要回归

本节是本批固定完成清单，不是继续扩展测试的起点。最终 diff 上各命令执行一次；上下文压缩后沿用已通过结果。只有命令失败或实现实际改变了额外共享边界，才增加有明确原因的定向回归。

```bash
(cd backend && go test ./cmd/business-worker ./internal/bus ./internal/config ./internal/notification ./internal/platform ./internal/worker ./migrations)
(cd backend && go vet ./cmd/business-worker ./internal/notification ./internal/worker)
(cd backend && go test -count=1 -race ./internal/notification ./internal/worker)
(cd backend && go test -count=1 -tags=integration ./internal/notification ./internal/worker)
(cd backend && go test ./internal/comment ./internal/like ./internal/outbox)
python3 scripts/ci/validate_versions.py
git diff --check
```

新增独立进程、MySQL Schema 和消息确认语义，因此保留真实 MySQL/RabbitMQ integration；只对 Worker/notification 运行 race/vet，并用 comment/like/outbox package 回归 Producer。公共 Frontend 契约未变，不运行 Frontend、Playwright 或完整 Phase 1 浏览器验收。RabbitMQ 容器重启、Worker 停止恢复、Backend 重启和 MySQL 故障矩阵统一由 Phase-02-05 执行，本批不得提前重复。

## 9. 验收标准

- 两类合法消息各生成正确接收者的一条通知；self event 不生成通知。
- 相同 event ID 重复投递后只存在一条 notification，重复消息被安全 ack；不要求额外穷举并发时序。
- 代表性临时错误进入 retry 后成功；达到上限的状态转换由定向测试证明会进入 dead queue。
- 非法/超限/未知事件直接死信，不阻塞后续合法消息。
- retry/dead publish 未 confirm 时原消息不会被 ack。
- SIGTERM 有界退出，未完成消息可重投，不泄漏凭据或 Payload。
- 第 8 节固定门禁全部通过；Phase-02-05 所属的重启/故障矩阵尚未执行且不阻塞本批完成。

## 10. 明确完成条件

本批提交、版本、实施记录和服务端异步验收完成；数据库中已有可验证通知结果，但用户尚不能通过公共 API/Frontend 查看。只有该边界被如实记录，Phase-02-03 才可标记完成。

## 11. 下一批交接

向 Phase-02-04 提供：

- 稳定 notifications Schema 和幂等事实。
- 接收者、动作用户、帖子和评论的关系。
- 可用于公共查询的排序键与已读字段。
