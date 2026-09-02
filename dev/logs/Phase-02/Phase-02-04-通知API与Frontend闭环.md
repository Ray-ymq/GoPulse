# Phase 2-04：通知 API 与 Frontend 闭环开发记录

## 1. 完成内容

- 新增认证通知 API：recipient-scoped 列表查询与单条幂等标记已读。
- 使用 `(created_at, id)` 严格降序 keyset cursor，默认 `limit=20`、最大 `limit=50`。
- 通知 DTO 仅暴露通知 ID、类型、时间、公开 actor 摘要、帖子 ID 与可空评论 ID；未暴露 source event、Outbox、AMQP 或 Worker 内部状态。
- 已读更新同时限定 notification ID 与当前 recipient；不存在和越权统一映射为安全的 `404 notification_not_found`，重复操作保留首次 `read_at`。
- Backend Server 完成 Notification Repository、Service、Handler 和受保护路由装配。
- Frontend 新增受保护 `/notifications` 路由、主导航入口、严格 DTO 校验和通知页面。
- 页面实现初次加载、空列表、显式刷新、分页、已无更多、加载失败、刷新失败、操作失败和单条防重复提交状态，并明确提示通知异步到达。
- 新增真实双用户 Playwright 链路，验证评论/首次点赞经 Outbox、RabbitMQ 和 Worker 最终出现在接收者页面，actor 隔离、重复点赞不增加通知、已读持久化及帖子跳转有效。
- 更新 README 和产品版本至 `0.3.4`。

## 2. 变更文件

### Backend

- `backend/cmd/server/main.go`
- `backend/internal/apperror/error.go`
- `backend/internal/http/api.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/router_notification_test.go`
- `backend/internal/notification/handler.go`
- `backend/internal/notification/integration_test.go`
- `backend/internal/notification/model.go`
- `backend/internal/notification/pagination.go`
- `backend/internal/notification/repository.go`
- `backend/internal/notification/service.go`
- `backend/internal/notification/service_test.go`

### Frontend

- `frontend/src/components/AppNav.vue`
- `frontend/src/router/index.ts`
- `frontend/src/services/api.ts`
- `frontend/src/services/http.ts`
- `frontend/src/styles.css`
- `frontend/src/types/api.ts`
- `frontend/src/views/NotificationsView.vue`
- `frontend/src/views/NotificationsView.test.ts`
- `frontend/e2e/business.spec.ts`
- `frontend/package.json`
- `frontend/package-lock.json`

### Documentation and version

- `README.md`
- `VERSION`
- `dev/logs/Phase-02/Phase-02-04-通知API与Frontend闭环.md`

## 3. 实际验证

- `(cd backend && go test ./internal/auth ./internal/http ./internal/notification)`：通过。
- `(cd backend && go vet ./internal/http ./internal/notification)`：通过。
- `(cd backend && go test -count=1 -tags=integration ./internal/notification)`：通过；使用临时、白名单 `gopulse_integration` MySQL 容器并应用全部向上迁移。
- `(cd frontend && npm test -- --run)`：通过，8 个测试文件、42 个测试通过。
- `(cd frontend && npm run typecheck)`：通过。
- `(cd frontend && npm run build)`：通过。
- `(cd frontend && npm run test:e2e -- --grep notifications)`：通过，1 条真实通知浏览器链路通过；开发 Backend/Frontend 与独立 Business Worker 连接真实 MySQL、RabbitMQ 运行。
- `python3 scripts/ci/validate_versions.py`：通过。
- `git diff --check`：通过。

## 4. 与方案的偏差

- 无产品范围偏差。
- Phase-02-05 才负责把 Worker 纳入统一生命周期与完整故障恢复验收，因此本批 Playwright 验证期间按现有 Phase-02-03 入口独立启动 Business Worker；未修改 `scripts/dev.sh` 或冻结 PowerShell 脚本。
- Frontend 标记已读 API 按方案返回 `204`，页面成功后立即呈现已读状态；刷新后再由数据库事实确认持久化时间。API 不额外返回已读 DTO。

## 5. 已知限制与后续项

- 不包含 WebSocket、SSE、后台轮询、批量已读、删除、归档、偏好或未读角标，符合本批边界。
- Worker 与 Broker 生命周期脚本、故障恢复矩阵和 Phase 2 阶段收口留给 Phase-02-05。
- 通知引用的帖子和评论继续依赖当前 RESTRICT 外键，不实现已删除资源 fallback。
