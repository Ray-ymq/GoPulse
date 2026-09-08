# Phase-13-06：Review 整改开发记录

## 任务与基线

- 执行报告：`dev/review/2026-09-08-Phase-13实现Review报告.md`，整改 P2-01–04、P3-01–02。
- 用户指定继续使用权威分支 `develop/1.10.6` 并推送。本任务作为该 Review 的后续整改保留当前分支，没有重新分叉、重命名或改写历史。
- 开始时工作区干净；执行 `git fetch --prune origin`。基线为 `origin/main` 的 `7fe41b1`，当前分支另有报告提交 `fce13f1`。
- 总实施方案新增 Phase-13-06 的 `1.10.6` / `develop/1.10.6` 权威分配和镜像 split plan；原五批 PR #114–#118 已合入的事实同步到总方案。

## 实际完成

| Finding | 整改 |
| --- | --- |
| P2-01 | 搜索在同一个 PIT 链内按 MySQL 存在事实补取，保持命中顺序；每请求最多 5 轮、每轮最多 limit+1 个 hit。下一条有效结果不提前消费，cursor 指向最后实际消费 hit，并携带最新 PIT ID。预算耗尽可返回空页及 continuation；Frontend 此时仍显示加载更多，不显示确定的无结果文案。 |
| P2-02 | 新增向前 migration 000011，用 RESTRICT 外键和活动/完整墓碑 CHECK 保护通知形状。帖子删除事务先同时清空通知 post/comment 引用，再删资源和写 Outbox；任一步失败整体回滚。没有修改已交付的 000010 SQL。 |
| P2-03 | Frontend 类型与已知错误码加入 user_not_found，保留服务端安全消息，资料页显示“用户不存在”。 |
| P2-04 | 新增整改批次权威版本/分支分配、split plan、实施记录，受管产品版本同步到 1.10.6。 |
| P3-01 | 原五批完成版本 1.10.5、PR #118 / 7fe41b1 已合入与追加整改状态明确区分，后续交接基线改为整改合入后的 1.10.6。 |
| P3-02 | Follow PUT/DELETE route 和 handler 数字 ID 解析参数统一为 userId；公开资料 GET 继续使用 username。 |

## 验证证据

命令输出保存在本机 `/tmp/gopulse-p1306/`，不把临时日志或测试凭据提交到仓库。

| 实际命令 | 结果 |
| --- | --- |
| `cd backend && go test ./internal/search ./internal/post ./internal/http` | 通过，go.log。新增搜索直接测试覆盖 20 条陈旧 hit 后的有效结果，以及 110 条陈旧 hit 触发扫描预算后继续到有效结果。 |
| `cd backend && go run ./cmd/migrate up` | 隔离 MySQL 实际应用全量 migration 通过，migrate.log。 |
| `go test -tags=integration ./migrations -run TestIntegrationNotificationShapeMigrationRoundTrip -count=1` | 通过，migration-test.log。实际 up/down/up；合法活动通知/完整墓碑，非法类型组合、双向半墓碑和非法 UPDATE；回滚保留数据。 |
| `go test -tags=integration ./internal/post ./internal/notification ./internal/search ./internal/http` | 通过，integration.log。post/search/http 实际执行；notification 首次命中 Go 缓存，以下命令强制在新数据库重新验证。 |
| `go test -count=1 -tags=integration ./internal/notification` | 实际通过，notification.log。新数据库和约束改变是必要重跑原因，不把缓存当作新环境证据。 |
| `go test ./migrations` | 通过，migrations-unit.log。 |
| `cd frontend && npm test -- --run` | 17 files / 68 tests 通过，frontend.log；新增 HTTP 404 语义、资料不存在状态和空搜索页续页测试。 |
| `npm run typecheck` | 通过，typecheck.log。 |
| `npm run build` | 通过，build.log；版本元数据改为 1.10.6 后再次构建通过，build-versioned.log。重跑仅为验证最终版本化产物。 |
| `bash scripts/verify-business.sh --self-test` | 通过，self-test.log：接受一个安全目标、拒绝六个不安全目标且未访问 Docker。 |
| `python3 scripts/ci/validate_versions.py` | 通过，受管元数据与根 VERSION 一致。 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.10.6 --base-ref origin/main` | 通过，整改分支匹配唯一权威分配。 |

## 偏差、限制与停止边界

1. 报告建议直接添加 CHECK 并保留 SET NULL，但在本任务隔离 MySQL 8.4 实际复现错误 3823：被 SET NULL 外键动作使用的列不能参与 CHECK。因此采用 RESTRICT + 删除事务显式原子墓碑化，已用真实 migration、删除成功/Outbox 失败回滚和业务闭环验证。没有读取第三方依赖源码。
2. 000011 down 明确恢复原 v10 schema（SET NULL 且无形状 CHECK），保留墓碑；再次 up 恢复约束。并不声称历史 v10 或 000010 down 具备新约束。
3. 已有非法通知数据会导致新增 CHECK 的 migration 失败，不自动删除或猜测修复数据。升级时须先排查异常；直接删除仍被活动通知引用的资源将被 RESTRICT 拒绝，必须走原子墓碑化路径。
4. 本轮范围扩大仅限报告要求的通知持久化与搜索分页直接集成；未运行额外全量 Compose、独立架构 Review、依赖审计或 Phase 14 实现。
5. 独立集成使用本任务新建的 `gopulse-p1306` Compose project、白名单 gopulse_integration 数据库/用户、loopback 端口和 Redis DB 15。集成结束后已清理本任务资源，再运行业务验收，避免触发其外部环境变更保护。没有手工修改现有 `gopulse-p13-local` 资源。

## 验证中发现的问题与最终复验

- 首次 `verify-business.sh` 未通过：edit E2E 等待旧词搜索空数组时收到错误响应（business.log）。原因是补页实现对零命中调用了不接受空 ID 的 FindMany。修复为只对非空 ID 批次水合，并按既有最多 50 IDs 合同拆批，避免最大 limit 加 lookahead 超限；新增空命中/最大页直接回归测试。没有把这次失败记为成功。
- 修复后 `go test ./internal/search` 通过（search-final.log）。其余前端、通知/删除代码未受此改动影响，不重复已成功的相应检查。
- 因搜索代码改变，重建本任务隔离环境并重新执行搜索集成。第一次 Elasticsearch 尚未 ready（search-integration-final.log，search unavailable）；等待实际 cluster health 后，`go test -count=1 -tags=integration ./internal/search` 通过（search-integration-ready.log）。再次清理该环境后，重新运行尚未通过的完整业务门禁。

## 最终业务门禁与完成状态

- `bash scripts/verify-business.sh` 最终退出码 0（business-final.log）：Chromium 7 passed / 3 条环境条件型 skipped；targeted search-rebuild 1 passed；targeted search-live 1 passed；Phase 2 故障矩阵 10/10；Backend/Worker/Indexer/Reindex 日志校验通过；隔离资源清理与开发状态不变检查通过。
- 所有约定的完成门禁已通过，首轮失败及修复如上，无剩余阻断项。产品完成版本为 `1.10.6`；本记录不声称整改分支已经合入 main，也不创建 PR。

## 变更文件

- `.env.example`
- `VERSION`
- `backend/internal/http/api.go`
- `backend/internal/http/user_handler.go`
- `backend/internal/post/delete.go`
- `backend/internal/post/edit_integration_test.go`
- `backend/internal/search/service.go`
- `backend/internal/search/service_test.go`
- `backend/migrations/000011_notification_shape.down.sql`
- `backend/migrations/000011_notification_shape.up.sql`
- `backend/migrations/notification_shape_integration_test.go`
- `dev/imple/Phase-13/Phase-13-06-Review整改.md`
- `dev/imple/Phase-13/Phase-13-总实施方案.md`
- `dev/logs/Phase-13/Phase-13-06-Review整改.md`
- `dev/review/2026-09-08-Phase-13实现Review报告.md`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/services/http.test.ts`
- `frontend/src/services/http.ts`
- `frontend/src/types/api.ts`
- `frontend/src/views/ProfileView.test.ts`
- `frontend/src/views/ProfileView.vue`
- `frontend/src/views/SearchView.test.ts`
- `frontend/src/views/SearchView.vue`

- 最终 `git diff --check` 通过；仅暂存本任务变更并以英文 Conventional Commit 提交，随后按用户要求推送。
