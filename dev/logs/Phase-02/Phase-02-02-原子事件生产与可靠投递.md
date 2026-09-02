# Phase-02-02：原子事件生产与可靠投递开发记录

## 1. 执行信息

- 日期：2026-09-02
- 分支：`develop/0.3.2`
- 目标版本：`0.3.2`
- 起始基线：`origin/main` 的 `6dcb6ab`（Phase-02-01 已合入）
- 依据：`dev/imple/Phase-02/Phase-02-02-原子事件生产与可靠投递.md`
- 执行环境：WSL2 Linux、Bash、Go

## 2. 实际完成工作

### 2.1 评论与点赞原子事件生产

1. 为 Outbox 暴露最小 `Writer` 事务写入边界，使业务 Repository 可以把活动 `*sql.Tx` 作为 executor 传入，而不把事务句柄泄漏到 Handler。
2. 评论创建改为在 `READ COMMITTED` MySQL 事务中锁定帖子、写入评论，并在评论者不是帖子作者时写入唯一 `comment.created` Outbox 事件；任一步失败都会回滚事务。
3. 首次点赞改为在同一类事务中锁定帖子、插入点赞，并在点赞者不是帖子作者时写入 `post.liked` Outbox 事件。
4. 重复点赞继续返回既有幂等结果，不创建重复事件；自评论、自赞和取消点赞不创建通知事件。
5. 现有 Redis post-detail invalidation 仍在 MySQL 提交后执行，缓存错误不回滚业务事实或 Outbox。
6. 新增事务内帖子作者查询与 `FOR UPDATE` 锁定辅助函数，用于稳定确定事件 recipient。

### 2.2 Outbox Dispatcher

1. 新增受控 `Dispatcher`，按配置轮询并 claim 有界租约批次，逐条解码、发布和更新 Outbox 状态。
2. 每条发布使用独立超时；成功后才调用 `MarkPublished`，失败则使用有限 failure code 调用 `ReleaseFailed`。
3. 保留“RabbitMQ 已确认、MySQL 尚未标记 published 时进程崩溃”可能导致重复投递的至少一次边界，不采用可能永久丢消息的预先标记顺序。
4. context 取消后停止 claim 新记录；无法确定结果的活动租约由数据库 lease 机制恢复。
5. 新增 Dispatcher 的选项校验、批次处理、失败分类、租约丢失、取消和敏感错误收敛测试代码。

### 2.3 RabbitMQ Publisher

1. 新增懒连接 RabbitMQ Publisher；Backend 启动时不主动连接 Broker，因此 Broker 暂不可用不会阻止 HTTP Server 初始化。
2. 首次发布时建立连接与 Channel、声明 Phase-02-01 的 durable topology，并启用 publisher confirms、mandatory returns 和 close notifications。
3. 发布消息使用 persistent delivery mode、v1 routing key、`message_id=event_id`、JSON content type、事件 type 和 UTC timestamp。
4. 只有 ack 且未收到匹配的 unroutable return 才返回成功；nack、return、超时、连接或 Channel 关闭均映射为有限 Outbox failure code。
5. 连接异常会关闭失效状态，并按有上限且带抖动的退避重新建立连接；Publisher 的公开错误不包含 AMQP URL 或底层凭据。

### 2.4 Backend 生命周期与配置

1. Backend 初始化 Outbox Repository、RabbitMQ Publisher 和 Dispatcher，并把带 Outbox 的评论、点赞 Repository 注入现有 Service。
2. Dispatcher 与 HTTP Server 共享信号生命周期；HTTP Server 结束后取消 Dispatcher，并在既有 shutdown timeout 内等待退出。
3. 新增 `OUTBOX_POLL_INTERVAL`、`OUTBOX_CLAIM_BATCH`、`OUTBOX_LEASE_DURATION`、`OUTBOX_PUBLISH_TIMEOUT` 和 `OUTBOX_RETRY_DELAY` 的默认值、边界校验和配置测试代码。
4. 更新 `.env.example` 与 README，说明原子事件、可靠投递、配置项和至少一次重复边界。
5. 将根版本和 Frontend npm 元数据统一更新为 `0.3.2`。

## 3. 变更文件

- `.env.example`
- `README.md`
- `VERSION`
- `backend/cmd/server/main.go`
- `backend/internal/comment/repository.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/like/repository.go`
- `backend/internal/outbox/dispatcher.go`
- `backend/internal/outbox/dispatcher_test.go`
- `backend/internal/outbox/model.go`
- `backend/internal/outbox/repository.go`
- `backend/internal/platform/rabbitmq_publisher.go`
- `backend/internal/post/repository.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `dev/logs/Phase-02/Phase-02-02-原子事件生产与可靠投递.md`

## 4. 实际验证

本批收口时按用户明确要求停止继续扩大测试范围，没有再执行实施方案中的 Backend 全量、race、integration、RabbitMQ 故障矩阵、Frontend 或仓库治理测试。未把这些未执行项记录为通过。

实际执行：

```bash
gofmt -w <本批变更的 Go 文件>
git diff --check
```

结果：Go 文件已格式化；`git diff --check` 通过，无 whitespace error。

## 5. 实施偏差与已知限制

1. 本次收口没有继续执行原计划第 8 节的完整验证矩阵，这是依照用户要求立即结束测试阶段的明确范围缩减；因此本记录不提供全量单元、race、真实 MySQL/RabbitMQ 或故障恢复通过证据。
2. 当前交付仅把评论和首次非自赞事件可靠送入 RabbitMQ 主队列；没有 Business Worker、通知表、消费幂等、retry/dead 消费处理或通知 API。
3. 至少一次投递允许同一 `event_id` 重复出现，Phase-02-03 Consumer 必须以事件 ID 实现幂等。
4. RabbitMQ 不可用时业务 MySQL 事务仍可成功，待投递记录由 Dispatcher 后续重试；`/ready` 仍会按既有契约报告 RabbitMQ 不可用。

## 6. Phase-02-03 交接

Phase-02-03 可基于以下能力继续实现 Business Worker：

- 评论和首次非自赞点赞的 v1 Outbox 事件已与业务事实原子提交。
- Backend Dispatcher 已按 persistent、mandatory 和 publisher confirm 规则投递到 durable 主队列。
- Outbox 的 failure code、退避、租约恢复和至少一次重复边界已接入生产路径。
- Consumer 必须把 `event_id` 作为幂等键，并负责 retry/dead 与通知持久化闭环。

## 7. 后续修复：Integration 测试包循环依赖

- 日期：2026-09-02
- 触发原因：`develop/0.3.2` 和后续 `update` 推送的 GitHub Integration 门禁均在编译 `internal/outbox` 时失败，自动 PR 创建步骤因此被跳过。
- 根因：Phase-02-02 新增的 RabbitMQ Publisher 形成 `platform -> outbox` 生产依赖，而原有 `outbox/integration_test.go` 仍使用包内测试并导入 `platform`，在 integration build tag 下形成 `outbox -> platform -> outbox` 测试编译环。
- 实际修复：仅把该 integration test 改为外部包 `outbox_test`，并通过 `outbox` 的公开 API 引用 Repository、Record、状态和错误；生产代码、Schema、版本及运行语义均未改变。
- 本地验证：`git diff --check` 通过，并检查测试文件不再存在未限定的 `outbox` 标识符。当前 macOS 规划环境没有 Go 工具链，未把本地 Go 编译记录为通过。
- 远程验证：首次修复推送触发的 GitHub Actions 运行 `33596299209` 中，Backend test/vet/race、Frontend、脚本治理和真实 MySQL/Redis Integration 全部通过，自动创建 PR #33 并启用 auto-merge。
- 版本保持 `0.3.2`，本修复属于 Phase-02-02 的同批次缺陷修复，不占用 Phase-02-03 的 `0.3.3`。
