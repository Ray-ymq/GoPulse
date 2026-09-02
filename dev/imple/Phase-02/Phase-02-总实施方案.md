# Phase 2：业务异步化总实施方案

## 1. 实施目标

在 Phase 1 已完成的同步业务闭环上引入 RabbitMQ 业务异步链路，使评论和首次点赞在 MySQL 核心事实提交后，能够可靠地产生面向帖子作者的异步通知。

本阶段采用 MySQL Transactional Outbox 保存“待投递事实”，由 Backend 后台 Dispatcher 投递持久化 RabbitMQ 消息，再由独立 Business Worker 以至少一次语义消费并幂等写入 MySQL 通知表。Frontend 通过 Backend API 查询和处理通知，不直接访问 RabbitMQ 或 MySQL。

阶段完成后的主链路为：

```text
评论 / 首次点赞 HTTP 请求
  → MySQL 事务：核心事实 + Outbox
  → HTTP 成功响应
  → Backend Outbox Dispatcher
  → RabbitMQ durable business queue
  → Business Worker
  → MySQL notifications
  → Backend Notification API
  → Frontend 通知页
```

该链路必须保持 MySQL 是核心业务和通知结果的最终事实来源；RabbitMQ 只负责异步任务传输，不替代数据库保存评论、点赞或通知结果。

## 2. 当前真实基线

Phase 2 以已合入配置主远程 `main` 的 PR #29 为输入基线：

- 基线提交：`0544960ce90a2f2dd34ef5053fed4e7a944e60b3`。
- 根 `VERSION`：`0.2.7`。
- Phase 1 七个批次、实施记录和最终 Review 整改均已完成，Phase 1 总实施方案已给出通过结论。
- Gin Backend 已提供注册、登录、帖子、评论、点赞和取消点赞 API；Vue Frontend 已能完成对应浏览器闭环。
- `comments` 与 `post_likes` 直接通过各自 Repository 执行单条 SQL；当前没有跨核心事实与消息记录的事务边界，也没有 Outbox 表。
- MySQL 现有迁移只有 `000001_phase1_schema`，包含 `users`、`posts`、`comments` 和 `post_likes`。
- Redis 只保存可降级的帖子详情公共投影；缓存失败不会改变 MySQL 成功语义，并存在已记录的 TTL 有界陈旧限制。
- `deploy/compose.yaml` 已提供带命名卷的 RabbitMQ 3.13.3 management 服务，Backend 配置已要求合法 `RABBITMQ_URL`。
- `backend/internal/platform/rabbitmq.go` 当前只为 `/ready` 建立短连接检查；仓库尚无业务交换机、队列、Producer、Consumer、重试、死信或独立 Worker 进程。
- Backend 已直接依赖 `github.com/rabbitmq/amqp091-go v1.14.0`，无需为了 Phase 2 更换 AMQP 客户端。
- `scripts/dev.sh`、`scripts/down.sh` 和 `scripts/verify-business.sh` 只管理 Backend、Frontend 与基础设施；尚未管理 Business Worker，也未验收消费者暂停、消息积压、Broker 故障和恢复。
- `scripts/*.ps1` 冻结在 `0.2.1` 能力基线，不属于 Phase 2 修改或验收范围。

上述内容只描述仓库中已存在且已验证的事实。Outbox、通知、Worker 和可靠投递均是本阶段待实现能力。

## 3. 前置条件与仓库约束

### 3.1 前置条件

- PR #29 已以 `develop/0.2.7` → `main` 合并，远程 `main` 根 `VERSION` 为 `0.2.7`。
- 每批开始前 fetch 配置的主远程，并确认前置批次已合入该远程 `main`。
- 每批从远程最新 `main` 创建本方案分配的独立 `develop/x.x.x` 分支。
- 实施与应用验收在 Windows 宿主机的 WSL2 Linux 环境进行，活动仓库位于 WSL Linux 文件系统。
- 使用 Bash 作为唯一维护的本地生命周期和验收入口；不修改冻结的 PowerShell 脚本。
- 开始前记录 Git 状态，不覆盖、暂存或提交用户及其他任务的改动。

### 3.2 版本与分支权威分配

Phase 2 使用 `0.3.x` 版本线，`0.3.0` 只作为阶段基线，不创建空批次。下表是本阶段批次、执行顺序、目标版本和开发分支的唯一权威分配：

| 执行批次 | 目标版本 | 开发分支 | 当前状态 |
| --- | --- | --- | --- |
| Phase-02-01 | `0.3.1` | `develop/0.3.1` | 已完成（PR #31，`6dcb6ab`） |
| Phase-02-02 | `0.3.2` | `develop/0.3.2` | 已完成（PR #32，`e96414a`；验证范围见实施记录） |
| Phase-02-03 | `0.3.3` | `develop/0.3.3` | 已完成（`e1e7639`；CI 补充 PR #36，`4020d6b`） |
| Phase-02-04 | `0.3.4` | `develop/0.3.4` | 已完成（PR #37，`f8eed59`） |
| Phase-02-05 | `0.3.5` | `develop/0.3.5` | 本地完成（阶段验收通过，待 PR 合入与远程门禁） |

执行规则：

- 每批全部提交共享该批目标版本；批次完成时更新根 `VERSION` 和 Frontend npm 根包元数据，并通过现有版本一致性校验。
- 批次完成或已打开 Pull Request 后，不继续在该分支执行下一批。
- 如实施前改变批次数量或顺序，先更新本表并重算尚未创建的分支；已推送分支不得静默改名或重新编号。
- 每批完成前创建对应 `dev/logs/Phase-02/Phase-02-XX-*.md`，只记录实际执行内容和验证结果。

## 4. 阶段范围与非目标

### 4.1 本阶段实现

- 版本化业务事件 Envelope，以及 `comment.created`、`post.liked` 两类事件契约。
- MySQL Transactional Outbox、投递状态、租约回收和清理边界。
- 评论与“首次成功建立点赞事实”的核心写入和 Outbox 原子事务。
- Backend 内独立生命周期的 Outbox Dispatcher，使用持久化消息、Publisher Confirm 和 mandatory return。
- RabbitMQ durable exchange、主队列、延迟重试队列和死信队列。
- 独立 `business-worker` 进程、手动 ack、有限重试、断线恢复和幂等消费。
- MySQL 通知持久化、认证用户的通知分页查询和幂等已读操作。
- Frontend 通知页面，使用户可观察异步动作完成与延迟。
- WSL2/Bash 生命周期、集成测试和隔离业务验收对 Worker 与 RabbitMQ 故障矩阵的覆盖。

### 4.2 明确不做

- 不将 RabbitMQ 消息、队列深度或 Outbox 行作为评论、点赞或通知查询的最终事实。
- 不为重复点赞生成重复事件；`post.unliked` 本阶段保持同步事实，不产生用户通知事件。
- 不实现评论回复、关注、私信、邮件、短信、推送或通知偏好。
- 不实现 WebSocket、SSE、浏览器后台轮询或实时未读角标；通知页使用显式加载/刷新。
- 不实现任意事件编排、Saga、跨服务分布式事务或 exactly-once 声明。
- 不引入 Kafka；Kafka 仍只用于后续可观测数据链路。
- 不建设 RabbitMQ 集群、镜像/仲裁队列、多地域容灾、自动伸缩或大规模性能治理。
- 不提前实现 Phase 3 Elasticsearch 搜索或后续可观测组件。
- 不修改 `scripts/*.ps1`，不新增 Windows runner 或原生 Windows 验收。

## 5. 核心一致性与数据归属

### 5.1 写入顺序

评论和首次点赞必须在一个 MySQL 事务内同时写入核心事实与对应 Outbox 行：

```text
BEGIN
  INSERT comment / INSERT first like
  INSERT business_outbox
COMMIT
```

只有事务提交后 Dispatcher 才能投递 RabbitMQ。请求处理线程不得直接把 AMQP publish 成败作为业务成功条件。

- 核心事实或 Outbox 任一 SQL 失败：整个事务回滚，API 返回现有安全内部错误。
- MySQL 事务提交成功、RabbitMQ 不可用：API 仍成功，Outbox 保留待投递状态。
- RabbitMQ 恢复：Dispatcher 继续投递未完成 Outbox，不要求重放客户端请求。
- 发布成功但进程在标记 Outbox 前崩溃：允许重复投递，由 Worker 幂等约束吸收。

### 5.2 交付语义

阶段交付语义是“至少一次投递 + 幂等副作用”，不得表述为 exactly once：

- Outbox `event_id` 全局唯一且不可复用。
- RabbitMQ 消息使用 `event_id` 作为 `message_id`，使用稳定事件类型作为 routing key。
- Worker 只在 MySQL 通知事务成功后 ack。
- `notifications.source_event_id` 建立唯一约束；同一消息重复到达只得到一条通知。
- 无法解析、未知版本或违反契约的永久错误直接进入死信，不无限重试。
- 临时 MySQL/网络错误有限重试；超过上限进入死信并保留可诊断但不含凭据的原因摘要。

### 5.3 业务选择

- `comment.created`：评论者不是帖子作者时，为帖子作者生成通知。
- `post.liked`：只有新建点赞事实且点赞者不是帖子作者时，为帖子作者生成通知。
- 重复 `PUT like` 不产生新 Outbox；取消点赞不撤回历史通知，也不产生新通知。
- Payload 只携带处理所需稳定标识和时间，不复制评论正文、帖子正文、用户名、Cookie、JWT、密码或连接信息。

## 6. 消息契约与 RabbitMQ 拓扑

### 6.1 事件 Envelope

第一版 JSON Envelope 至少包含：

```json
{
  "schema_version": 1,
  "event_id": "uuid",
  "event_type": "comment.created",
  "occurred_at": "RFC3339Nano UTC",
  "actor_id": 123,
  "recipient_id": 456,
  "post_id": 789,
  "comment_id": 1011
}
```

`post.liked` 不包含 `comment_id`。Decoder 必须限制消息体大小、拒绝未知字段/多个 JSON 值、校验正整数 ID、UTC 时间、事件类型与 schema version。契约演进通过 `schema_version` 和版本化 routing key 完成，不依赖 Go 私有结构的隐式 JSON 形状。

### 6.2 第一版拓扑

拓扑名称集中定义并由 Producer/Worker 共用，避免多处字符串漂移：

```text
direct exchange: gopulse.business.v1
routing keys:    comment.created.v1, post.liked.v1
main queue:       gopulse.business-worker.v1
retry exchange:   gopulse.business.retry.v1
retry queue:      gopulse.business-worker.retry.v1
dead exchange:    gopulse.business.dead.v1
dead queue:       gopulse.business-worker.dead.v1
```

- Exchange、queue 和 binding 均持久化，业务消息使用 persistent delivery mode。
- Producer 使用 Publisher Confirm 和 mandatory publish；未路由 return 或 nack 不得标记 Outbox 已发布。
- Worker 设置有界 prefetch，使用手动 ack。
- 重试消息保留原始 `message_id`、event type 和 retry count；成功确认进入重试队列后才 ack 原消息。
- 拓扑参数变化必须显式版本化；不得依赖 RabbitMQ 管理界面中的手工配置。

## 7. MySQL 目标数据模型

### 7.1 `business_outbox`

至少保存：自增内部 ID、唯一 `event_id`、事件类型、schema version、JSON payload、状态、下次可用时间、投递尝试次数、租约持有者/到期时间、发布时间、有限错误摘要和创建/更新时间。

需要索引：

- 唯一 `event_id`。
- 按状态、`available_at`、ID 扫描待投递行。
- 按租约到期时间回收崩溃 Dispatcher 留下的 claim。

Dispatcher 使用有界批量和有期限租约，不能持有数据库事务等待网络 publish。已发布记录保留有限时间供审计，清理只删除明确达到保留期的 published 行，不删除 pending/leased 行。

### 7.2 `notifications`

至少保存：通知 ID、唯一 `source_event_id`、通知类型、接收者、动作用户、帖子 ID、可空评论 ID、创建时间和可空已读时间。

- 外键引用现有 `users`、`posts`、`comments`，删除策略保持 Phase 1 的 `RESTRICT` 事实边界。
- 以 `(recipient_id, created_at DESC, id DESC)` 支持稳定 keyset 分页。
- 不复制正文和用户名；查询时通过事实表装配公开摘要。
- 已读更新只能由通知接收者执行并保持幂等。

## 8. 进程、配置与故障边界

### 8.1 Backend Dispatcher

- 作为 Backend 内受控后台组件运行，但启动与 HTTP Server 解耦。
- RabbitMQ 在启动时不可用不阻止 Backend 提供依赖 MySQL 的核心 API；`/ready` 继续按既有契约把 RabbitMQ 标为 `down`。
- 连接/Channel 关闭后使用有上限且带抖动的退避重连，不忙循环、不记录完整 AMQP URL。
- 进程关闭时停止领取新行、等待有界的当前 publish、释放或等待租约过期，再关闭 AMQP/MySQL 资源。

### 8.2 Business Worker

- 新增 `backend/cmd/business-worker` 独立进程，只加载 MySQL 和 RabbitMQ 所需配置。
- Worker 可以在 Backend 停止时继续消费已入队消息，也可以在 RabbitMQ 暂时不可用时持续重连。
- SIGINT/SIGTERM 停止拉取新消息，并在有界时间内完成或重新入队当前未完成消息。
- Worker 不依赖 Redis、Gin、认证 Cookie 或 Frontend 配置。

### 8.3 配置

在现有 `RABBITMQ_URL` 基础上只增加有明确默认值和上下限的必要参数，例如：Outbox poll interval/batch/lease、publish timeout、Worker prefetch、retry delay/max attempts、shutdown timeout。拓扑名称默认固定并集中定义，第一版不把所有内部名称暴露为环境变量。

`.env.example` 只提供开发默认值；错误日志不得输出密码、完整 URL、Payload 原文或底层 SQL。

## 9. 跨批次依赖与执行顺序

```text
Phase-02-01 契约 + Outbox Schema
  ↓
Phase-02-02 原子事件生产 + Dispatcher
  ↓
Phase-02-03 Worker + 幂等通知持久化
  ↓
Phase-02-04 Notification API + Frontend
  ↓
Phase-02-05 生命周期 + 故障矩阵 + 阶段收口
```

- 02-01 冻结第一版消息和 Outbox 数据契约，后续批次不得以临时结构绕过。
- 02-02 交付可靠进入 RabbitMQ 的能力，但不把尚无 Worker 的队列积压描述为通知完成。
- 02-03 交付 Backend-to-Worker-to-MySQL 通知结果，02-04 才把结果暴露给用户。
- 02-05 只在前四批已合入并通过各自验收后扩展完整生命周期和故障恢复验收。

## 10. 批次摘要

### 10.1 [Phase-02-01：事务 Outbox 与消息契约基础](Phase-02-01-事务Outbox与消息契约基础.md)

- 定义业务事件 Envelope、允许事件类型、大小和兼容性边界。
- 新增 Outbox 迁移、Repository 和租约状态机。
- 建立 RabbitMQ 拓扑常量/声明契约与配置验证，但不接入评论或点赞请求。

### 10.2 [Phase-02-02：原子事件生产与可靠投递](Phase-02-02-原子事件生产与可靠投递.md)

- 评论和首次点赞在 MySQL 事务中原子写入核心事实与 Outbox。
- Backend Dispatcher 通过 persistent + confirm + mandatory 投递。
- 验证 RabbitMQ 不可用、发布确认丢失和进程重启时 Outbox 不丢失。

### 10.3 [Phase-02-03：Business Worker 与幂等通知](Phase-02-03-BusinessWorker与幂等通知.md)

- 新增通知 Schema 和独立 Worker。
- 实现手动 ack、幂等写入、有限延迟重试、死信和断线恢复。
- 验证重复消息只生成一条通知，永久坏消息不阻塞主队列。

### 10.4 [Phase-02-04：通知 API 与 Frontend 闭环](Phase-02-04-通知API与Frontend闭环.md)

- 提供认证通知分页查询和幂等已读 API。
- 新增通知页面、显式刷新和稳定错误/空状态。
- 验证接收者隔离、分页、已读权限与浏览器可观察链路。

### 10.5 [Phase-02-05：可靠性验收与阶段收口](Phase-02-05-可靠性验收与阶段收口.md)

- Bash 生命周期统一管理 Backend、Business Worker 和 Frontend。
- 扩展隔离验收覆盖消费者暂停/恢复、Broker 停止/恢复、重复投递、重试/死信和重启恢复。
- 更新 CI、README、实施记录和阶段状态，完成 Phase 2 里程碑判定。

## 11. 测试与必要回归范围

### 11.1 Phase-02-03 起的执行预算与停止规则

- 每批先从详细方案提取“本批新增行为 → 最低有效测试层 → 固定完成门禁”的最小清单；方案以外的代码审计、依赖审计、覆盖率提升和假设性加固不属于任务。
- 初始阅读限定为直接受影响的项目代码、已有测试和公开接口，并在 10 分钟内进入实现。没有具体编译、运行或必需测试失败时，不阅读第三方依赖源码。
- 新增测试必须能映射到本批某条验收标准、已复现缺陷，或本批实际改变的安全/持久化/公共契约风险。禁止为未发生的组合补穷举边界、为覆盖率补测试，或在 unit/integration/E2E 多层重复证明同一事实。
- 实现中先运行受影响 package 的定向测试；最终 diff 稳定后，每项固定门禁只执行一次。会话上下文压缩不使已通过证据失效，不得因此重新阅读、重新设计测试或重跑成功命令。
- 可选调查或可选测试连续 15 分钟没有解决必需失败、也没有推进产品实现时立即停止。非 P0/P1 问题写入实施记录的跟进项，不扩大当前批次。
- 固定门禁通过且无 P0/P1 阻塞后，立即完成版本、实施记录和提交并停止，不再追加重构、清理或新边界测试。

### 11.2 后三批最小验证矩阵

| 批次 | 本批直接证据 | 固定必要回归 | 明确留后/不重复 |
| --- | --- | --- | --- |
| Phase-02-03 | Worker/notification 定向单元测试；真实 MySQL/RabbitMQ 的正常、重复、代表性 retry/dead 场景 | 评论、点赞与 Outbox Producer 定向回归；受影响 Worker package 的 race/vet；版本一致性 | 不跑 Frontend/Playwright；Broker/进程重启和完整故障矩阵留到 02-05；不穷举 ack/nack/关闭时序 |
| Phase-02-04 | Notification Repository/API 定向测试；通知页组件测试；一条真实双用户通知浏览器链路 | Frontend test/typecheck/build；认证与通知路由定向回归；版本一致性 | 不重跑 RabbitMQ 故障矩阵；不审计未修改 Backend package；不为 cursor/limit/状态组合建立全排列 |
| Phase-02-05 | Bash Worker 生命周期安全测试；第 11.3 节封闭故障矩阵一次 | 脚本治理、版本一致性和远程 CI；仅对实际修复的 Backend/Frontend package 补定向回归 | 不重复 02-03/02-04 已通过的单元矩阵；不新增业务边界、依赖源码研究或计划外全量测试 |

详细方案中的命令和场景是固定完成清单，不是继续扩展测试的起点。同一最终实现上每项执行一次；某项失败后，只重跑受修复影响的项目及恢复前置，不从头重复整套矩阵。

### 11.3 阶段级端到端验收

在 `scripts/verify-business.sh` 创建且验证归属的独立 Compose project 中，从空数据库固定执行以下十项：

1. 迁移、Backend、Business Worker 和 Frontend 启动成功，`/health`、`/ready` 与 Phase 1 业务闭环通过。
2. 用户 A 创建帖子；用户 B 评论和首次点赞后，HTTP 操作立即成功且 MySQL 核心事实可查询。
3. Worker 运行时，用户 A 最终能在 Frontend 看到两类通知；重复点赞不增加通知。
4. 停止 Worker 后继续评论/首次点赞，核心 API 成功、消息保留；恢复 Worker 后待处理通知各生成一次。
5. 停止 RabbitMQ 后继续评论/首次点赞，核心 API 成功且 Outbox 保留；恢复 RabbitMQ 后 Dispatcher 自动补投、Worker 自动消费，无需客户端重试。
6. 在“RabbitMQ 已接收但 Outbox 尚未标记”边界制造重复投递，通知唯一约束吸收重复。
7. 临时处理失败按有限延迟重试，非法/未知消息进入死信，后续合法消息不被永久阻塞。
8. 重启 Backend 和 Worker 后，pending/leased Outbox、队列消息和通知事实均按契约恢复。
9. Redis 清空/故障回归仍不影响 MySQL 核心事实；RabbitMQ 不参与帖子和评论查询事实装配。
10. 成功、失败或中断后只清理本次验收资源，日常开发栈、用户 `.env`、数据库、Redis 和 RabbitMQ volume 不受影响。

这十项构成 Phase 2 的完整且封闭的阶段矩阵；除非某项真实失败暴露新的 P0/P1 风险，不追加等价故障排列、性能测试或新的依赖失效场景。

## 12. Phase 2 阶段级验收标准

- 评论和首次点赞的 MySQL 核心事实与 Outbox 原子提交，任何路径不存在“事实成功但永久没有待投递记录”的已知窗口。
- 请求线程不直连 RabbitMQ 决定业务成功；Broker 故障期间核心写 API 保持可用，恢复后自动补投。
- Consumer 停止期间 durable queue 保留消息，恢复后继续处理。
- Dispatcher 和 Worker 均能处理连接/Channel 中断与进程重启，不忙循环、不泄漏凭据。
- 投递语义如实记录为至少一次；重复投递只产生一条 `notifications` 记录。
- 临时失败有限重试，永久错误进入可检查死信队列，不形成无限热重试。
- 用户只能查询和更新自己的通知；Frontend 不直连 RabbitMQ/MySQL，能够显示通知加载、空、错误、未读/已读和刷新状态。
- Phase 1 注册、登录、发帖、查询、评论、点赞、Redis 降级、认证恢复和浏览器闭环无回归。
- RabbitMQ 只承载 Phase 2 业务异步任务；Kafka、搜索和可观测数据未被提前引入。
- WSL2/Bash 生命周期和隔离验收通过，冻结的 PowerShell 文件没有被修改。
- 五份实施记录存在且与实际提交、命令、结果和限制一致，根 `VERSION` 与 Frontend npm 元数据为 `0.3.5`。

## 13. 里程碑完成条件

只有同时满足以下条件，Phase 2 才可标记完成：

1. Phase-02-01 至 Phase-02-05 均从对应权威分支完成、合入主远程 `main`，各批验收和必要回归通过。
2. 第 11.3 节完整故障矩阵在受支持 WSL2/Bash 环境真实执行并通过，不以 mock 或仅队列存在替代端到端通知结果。
3. `main` 根 `VERSION` 为 `0.3.5`，Frontend npm 元数据一致，质量门禁通过。
4. 文档、消息契约、拓扑、配置、脚本和真实代码一致；未执行的远程或本地检查不得写成通过。
5. 没有未关闭的 P0/P1 问题；非阻断改进和接受限制已记录为后续项，而不是无限扩大本阶段。
6. 阶段产物可以直接支撑 Phase 3：后续帖子搜索索引可复用 Outbox/可靠投递思想，但不得未经设计直接复用通知队列或把 RabbitMQ 与 Elasticsearch 事实混为一体。

### 13.1 当前阶段结论（2026-09-02）

Phase-02-05 已在 `develop/0.3.5` 上完成本地实现、固定完成门禁和 WSL2/Bash 隔离故障矩阵，五份实施记录齐备，当前未发现 P0/P1 阻塞。由于本批尚未合入主远程 `main`，远程质量门禁也尚无本批通过证据，因此这里只判定“本地阶段验收通过”；待 `develop/0.3.5` 合入 `main` 且远程门禁通过后，才按本节条件将仓库 Phase 2 里程碑判定为完成。

## 14. 实施记录规则

每个详细方案完成后创建同名镜像记录：

```text
dev/imple/Phase-02/Phase-02-XX-<名称>.md
dev/logs/Phase-02/Phase-02-XX-<名称>.md
```

记录必须包含实际完成内容、文件、命令和结果、偏差、已知限制与跟进项。总方案本身不提前创建五份空实施记录，也不把规划验收表述为已经通过。

## 15. Phase 3 交接边界

Phase 2 向 Phase 3 交付可验证的业务异步基础和真实通知流量，但 Phase 3 的帖子搜索仍需单独设计索引文档、重建流程和 Elasticsearch 故障语义。

- MySQL 继续是帖子与通知事实来源。
- RabbitMQ 业务队列不承担搜索查询或可观测数据传输。
- 如 Phase 3 复用 Outbox 产生索引事件，应新增明确事件类型、消费者和重建兜底，不让通知 Worker 承担搜索职责。
- Phase 2 不承诺通用消息平台；只承诺本文定义的两类业务事件和通知副作用。
