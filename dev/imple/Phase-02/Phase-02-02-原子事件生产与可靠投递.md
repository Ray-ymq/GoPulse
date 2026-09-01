# Phase 2-02：原子事件生产与可靠投递实施方案

> 执行序号：2 / 5  
> 前置批次：Phase 2-01 已完成并通过验收  
> 总方案来源：[Phase-02-总实施方案.md](Phase-02-总实施方案.md)

## 1. 批次目标

把评论和首次点赞接入 Transactional Outbox，并在 Backend 内实现受控生命周期的 Dispatcher，将已提交事件可靠投递到 RabbitMQ durable 主队列。

本批结束时，核心写入与“待投递记录”不存在已知双写窗口，RabbitMQ 不可用不会回滚已提交 MySQL 事实；尚未实现 Business Worker 和通知结果。

## 2. 前置条件

- Phase-02-01 已合入远程 `main`，根版本等于前一批目标版本。
- v1 事件、拓扑和 Outbox Repository 已冻结并通过真实 MySQL 测试。
- Phase 1 评论/点赞幂等、Redis 缓存失效和 HTTP 契约测试仍通过。
- 已从远程最新 `main` 创建本批权威开发分支，工作树无待覆盖改动。

## 3. 实施范围

### 3.1 原子业务写入

- 评论 Repository 在同一 MySQL 事务内插入 comment 和 `comment.created` Outbox。
- Like Repository 只在真正插入新 `(post_id, user_id)` 行时写入 `post.liked` Outbox。
- 在事务内读取并锁定/确认帖子作者作为 recipient；actor 与 recipient 相同时不创建通知事件。
- 事务提交后才执行现有 Redis 最努力失效，Redis 失败不影响核心事实或 Outbox。
- 重复点赞保持 HTTP 204 幂等，不创建重复事件；取消点赞保持现有同步事实和缓存失效，不产生事件。
- API 响应形状和现有稳定错误码不改变。

### 3.2 Dispatcher

- Backend 启动受控 Dispatcher goroutine，定期 claim 有界 Outbox batch。
- RabbitMQ 连接和 Channel 懒建立；启动时 Broker 不可用不阻止 HTTP Server 启动。
- 每条消息使用 persistent delivery mode、v1 routing key、`message_id=event_id`、JSON content type、mandatory publish。
- 只有收到 publisher confirm ack 且没有 unroutable return 时标记 published。
- nack、return、publish timeout、连接断开或 context 取消均保留/释放 Outbox，按配置退避重试。
- 连接/Channel 异常使用有上限且带抖动的退避重建拓扑，不忙循环。
- Shutdown 停止 claim 新行，并在明确超时内结束当前 publish；未完成行由 lease 恢复。

### 3.3 并发与重复边界

- 多个 Backend 实例可通过 Outbox lease 并发运行，不同时持有同一有效 lease。
- 发布成功后、mark-published 前崩溃会造成重复投递，这是明确接受的至少一次边界。
- 不通过“先标已发布再 publish”避免重复，因为该顺序会造成永久丢失。
- Dispatcher 不删除 pending/leased 行；published 清理遵循 Phase-02-01 的保留期。

## 4. 实施边界与非目标

- 不直接从 Handler 或业务 Service 调用 RabbitMQ publish。
- 不把 RabbitMQ 错误返回给已经成功提交 MySQL 的评论/点赞请求。
- 不实现 Consumer、通知表、重试队列消费或死信处理。
- 不修改公共 HTTP 路径、Frontend 页面或 PowerShell 脚本。
- 不声称队列中的消息已经产生用户通知。

## 5. 目标文件与交付物

预计涉及：

```text
backend/internal/comment/
backend/internal/like/
backend/internal/post/
backend/internal/outbox/
backend/internal/platform/rabbitmq*.go
backend/internal/config/
backend/cmd/server/
.env.example
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-02/Phase-02-02-原子事件生产与可靠投递.md
```

## 6. 详细实施步骤

1. 为业务事务定义最小 executor/transaction runner，不把 `*sql.Tx` 泄漏到 Handler。
2. 调整评论创建，使 comment、recipient 和 Outbox 在同一事务内形成。
3. 调整点赞创建，区分新建与已存在；只有新建且非自赞时写 Outbox。
4. 保持 Redis invalidation 在事务提交后，回归已接受的 TTL 有界一致性语义。
5. 实现 AMQP publisher adapter、confirm/return 关联和 publish timeout。
6. 实现 Dispatcher claim/publish/mark/retry 循环与断线重连。
7. 将 Dispatcher 接入 Backend 启动、信号和有界关闭流程。
8. 增加配置默认值、上下限和敏感信息保护测试。
9. 在真实 MySQL/RabbitMQ 环境验证正常投递、Broker 停止期间积压和恢复补投。
10. 模拟 publish 成功后未 mark 的崩溃边界，确认会重复而不会丢失。
11. 更新 README、版本和本批实施记录。

## 7. 风险与控制

- **业务/Outbox 双写窗口**：只允许同一 MySQL 事务，不接受先提交事实后另起事务写 Outbox。
- **幂等点赞误发**：Repository 返回是否真正插入，重复请求测试断言 Outbox 数不增长。
- **AMQP confirm/return 竞态**：使用 message/event ID 关联，并覆盖 return、nack、超时和 Channel 关闭。
- **启动耦合**：Broker 不可用时 Dispatcher 后台退避，Backend 核心 API 仍可启动和写 MySQL。
- **Shutdown 丢租约**：有界等待后依赖 lease expiry 恢复，不强行标记 published。

## 8. 验证命令与必要回归

至少执行：

```bash
cd backend && go test ./...
cd backend && go vet ./...
cd backend && go test -count=1 -race ./...
cd backend && go test -count=1 -tags=integration ./...
scripts/verify-business.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
git diff --check
```

本批修改共享业务写路径、MySQL 事务和 Backend 生命周期，必须回归 Phase 1 评论/点赞 HTTP、真实 MySQL、Redis 失效/降级和 RabbitMQ integration。Frontend API 契约未改变时运行现有 Frontend 单元/类型检查即可，不重复完整浏览器故障矩阵；最终完整矩阵由 Phase-02-05 执行。

## 9. 验收标准

- 评论成功时 comment 与唯一 Outbox 同时存在；任一插入失败时二者均不存在。
- 首次非自赞产生一条 Outbox；重复点赞、自赞和取消点赞不产生通知事件。
- Redis 删除失败不回滚 comment/like/Outbox，也不改变 API 成功状态。
- RabbitMQ 正常时 Dispatcher 以持久化消息投递并在 confirm 后标记 published。
- RabbitMQ 停止时评论/点赞 API 成功、Outbox 保持未发布；恢复后自动补投。
- unroutable、nack、timeout 和连接断开不会错误标记 published。
- publish 后崩溃边界最多导致重复，不导致永久丢失。
- Backend 启停和信号测试无 goroutine 泄漏或无限等待，日志不泄漏 AMQP 凭据。
- 受影响的 Phase 1 测试、race、vet 和真实 integration 通过。

## 10. 明确完成条件

本批提交、版本、实施记录和验收均完成；RabbitMQ 主队列能收到可靠事件，但仓库仍没有 Consumer 和用户通知结果。只有明确保留该未完成边界，Phase-02-02 才可标记完成。

## 11. 下一批交接

向 Phase-02-03 提供：

- 已验证的 durable 主队列和两类 v1 消息。
- Broker 故障后可自动补投的 Outbox Dispatcher。
- 重复投递可能发生且必须由 Consumer 幂等处理的明确契约。
