# Phase 2-01：事务 Outbox 与消息契约基础实施方案

> 执行序号：1 / 5  
> 前置基线：Phase 1 已在 `0.2.7` 完成并通过最终验收  
> 总方案来源：[Phase-02-总实施方案.md](Phase-02-总实施方案.md)

## 1. 批次目标

建立 Phase 2 后续批次共同依赖的稳定基础：版本化业务事件 Envelope、RabbitMQ 拓扑声明、Outbox 数据表及其 Repository/租约状态机。

本批不把事件接入评论或点赞请求，也不运行 Producer/Worker。完成状态只表示契约和持久化基础可用，不得描述为异步通知已经实现。

## 2. 前置条件

- 配置主远程 `main` 指向 Phase 1 最终提交，根 `VERSION` 为 `0.2.7`。
- 从远程最新 `main` 创建总方案分配的本批开发分支。
- Phase 1 迁移、integration 隔离和现有 AMQP readiness 测试可重复执行。
- 开始前确认工作树，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 事件契约

- 在独立业务消息包中定义 Envelope 和事件类型，不依赖 Gin Handler DTO。
- 第一版只允许 `comment.created` 和 `post.liked`，schema version 固定为 `1`。
- Envelope 包含唯一 event ID、事件类型、UTC 发生时间、actor/recipient/post ID，以及评论事件的 comment ID。
- Encoder/Decoder 使用严格 JSON、明确消息体上限和字段组合校验。
- Payload 不含评论/帖子正文、用户名、凭据、Cookie、JWT、AMQP URL 或底层错误。
- 定义 routing key 映射与 AMQP `message_id`、`content_type`、`type`、timestamp 规则。

### 3.2 RabbitMQ 拓扑契约

- 集中定义总方案中的 durable direct exchange、主队列、retry exchange/queue 和 dead exchange/queue。
- 声明函数必须幂等；参数不兼容时明确失败，不静默使用手工残留拓扑。
- 主队列绑定两种版本化 routing key；retry queue 通过 TTL/DLX 返回主交换机，dead queue 只接收最终失败消息。
- 为拓扑名称、durable/auto-delete/exclusive 参数和 binding 建立自动测试。
- 本批可用 fake/channel abstraction 验证声明，不建立常驻连接循环。

### 3.3 Outbox Migration

新增下一顺序双向迁移，创建 `business_outbox`：

- 自增内部主键和唯一 `event_id`。
- `event_type`、`schema_version`、JSON payload。
- `pending`、`leased`、`published` 等受约束状态。
- `available_at`、尝试次数、租约 owner/expiry、published time、有限错误摘要、创建/更新时间。
- pending 扫描、租约回收和 published 清理所需索引。

Down migration 只删除本批新增表，不触碰 Phase 1 四张事实表。真实 down 验收仅针对由测试创建且确认归属的隔离数据库。

### 3.4 Outbox Repository 与状态机

- 插入操作接受 `sql.DB` 或事务 executor，使后续业务 Repository 可在同一事务写入。
- Claim 使用有界 batch、稳定 ID 顺序和租约到期时间；不得持有数据库事务等待网络。
- Mark-published 只能由当前有效租约完成。
- Publish 失败更新有界错误摘要、递增尝试并设置退避后的 `available_at`。
- 过期 lease 可被重新领取；未过期 lease 不能被其他 Dispatcher 抢占。
- 清理只删除超过保留期的 published 行，永不删除 pending/leased 行。
- 时间、owner 和退避计算可注入，以便确定性测试。

## 4. 实施边界与非目标

- 不修改评论、点赞 Service/Repository 的现有成功路径。
- 不在 Backend 启动 Dispatcher，不连接 RabbitMQ 发布业务消息。
- 不新增 `notifications` 表或 Business Worker。
- 不把 Outbox 暴露为公共 HTTP API。
- 不实现通用事件框架、动态插件或任意拓扑配置系统。
- 不修改 Redis 缓存逻辑、Frontend 或冻结的 PowerShell 脚本。

## 5. 目标文件与交付物

预计涉及：

```text
backend/migrations/000002_*.up.sql
backend/migrations/000002_*.down.sql
backend/internal/bus/
backend/internal/outbox/
backend/internal/platform/rabbitmq*.go
backend/internal/**/*_test.go
README.md（仅必要的开发者契约说明）
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-02/Phase-02-01-事务Outbox与消息契约基础.md
```

实际目录可在保持依赖方向清晰的前提下调整，并在实施记录说明。

## 6. 详细实施步骤

1. 核对 Phase 1 最终迁移、MySQL 配置加载边界和 AMQP client 版本。
2. 定义事件 Envelope、构造器、严格编解码、大小上限和 routing key 映射。
3. 定义并测试第一版 RabbitMQ 拓扑声明。
4. 编写 Outbox up/down migration 和迁移结构测试。
5. 实现插入、claim、mark-published、release/retry、lease recovery 和 cleanup Repository。
6. 用 fake clock 和并发测试验证租约排他、过期回收和错误退避。
7. 在隔离 MySQL 从空库执行全部 up，验证表、索引、约束和 JSON 字段。
8. 只在可确认的临时数据库执行一次 down/up 往返，并确认 Phase 1 表不受影响。
9. 更新必要文档与本批实施记录，将产品版本更新到总方案分配值。

## 7. 风险与控制

- **契约过早泛化**：只支持两种明确事件，未知类型和版本拒绝处理。
- **Outbox 状态丢失**：状态转换使用条件更新和租约 owner，测试进程崩溃后的过期回收。
- **数据库热扫描**：使用 `(status, available_at, id)` 等明确索引和有限 batch。
- **错误信息泄漏**：只保存归一化、截断的错误类别/摘要，不写凭据或 Payload。
- **迁移破坏 Phase 1**：down 只操作新表，真实验收使用隔离数据库。

## 8. 验证命令与必要回归

至少执行：

```bash
cd backend && go test ./...
cd backend && go vet ./...
cd backend && go test -count=1 -race ./...
cd backend && go test -count=1 -tags=integration ./...
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
git diff --check
```

需要真实 MySQL integration，因为本批新增共享持久化 Schema 和并发租约；无需执行完整 Frontend 浏览器验收，因为公共 HTTP/Frontend 契约未改变。若修改共享配置或 lifecycle 脚本，再补对应 Bash/Frontend 回归。

## 9. 验收标准

- 合法的两类 v1 事件可稳定 round-trip，非法/超限/未知字段、类型或版本被拒绝。
- 拓扑声明参数与总方案一致，并可重复执行。
- 空库迁移后 Phase 1 表与 `business_outbox` 同时存在，索引和约束符合设计。
- 并发 claim 不返回同一未过期租约行；过期 lease 可恢复。
- mark-published、失败退避和清理只能作用于符合状态/owner/保留期的行。
- 默认单元、race、vet 和真实 MySQL integration 通过，现有 Phase 1 Repository 无回归。
- 根 `VERSION` 和 Frontend npm 元数据更新为本批目标版本，实施记录只陈述实际结果。

## 10. 明确完成条件

本批代码、迁移、测试和实施记录已提交；事件与 Outbox 契约可供下一批使用，但任何业务请求仍不会创建 Outbox 或 RabbitMQ 消息。只有满足该边界并通过本批验收，Phase-02-01 才可标记完成。

## 11. 下一批交接

向 Phase-02-02 提供：

- 冻结的 v1 Envelope 和 routing key。
- 可在业务事务内调用的 Outbox 插入接口。
- 可安全 claim/retry/mark 的 Repository。
- 幂等拓扑声明及其测试证据。
