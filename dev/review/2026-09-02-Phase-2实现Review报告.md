# GoPulse Phase 2 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-02 |
| Review 基线 | `efff938367b92d377293e27c9a052d1a04a4b8b6`（`origin/main`，PR #39 合并提交） |
| Phase 2 变更范围 | `0d96c2be31ff8df071b397768889ad420995513f..efff938367b92d377293e27c9a052d1a04a4b8b6` |
| Review 分支 | `develop/0.3.6`，从最新 `origin/main` 创建 |
| 当前完成版本 | `0.3.5`；本次只新增 Review 文档，不修改 `VERSION` |
| 实施批次 | Phase-02-01 至 Phase-02-05 |
| 实际执行环境 | WSL2 Linux，Go 1.26.7，Node.js 24.20.0，npm 11.19.0，Docker 29.7.2 / Compose v5.5.0，Docker context `default` |
| Review 范围 | Phase 2 总方案与五份拆分方案、五份实施记录、消息契约、Outbox、RabbitMQ Publisher/拓扑、评论与点赞事务、Business Worker、通知持久化/API/Frontend、Bash 生命周期与可靠性验收、Git 提交及远程质量门禁 |
| 变更规模 | 89 个文件，7421 行新增、137 行删除 |
| 结论 | **有条件通过（Conditional Pass）** |

本次 Review 以已经合入主远程 `main` 的 Phase 2 最终状态为准，重点判断：

1. 评论和首次非 self 点赞是否能够在同一 MySQL 事务中保存核心事实与 Outbox 事件。
2. RabbitMQ 暂停、不可用、重启或消息重复时，是否仍维持 MySQL 成功语义、至少一次投递和通知幂等。
3. 独立 Business Worker、通知 API 与 Frontend 是否形成真实用户可见的异步闭环。
4. 生命周期、故障矩阵、版本、分支和文档是否达到阶段完成条件。
5. Phase 2 产物能否作为 Phase 3 搜索与后续异步能力的可靠输入基线。

## 2. 总体结论

Phase 2 的核心架构和用户链路实现质量良好。评论和首次点赞已经通过 Transactional Outbox 与 RabbitMQ 解耦；请求线程不依赖 Broker 成功来决定 MySQL 核心写入结果；Publisher 使用持久消息、mandatory routing 和 publisher confirms；Worker 使用 manual ack、确认后的 retry/dead 二次发布以及 `source_event_id` 唯一约束实现至少一次消费下的通知幂等。通知 API 始终使用认证用户作为 recipient scope，Frontend 也已形成加载、空状态、错误、分页、刷新、已读和帖子跳转闭环。

本次独立执行 Backend 默认测试、vet、race、Frontend 测试/类型检查/构建、治理测试、Bash/Compose 检查、验收安全自测和完整隔离业务验收，均通过。完整验收包括 2 条真实 Chromium E2E 和 10 项 Phase 2 封闭可靠性矩阵，结束后没有遗留验收容器、网络或命名卷。PR #39 已于 2026-09-02 08:26:59 UTC（北京时间 16:26:59）合入 `main`，其 PR head `8d36cd720e32c0ad0c84568ac1bafd37189586bb` 上的 Integration、Backend、Frontend、Branch governance、Scripts and Compose 及自动合并任务均为 `success`。

本次未发现 P0 或 P1 问题。记录 4 项 P2 和 1 项 P3：

1. Outbox 已提供 published cleanup Repository，但生产运行时没有任何清理调度，已发布行会无限累积。
2. 同一批记录共享一个租约截止时间，但 Dispatcher 串行 publish；配置只约束单次 publish timeout 小于租约，没有保证整个 batch 能在租约内完成。
3. Worker shutdown timeout 到期后返回，但在途 handler 使用不可取消的 `context.Background()`，旧处理 goroutine 仍可能存活并与 redelivery 重叠。
4. Phase 2 总方案和 README 仍保留“待 PR 合入/待远程门禁”的历史状态，与已经合入且门禁通过的事实不一致。
5. Phase 2 主线包含一个标题像 merge、实际为单父且 tree 不变的无内容提交，增加提交历史歧义。

这些问题当前没有破坏核心事实一致性，也没有导致验收失败，因此结论为有条件通过。建议在扩大流量、运行多个 Backend 实例或让 Phase 3 复用 Outbox 模式前，优先关闭 P2-01 和 P2-02；P2-03 应在复用 Worker runtime 前关闭；P2-04 应作为阶段治理收口立即修正。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心业务完全不可用，必须立即停止发布 |
| P1 | 阻断阶段验收、受支持平台或关键安全/事实一致性边界，应在进入下一阶段前修复 |
| P2 | 核心基线可运行，但容量、故障恢复、生命周期、阶段治理或维护风险明显，应安排近邻修复 |
| P3 | 低风险提交卫生、元数据或文档可读性问题，可随相邻批次处理 |

本次共记录：

- P0：0 项
- P1：0 项
- P2：4 项
- P3：1 项
- 已知且被方案接受的限制：2 项

## 4. Phase 2 完成定义核对

| 完成定义 | 结果 | Review 证据 |
| --- | --- | --- |
| 评论与首次非 self 点赞在同一事务写核心事实和 Outbox | 通过 | 评论、点赞 Repository 使用活动 `*sql.Tx` 同时提交事实与事件；重复点赞和 self 行为不产生新事件 |
| 请求线程不以 RabbitMQ 可用性决定 MySQL 业务成功 | 通过 | HTTP 写入只依赖 MySQL 事务；Broker outage 矩阵中评论/点赞事实及 Outbox 仍成功保存 |
| Publisher 只在消息持久、可路由且收到 confirm 后标记 published | 通过 | persistent、mandatory、return、confirm 和发布超时均已实现；故障矩阵覆盖 Broker 停止和恢复 |
| Outbox lease 支持并发 claim、失败释放、过期回收和至少一次重投 | 通过，但存在 P2-02 | 状态机和 integration 证据完整；批量租约预算仍可能不足 |
| 已发布 Outbox 记录只保留有限时间 | **未完全通过，见 P2-01** | Repository 和索引存在，但没有生产清理循环、保留期配置或运维入口 |
| Worker 独立运行、断线重连、manual ack、有限 retry 和 dead queue | 通过，但存在 P2-03 | 正常、临时失败、永久 poison、RabbitMQ 重启和 Worker 重启矩阵均通过；超时关闭未真正取消在途 handler |
| 重复投递只产生一条通知 | 通过 | `notifications.source_event_id` 唯一约束；重复消息矩阵和 integration 测试通过 |
| 用户只能查询和更新自己的通知 | 通过 | recipient ID 来自认证上下文；他人/不存在通知统一返回安全 404 |
| Frontend 通知闭环真实可用 | 通过 | 8 个 Vitest 文件、42 项测试通过；真实双用户 Chromium 通知 E2E 通过 |
| Phase 1 业务和 Redis 降级无回归 | 通过 | Backend 全量测试、race 和完整浏览器业务验收通过 |
| WSL2/Bash 生命周期与隔离故障矩阵通过 | 通过 | `scripts/verify-business.sh` 完整执行，2 条 E2E 和 10 项可靠性矩阵全部通过 |
| PowerShell 保持 `0.2.1` 冻结基线 | 通过 | Review 变更范围内没有 `scripts/*.ps1` 修改 |
| 根版本与 Frontend npm 元数据均为 `0.3.5` | 通过 | `python3 scripts/ci/validate_versions.py` 通过 |
| Phase-02-01 至 02-05 已合入 `main` 且远程门禁通过 | 通过 | `origin/main=efff938`；PR #39 已合入，PR head 上 6 个 check run 均成功 |
| 权威文档与当前阶段状态一致 | **未通过，见 P2-04** | 总方案和 README 仍声明等待 PR 合入及远程门禁 |

## 5. 分批实现评估

### 5.1 Phase-02-01：事务 Outbox 与消息契约基础

完成了版本化业务事件、严格编码/解码、RabbitMQ 拓扑契约、`business_outbox` Migration 和 lease-aware Repository。消息大小、UUID、UTC 时间、事件字段组合、状态与租约字段组合均有明确约束。并发 claim 使用 `FOR UPDATE SKIP LOCKED`，mark/release 由 owner 和有效租约保护，基础状态机设计正确。

主要缺口是 cleanup 只停留在 Repository 能力，没有接入运行时，因而总方案中的“已发布记录有限保留”尚未形成真实运维闭环，见 P2-01。

### 5.2 Phase-02-02：原子事件生产与可靠投递

评论和首次点赞实现了核心事实与 Outbox 原子写入；self comment、self like 和重复点赞不会错误地产生通知事件。Dispatcher 不持有数据库事务等待网络，Publisher 对 mandatory return、confirm ack/nack、Channel/Connection 关闭和超时进行了分类处理。成功 publish 后 mark 失败被如实视为至少一次重复边界，没有虚构 exactly-once。

主要缺口是 batch 内所有记录共享 claim 时刻的租约，而 publish 串行执行，当前配置关系没有覆盖整个批次的最坏时间预算，见 P2-02。

### 5.3 Phase-02-03：Business Worker 与幂等通知

独立 Worker、拓扑声明、连接重建、manual ack、确认后的 retry/dead publish、最大重试次数、poison 隔离和通知唯一键均已实现。Worker 不依赖 Redis、Gin、Cookie 或 Frontend，职责边界清晰。失败原因和日志经过收敛，不输出 AMQP 凭据或 payload 原文。

主要缺口是 shutdown timeout 只限制 runtime 等待时间，没有取消在途处理 goroutine，见 P2-03。

### 5.4 Phase-02-04：通知 API 与 Frontend 闭环

通知列表使用严格 keyset cursor 和 limit；已读更新是 recipient-scoped 且幂等；API 对非法参数、未认证和资源不可见场景返回稳定错误。Frontend 对通知响应做严格形状校验，覆盖初始加载、空状态、失败重试、分页、刷新、未读/已读和跳转。真实双用户 E2E 证明评论和首次点赞最终可见，重复点赞不会新增通知。

本批未发现新的 P0-P2 功能问题。

### 5.5 Phase-02-05：可靠性验收与阶段收口

Worker 已纳入 Bash-only 生命周期，验收脚本使用随机项目名、回环端口、归属检查和无条件 trap 清理。可靠性矩阵真实覆盖消费者停止、Broker 停止、Backend 重启、Worker unacked redelivery、重复 event ID、有限 retry、dead queue、后续健康消息和 RabbitMQ 持久化重启。此次 Review 重新执行完整矩阵，结果一致。

实现和验收本身满足阶段收口要求，但 PR 合入后没有同步更新权威总方案与 README 的最终状态，见 P2-04。

## 6. Findings

### P2-01：published Outbox 行没有生产运行时清理机制

**位置**

- `backend/internal/outbox/repository.go:255-280`
- `backend/internal/outbox/dispatcher.go:26-31`
- `backend/migrations/000002_business_outbox.up.sql:20`
- `dev/imple/Phase-02/Phase-02-总实施方案.md:191`
- `dev/imple/Phase-02/Phase-02-01-事务Outbox与消息契约基础.md:58,96`

**证据**

`Repository.CleanupPublished` 已实现按 cutoff 和有限 batch 删除 published 行，Migration 也提供了 `idx_business_outbox_published_cleanup`。但是全仓库对 `CleanupPublished` 的引用只有该方法自身和 integration 测试；Dispatcher 的 `Store` 接口只包含 `Claim`、`MarkPublished` 和 `ReleaseFailed`，Backend、Worker、Bash 脚本也没有清理调度、保留期配置或运维命令。

因此每一条成功投递事件都会永久保留在 `business_outbox`。当前验收流量下不会暴露，但持续运行后表、cleanup 索引、备份和查询统计都会无界增长。

**影响**

- 长期磁盘和备份体积持续增长。
- pending/lease 扫描索引和表维护成本上升，可能间接增加投递延迟。
- 方案宣称的“已发布记录有限时间保留”没有在真实运行时成立。
- 不直接造成消息丢失，因此定为 P2 而不是 P1。

**建议**

1. 增加明确的 published retention 配置和上下限，例如按天定义保留期。
2. 在 Backend Dispatcher 旁增加可取消、低频、有界 batch 的清理循环，或提供由受控定时任务调用的独立维护命令。
3. 每次只删除有限数量，批间退让；失败只记录收敛日志，不阻断业务写入和投递。
4. 继续保证清理条件只能命中 `status='published'` 且 `published_at < cutoff` 的行。
5. 增加一个最低层测试，证明运行时调度会调用 cleanup，且 pending/leased 行不会被删除。

**完成条件**

- 生产入口真实启用有界清理。
- 保留期和 batch 有明确默认值、范围与文档。
- 长期运行时 published 行有可验证的容量上界，而 pending/leased 安全边界不变。

---

### P2-02：Outbox 租约只覆盖单次 publish，没有覆盖串行 batch 的最坏耗时

**位置**

- `backend/internal/outbox/repository.go:125-126,163-173`
- `backend/internal/outbox/dispatcher.go:150-163,177-203`
- `backend/internal/config/config.go:204-220`
- `.env.example:29-32`

**证据**

一次 `Claim` 为整个 batch 计算同一个 `leaseExpiresAt = now + leaseDuration`，Dispatcher 随后逐条串行 publish。配置校验只要求：

```text
OUTBOX_PUBLISH_TIMEOUT < OUTBOX_LEASE_DURATION
```

没有约束：

```text
OUTBOX_CLAIM_BATCH × OUTBOX_PUBLISH_TIMEOUT + 状态更新开销 < OUTBOX_LEASE_DURATION
```

默认值为 batch 10、单次 publish timeout 5 秒、lease 30 秒；仅 publish 的理论最坏时间已经达到 50 秒。合法配置上限还允许 batch 100、publish timeout 接近 lease，风险更明显。

当 Broker 或网络变慢时，batch 后部记录可能在真正 publish 前就失去租约。若消息已经成功进入 RabbitMQ，但 `MarkPublished` 时租约过期，记录会再次被 claim 并重复 publish；多 Backend 实例下，旧 owner 和新 owner 还可能同时处理同一行。

**影响**

- 重复消息和无效 publish 明显放大，增加 RabbitMQ、Worker 和 MySQL 压力。
- 租约丢失会制造难以解释的 mark/release 失败和投递延迟。
- 通知唯一键能够吸收重复，因此当前不构成数据丢失或重复通知事实；仍属于稳定性 P2。

**建议**

可选择一种或组合实现：

1. 配置层强制 lease 覆盖整个 batch 的 publish budget，并保留数据库更新和调度余量。
2. 每次只 claim 当前准备处理的记录，或把租约截止时间按记录/小批次错开。
3. 在处理下一条前检查剩余租约；不足以完成一次 publish 时不再发布，由安全状态转换或租约过期回收。
4. 提供受 owner 约束的 lease renewal，并明确 renewal 失败后的处理边界。
5. 增加确定性时钟测试，覆盖 batch 后部租约到期、成功 publish 后 mark 失败以及多 Dispatcher 回收。

**完成条件**

- 所有合法配置都不会在预期最坏 publish 时间内主动耗尽 batch 租约，或 runtime 能在剩余租约不足时安全停止。
- 至少一次语义保留，但慢 Broker 不再系统性制造可避免的重复投递。

---

### P2-03：Worker shutdown timeout 返回后，在途 handler 仍不可取消

**位置**

- `backend/internal/worker/runtime.go:111-129`
- `backend/internal/worker/integration_test.go:42-53,91-108`
- `backend/cmd/business-worker/main.go:34-38,56-59`
- `dev/imple/Phase-02/Phase-02-总实施方案.md:213-216`

**证据**

每条 delivery 都通过以下不可取消上下文启动：

```go
processingDone := make(chan error, 1)
go func() { processingDone <- handler.Handle(context.Background(), delivery) }()
```

收到 shutdown 后，runtime 停止新 delivery，并等待 `ShutdownTimeout`；timeout 到期时直接返回，没有 cancel handler 或等待 goroutine 退出。随后 session 被关闭，`run()` 返回并开始关闭 MySQL。

现有 integration 测试也证明 runtime 返回后旧处理仍然存活：测试先等待 shutdown runtime 完成，再启动新 runtime，最后才关闭旧 `blockingProcessor.release`。旧处理和 redelivery 因而存在重叠窗口；通知唯一键使结果仍为一条，但 runtime 的“有界完成或重新入队”没有包含“旧 handler 已停止”这一条件。

**影响**

- graceful shutdown 可能在数据库处理仍运行时关闭 AMQP/MySQL 资源。
- 作为可复用 runtime 或在测试进程中运行时会留下 goroutine；进程退出虽最终回收资源，但不是受控关闭。
- restart/redelivery 可能与旧处理并发，扩大至少一次重复窗口。
- 当前通知写入幂等且独立进程会退出，所以没有升级为 P1。

**建议**

1. 为当前 delivery 创建独立可取消 processing context，而不是 `context.Background()`。
2. shutdown 时先停止新 delivery，在 timeout 内允许当前处理完成；超时后 cancel processing context。
3. 保持 AMQP session 存活到 handler 观察 cancellation 并完成 nack/requeue 的 best-effort 路径，再关闭 session。
4. 用明确的 goroutine ownership/WaitGroup 保证 `Runtime.Run` 返回时其启动的 handler 已退出。
5. 修改阻塞测试 processor，使其同时等待 release 或 `ctx.Done()`，并断言 timeout 后旧 processor 不再执行数据库副作用。

**完成条件**

- `Runtime.Run` 返回时没有由该 runtime 启动的 handler goroutine 存活。
- shutdown timeout 后当前消息保持可 redelivery，且旧处理不会继续与新 consumer 并发执行。

---

### P2-04：Phase 2 权威状态仍停留在合入前

**位置**

- `dev/imple/Phase-02/Phase-02-总实施方案.md:65`
- `dev/imple/Phase-02/Phase-02-总实施方案.md:338-340`
- `README.md:341-343`

**证据**

当前 `origin/main` 已是 PR #39 的合并提交 `efff938367b92d377293e27c9a052d1a04a4b8b6`。PR #39 已于 2026-09-02 合入，PR head `8d36cd720e32c0ad0c84568ac1bafd37189586bb` 上的必要远程质量门禁全部成功。

但总方案状态表仍把 Phase-02-05 标记为“本地完成（阶段验收通过，待 PR 合入与远程门禁）”，第 13.1 节仍声明“尚未合入主远程 main，远程质量门禁也尚无本批通过证据”；README 也仍声明 Phase 2 只在 `develop/0.3.5` 完成、等待合并和门禁。

Phase-02-05 实施记录是当时执行事实，可以保留历史表述；问题在于总方案和 README 是当前权威状态入口，却没有在合入后收口。

**影响**

- 阶段状态、Git 历史、远程 checks 和权威计划互相冲突。
- Phase 3 规划或自动治理无法可靠判断 Phase 2 是否已经正式完成。
- 与总方案第 13 节要求的“文档和真实代码一致”不符。

**建议**

1. 将 Phase-02-05 状态更新为已完成，并记录 PR #39、合并提交和远程 checks 结论。
2. 将第 13.1 节改为 Phase 2 里程碑已完成，同时保留本次 Review 发现的 P2/P3 为后续项。
3. 更新 README 的阶段状态，不再使用“合入后才完成”的条件句。
4. 不回写 Phase-02-05 实施记录中的历史执行时状态，避免把事后结果伪装成当时已知事实。

**完成条件**

- 总方案、README、实施记录的时间语义、Git 合并历史、远程 checks 和 `VERSION=0.3.5` 对 Phase 2 状态给出一致且可追溯的结论。

---

### P3-01：Phase 2 主线包含标题误导的无内容提交

**位置**

- Commit `9ade10086ab7f8e79cbd26036eb971ae430759e0`

**证据**

该提交标题为：

```text
Merge remote-tracking branch 'upstream/main' into develop/0.3.2 (#34)
```

但它只有一个 parent `67b37616ea8dcaee9587a2a016414623e09efe9b`，且提交 tree 与 parent tree 都是 `a99362c7bf51391f20f04ef5e96167ed870f7979`；`git diff 9ade100^ 9ade100` 无输出。它既不是 Git merge commit，也没有树变更。

**影响**

- 不影响产品代码、版本或运行结果。
- 会让后续审计误以为发生过主线同步或冲突解决，增加提交范围判断成本。

**建议**

检查自动 PR/同步流程，避免生成标题为 merge 的单父 no-op commit；如果确需保留审计事件，应使用明确说明“no-op/sync marker”的提交信息或外部流水线记录。

## 7. 已知且被方案接受的限制

### 7.1 至少一次投递允许重复 publish，最终由通知唯一键收敛

RabbitMQ publish 成功但 Outbox mark 失败、Worker 成功写 MySQL 但 ack 失败，以及连接在确认边界中断，都可能导致重复投递。这是方案明确接受的至少一次语义，不应描述为 exactly-once。当前 `source_event_id` 唯一约束和 duplicate event 故障矩阵证明通知事实会收敛为一条。

P2-02 和 P2-03 不是因为“存在重复”本身，而是当前租约和关闭实现制造了额外、可避免的重复窗口。

### 7.2 当前验收是单节点本地 RabbitMQ，不代表生产集群 HA 或容量结论

完整矩阵覆盖单节点 RabbitMQ 容器停止/恢复、持久化重启、消费者停止、Worker 重启、retry 和 dead queue，但没有覆盖多节点 quorum queue、网络分区、跨可用区延迟、生产级备份恢复或规模压测。这与 Phase 2 范围一致，应在部署和稳定性阶段单独设计，不能从本次通过结果外推生产容量。

## 8. 验证记录

### 8.1 本地固定门禁和 Review 检查

| 命令 | 结果 |
| --- | --- |
| `test -z "$(gofmt -l .)"`（`backend`） | 通过 |
| `go test -count=1 ./...`（`backend`） | 通过 |
| `go vet ./...`（`backend`） | 通过 |
| `go test -race -count=1 ./...`（`backend`） | 通过，未发现数据竞争 |
| `npm ci`（`frontend`） | 通过，安装 171 个包，audit 报告 0 vulnerability |
| `npm test -- --run`（`frontend`） | 通过，8 个测试文件、42 项测试 |
| `npm run build`（含 `vue-tsc --noEmit`） | 通过 |
| `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | 通过，17 项治理测试 |
| `python3 scripts/ci/validate_versions.py` | 通过，根版本和 Frontend npm 元数据均为 `0.3.5` |
| LF checkout 检查 | 通过 |
| `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh` | 通过 |
| `bash scripts/verify-business.sh --self-test` | 通过，接受 1 个合法目标并拒绝 6 个不安全目标，未访问 Docker |
| `docker compose --env-file .env.example --file deploy/compose.yaml config` 及 4 个 loopback host binding 检查 | 通过 |
| `bash scripts/verify-business.sh` | 通过，真实 Chromium 2/2、封闭可靠性矩阵 10/10 |
| 验收后容器、volume、network 过滤检查 | 通过，无遗留 `gopulse-acceptance-*` 资源 |
| `git diff --check`（写入 Review 前） | 通过 |

本次没有重复运行本地 tagged integration 套件：同一最终 PR head 的远程 Integration gate 已成功，且本地完整隔离 E2E/故障矩阵已在真实 MySQL、Redis、RabbitMQ 和 Chromium 上通过。根据仓库执行效率规则，没有在环境和产品代码未变化时重复扩大验证。

### 8.2 远程 PR #39 证据

通过 GitHub API 独立确认：

- PR #39：`merged=true`
- 合并时间：2026-09-02 08:26:59 UTC
- 合并提交：`efff938367b92d377293e27c9a052d1a04a4b8b6`
- PR head：`8d36cd720e32c0ad0c84568ac1bafd37189586bb`
- `Quality gates before PR / Integration`：success
- `Quality gates before PR / Backend`：success
- `Quality gates before PR / Frontend`：success
- `Quality gates before PR / Branch governance`：success
- `Quality gates before PR / Scripts and Compose`：success
- `Open PR and enable auto-merge`：success

checks 记录在 PR head，而 squash merge 提交本身没有单独的 check run；本报告没有把 merge commit 上不存在的 checks 误写为通过。

### 8.3 Review 分支治理注意事项

按用户要求，本报告在本地 `develop/0.3.6` 分支生成。当前 Phase 2 总实施方案只分配到 `develop/0.3.5`，因此：

```text
python3 scripts/ci/validate_branch.py --branch develop/0.3.6 --base-ref origin/main
```

实际失败：

```text
ERROR: develop/0.3.6 must map to exactly one authoritative Phase allocation; found 0
```

这不是 `efff938` 上 Phase 2 产品实现的回归，也没有计入上述 P0-P3 数量；它是 Review 交付分支的治理条件。本次只新增 Review 文档，不擅自把 `0.3.6` 写入产品版本或虚构实施批次。若后续要推送该分支或为整改创建 PR，应先在权威总实施方案中分配对应批次/分支/版本；如果 Review 仅属于规划文档，则应按仓库规则评估是否改由 `update` 承载。

## 9. Phase 3 交接评估

Phase 2 已提供可供 Phase 3 借鉴的可靠异步基础：

- MySQL 核心事实与 Outbox 原子提交。
- 严格、版本化、有大小边界的事件契约。
- durable topology、persistent mandatory publish 和 confirms。
- 至少一次消费、幂等结果表、有限 retry 和 dead queue。
- 独立 Worker 生命周期和真实故障注入验收框架。

但 Phase 3 搜索索引不能直接把通知链路当成搜索事实源：

1. 搜索仍以 MySQL 帖子事实为源，Elasticsearch 只是可重建投影。
2. 索引事件应定义独立 event type、routing key、消费者和失败语义，不让通知 Worker 承担搜索职责。
3. 必须设计全量重建/对账路径，不能只依赖增量 RabbitMQ 消息恢复索引。
4. 在复用 Outbox 模式前关闭 P2-01 和 P2-02，否则索引事件量会放大表增长和租约重复问题。
5. 如果复用 Worker runtime，先关闭 P2-03，保证重建或批量消费任务能够受控停止。

综合判断：**Phase 3 可以开展方案设计和低流量实现，但不应在未处理 P2-01/P2-02 的情况下直接把当前 Outbox Dispatcher 扩展到高吞吐索引事件。**

## 10. 最终结论与关闭条件

Phase 2 核心目标已经实现，端到端业务和可靠性矩阵真实通过，远程 PR 门禁也已成功。本次没有发现阻断阶段的 P0/P1，因此给予 **有条件通过（Conditional Pass）**。

建议按以下顺序关闭 Review：

1. 更新 Phase 2 总实施方案和 README，记录 PR #39 已合入、远程门禁已通过、阶段里程碑已完成。
2. 为 published Outbox 增加真实运行时清理和保留期配置。
3. 修正 Outbox batch/lease 时间预算，避免合法配置系统性耗尽租约。
4. 修正 Worker processing context 和 goroutine ownership，确保 shutdown timeout 后旧处理已退出。
5. 调整自动同步/PR 流程，避免再次产生误导性的单父 no-op merge 标题提交。
6. 在推送 `develop/0.3.6` 前解决其权威批次分配；本次 Review 不修改 `VERSION=0.3.5`。

关闭 P2-04 后，仓库可以在治理层正式宣告 Phase 2 完成；关闭 P2-01、P2-02 和 P2-03 后，异步基础才适合在更高流量、多实例和 Phase 3 索引链路中继续复用。
