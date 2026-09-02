# Phase 2-04：通知 API 与 Frontend 闭环实施方案

> 执行序号：4 / 5  
> 前置批次：Phase 2-01 至 Phase 2-03 已完成并通过验收  
> 总方案来源：[Phase-02-总实施方案.md](Phase-02-总实施方案.md)

## 1. 批次目标

把 Worker 已持久化的通知以认证 Backend API 暴露给接收者，并新增 Frontend 通知页面，使用户能够观察评论/点赞后的异步结果、显式刷新和幂等标记已读。

本批不改变异步可靠性机制；Frontend 只查询 MySQL 通知事实，不读取 RabbitMQ/Outbox 状态。

## 2. 前置条件

- Phase-02-03 已合入远程 `main`，根版本与前一批目标一致。
- notifications 表、唯一 source event 和 Worker 写入语义已通过真实基础设施验收。
- Phase 1 认证 Cookie、统一响应、请求边界和 Frontend 恢复状态稳定。
- 已从远程最新 `main` 创建本批权威开发分支，未覆盖用户改动。

## 3. 实施范围

### 3.1 Notification API

新增认证路由：

```text
GET   /api/v1/notifications?limit=<n>&cursor=<opaque>
PATCH /api/v1/notifications/:notificationId/read
```

- 列表只返回当前用户作为 recipient 的通知。
- 按 `created_at DESC, id DESC` 使用严格 keyset cursor，默认/最大 limit 与现有分页风格一致。
- DTO 返回 notification ID/type/created_at/read_at，以及公开 actor 摘要、post ID 和可空 comment ID。
- 不返回 `source_event_id`、Outbox 状态、AMQP headers 或内部错误。
- 已读操作必须同时匹配 notification ID 和当前 recipient；不存在或不属于当前用户统一返回安全 404，防止枚举。
- 重复标记已读返回 204，保持第一次 `read_at` 或采用明确且测试固定的幂等语义。
- 严格路径 ID、cursor、limit 和统一错误响应沿用 Phase 1 规则。

### 3.2 Frontend 通知页

- 新增受保护 `/notifications` 路由和主导航入口。
- 页面显示动作用户、评论/点赞类型、目标帖子链接、创建时间和已读状态。
- 提供“加载更多”“刷新”和单条“标记已读”，防止重复并发提交。
- 明确显示初次加载、空列表、加载失败、刷新失败、已无更多数据和操作失败状态。
- 页面文案说明通知可能异步到达；不根据评论/点赞 API 成功立即伪造本地通知。
- 保持认证恢复错误页、受保护导航和 401 行为。
- 不引入 WebSocket、SSE、自动后台轮询或全局状态库。

### 3.3 契约装配

- Backend 查询时 join 公开用户摘要，不把用户名快照存入通知 Payload/表。
- 被引用帖子/评论在当前 Schema 下受 RESTRICT 外键保护；API 不需要为已删除资源虚构 fallback。
- Frontend DTO 严格校验公共响应，不接受任意 `any`。

## 4. 实施边界与非目标

- 不暴露 Outbox、queue depth、retry count 或 dead queue 管理 API。
- 不允许管理员或其他用户查询任意 recipient 的通知。
- 不实现批量已读、删除、归档、偏好、未读角标或实时推送。
- 不让 Frontend 直接访问 RabbitMQ management、MySQL 或 Worker。
- 不修改 Producer/Worker 交付语义，除非测试发现阻断缺陷；若修复必须记录归属并重跑对应验收。
- 不修改冻结 PowerShell 脚本。

## 5. 目标文件与交付物

预计涉及：

```text
backend/internal/notification/
backend/internal/http/
backend/cmd/server/
frontend/src/router/
frontend/src/views/
frontend/src/components/
frontend/src/services/
frontend/src/types/
frontend/src/**/*.test.ts
frontend/e2e/
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-02/Phase-02-04-通知API与Frontend闭环.md
```

## 6. 详细实施步骤

1. 定义 Notification 公共模型、cursor 和 Service/Repository 查询接口。
2. 实现 recipient-scoped keyset 查询和幂等已读更新。
3. 实现 Handler、统一响应/错误映射和认证路由。
4. 在最低有效层分别增加 Repository integration、Service/Handler 契约测试；同一权限或分页事实不跨层重复覆盖。
5. 定义 Frontend DTO/API 方法和严格响应处理。
6. 新增 `/notifications` 路由、导航和通知页面状态机。
7. 用组件测试覆盖页面主要成功状态，并各选一个代表性加载失败和操作失败；不穷举分页、刷新、空/错、401 与并发状态的全排列。
8. 用一条真实双用户链路同时验证 actor/recipient 隔离、帖子跳转、刷新后可见和已读操作。
9. 扩展一条通知专用 Playwright 场景，不重复完整 Phase 1 浏览器旅程。
10. 更新 README、版本和本批实施记录。

## 7. 风险与控制

- **越权读取/更新**：所有 SQL 同时限定 recipient ID，使用双用户测试证明隔离。
- **分页重复/遗漏**：使用 `(created_at,id)` 唯一稳定顺序和严格 opaque cursor。
- **泄漏内部消息细节**：DTO 白名单，不返回 event ID、payload、attempt 或 broker 信息。
- **错误的即时 UI**：页面只显示 API 查询结果，不从本地业务动作推断通知已完成。
- **认证回归**：沿用共享认证中间件与 HTTP 客户端，覆盖 401 和恢复状态。

## 8. 验证命令与必要回归

本节是本批固定完成清单。最终 diff 上各命令执行一次；上下文压缩后不重跑已通过项。只有实际失败或额外修改共享认证/运行环境时，才增加对应定向回归并在实施记录说明原因。

```bash
(cd backend && go test ./internal/auth ./internal/http ./internal/notification)
(cd backend && go vet ./internal/http ./internal/notification)
(cd backend && go test -count=1 -tags=integration ./internal/notification)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
(cd frontend && npm run test:e2e -- --grep notifications)
python3 scripts/ci/validate_versions.py
git diff --check
```

本批触及认证通知 API、数据库查询和 Frontend Router，因此只执行 auth/http/notification 定向 Backend 检查、Frontend 固定门禁和一条通知 E2E。该 E2E 必须由真实 Worker 链路产生通知；RabbitMQ/Worker 故障矩阵、完整 Phase 1 浏览器旅程、Backend 全量/race 和未修改 package 不重复执行，统一留给已有证据或 Phase-02-05 的阶段收口。

## 9. 验收标准

- 当前用户只能分页读取自己的通知；另一用户的通知既不可见也不可标记已读。
- 两种通知 DTO 字段正确，不泄漏 source event、Payload 或内部基础设施信息。
- cursor/limit/ID 代表性非法输入和不存在资源返回稳定错误；不要求所有等价编码与边界组合。
- 重复已读操作幂等且不会改变其他用户记录。
- Frontend 主要加载/空/刷新/分页/已读流程可用，并各有一个代表性加载失败和操作失败证据。
- 评论/首次点赞完成后，接收者通过刷新最终看到通知；重复点赞不增加通知。
- 页面刷新和临时认证恢复失败仍遵循 Phase-01-07 可重试契约。
- 第 8 节定向 Backend、Frontend、真实 MySQL/RabbitMQ/Worker 通知链路和 Playwright 固定门禁通过。

## 10. 明确完成条件

通知服务端事实已通过认证 API 和 Frontend 形成可操作闭环，全部契约与权限测试通过，版本和实施记录已提交。生命周期脚本和完整故障矩阵尚待 Phase-02-05；不满足或未如实记录该边界时不得标记本批完成。

## 11. 下一批交接

向 Phase-02-05 提供：

- 可从浏览器验证的两用户异步通知流程。
- 稳定 API/Frontend 状态，可用于消费者和 Broker 故障恢复断言。
- 必要的单元、integration 和 Playwright 基线。
