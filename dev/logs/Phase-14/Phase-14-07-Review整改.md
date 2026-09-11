# Phase-14-07：Review 整改开发记录

## 当前状态与基线

2026-09-12，三项整改已实现并通过本批固定门禁，完成版本 `1.11.7`，分支 `develop/1.11.7`。本记录不宣称整改已合入 main。

用户明确要求在权威分支上完整执行 Review 并推送，因此沿用含 Review 提交 `d030bd5` 的分支，不另建分支。开始时默认代理 `127.0.0.1:5780` 不可用，fetch 失败；随后仅对命令取消代理并设置 `git -c http.proxy=`，成功 fetch origin。origin/upstream 指向同一仓库；origin/main 仍为 `d16ed4a03051658903b43e83e4f6b4cdd97ccf78`，版本 `1.11.6`，已核对其包含第六批合入提交。原有未跟踪文件 `~` 未修改、不提交。

## 实际变更

- `monitor/internal/plugin/runtime.go`：shutdown 收集 lock/stop 错误并继续遍历全部官方插件，最终 `errors.Join` 返回；使用同一传入 context，未修改停止算法、归属验证或 blocked 拒绝逻辑。
- `monitor/internal/plugin/runtime_test.go`：保留真实损坏 Redis active revision 的代表性回归；确认 Redis 被阻断、原安全错误仍可 `errors.As` 识别、后续 MySQL collector 收到一次 Disable。
- `frontend/src/views/ObservabilityExportersView.vue`：更新成功后重新读取 catalog，使 revision 和配置门禁同步；刷新失败保留成功版本 DTO，单独提示刷新状态、不要重复上传。
- `frontend/src/views/ObservabilityExportersView.test.ts`：两种组件结果覆盖目录刷新成功/失败、旧门禁解除/保留、成功版本及上传文件清空。
- `dev/imple/Phase-14/Phase-14-总实施方案.md`：追加第七批 `1.11.7` / `develop/1.11.7` 权威分配；准确记录前六批已合入，保留合入前历史，关联整改与 Phase 15 交接。
- 第六批同名方案/记录：补充 `d16ed4a` 合入后状态，明确旧“待合入”及交接结论是历史。
- 第七批方案/本记录及 Review 报告：补齐验收合同、实际证据和整改状态，不改写原 Review 当时结论。
- `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`：同步产品和默认镜像版本 `1.11.7`；不改写已受信制品或第六批 acceptance fixture。

## 实际验证

| 命令/检查 | 结果 |
| --- | --- |
| `cd monitor && go test ./internal/plugin -run '^TestShutdownContinuesPastBlockedPlugin$' -v` | 通过，损坏 Redis 不阻断 MySQL Disable，仍返回安全错误 |
| `cd frontend && npm test -- src/views/ObservabilityExportersView.test.ts` | 通过，1 文件、2 tests |
| `cd monitor && go test ./internal/plugin ./internal/httpserver` | 最终门禁通过；plugin 实际执行，httpserver 使用缓存 |
| `cd frontend && npm test -- src/views/ObservabilityExportersView.test.ts src/services/exporters.test.ts` | 最终门禁通过，2 文件、6 tests |
| `cd frontend && npm run typecheck` | 通过 |
| `git merge-base --is-ancestor d16ed4a origin/main`、`git show origin/main:VERSION` | 通过；主线版本 `1.11.6` |

最终命令输出保存在本机 `/tmp/gopulse-phase14-remediation/`，不是持久交付物。定向检查后仅同步版本/文档，按新版本运行上述最终门禁一次。提交前还执行版本一致性、文档关联、`git diff --check` 与 `git diff --cached --check` 检查。

## 偏差、边界与停止

- 原六批交付后追加一个受限 Review 整改批次；用户指定的既有分支不改名、不重编号。
- 没有更改真实进程停止或总超时算法；回归在 Manager/collector 层证明隔离，不声称重新执行真实 Docker 退出场景。
- 没有重跑完整 Compose、Browser E2E、真实迁移、镜像构建、race、全仓库或跨平台支持矩阵；无依据扩大范围。
- 未检查第三方依赖源码。既有第六批完整运行时证据保留为历史，不冒充本批检查。
- P2-01、P3-01、P3-02 在本分支整改完成，无本批阻断项；合入 main 仍由后续合并流程完成。

## PR 门禁跟进：冷启动迁移连接超时（2026-09-12）

### 实际失败与定位

推送 `198710c` 后，Actions run `34632817517` 的 `Full-stack Compose acceptance`（job `103373519432`）失败，自动创建 PR 的 job 被跳过；其余九个质量门禁 job 成功。不是已创建 PR 的合并冲突。

远程日志显示空 Compose 的 migrate 服务在 `000009_post_edits` 的多语句 DDL 返回 `invalid connection`，随后 `SELECT RELEASE_LOCK(?)` 返回 `driver: bad connection`，最终 `cold complete Compose startup failed`。原 MySQL migration 连接继承业务连接的 1 秒 ReadTimeout。CI 日志没有底层网络错误细节，不能单靠该错误排除所有外因；定向实验证明这一过短预算可稳定产生相同错误。

### 修复与范围

- `backend/internal/platform/mysql.go`：只将 migration 专用 ReadTimeout 设为有界的 2 分钟；应用读超时仍为 1 秒，dial/write 超时不变，multi-statements 仍只用于迁移。
- `backend/internal/platform/platform_test.go`：验证业务与迁移读预算隔离、迁移预算有限及其余超时不变。
- `backend/internal/platform/integration_test.go`：真实 MySQL 下执行 `SELECT SLEEP(1.2)`，直接证明迁移连接能等待超过业务预算的服务器响应。
- 没有重试失败 DDL、force dirty version、跳过迁移或放宽 CI；不改迁移 SQL、生产数据及无关容器。
- 属于同一 `develop/1.11.7` 的 PR 跟进，产品版本仍为 `1.11.7`。扩展验证的具体依据是 CI 观测到的真实持久化迁移失败，而非额外审计。

### 实际检查

| 检查 | 结果 |
| --- | --- |
| `cd backend && go test ./internal/platform ./cmd/migrate ./migrations` | 通过；migrations 使用缓存 |
| 原 HEAD 的 mysql.go + `go test -tags=integration ./internal/platform -run '^TestIntegrationMigrationAllowsSlowStatement$' -count=1 -v` | 如预期失败，约 1.01 秒返回 `invalid connection`；随即恢复修复代码 |
| 修复后同一定向 integration 命令 | 通过，约 1.20 秒 |
| 隔离 MySQL 8.4.0 空库：`go run ./cmd/migrate up` | 全套真实迁移通过 |
| 同库第二次 `go run ./cmd/migrate up` | 返回 `database migrations already up to date` |
| `SELECT version, dirty FROM schema_migrations` | `11 / 0` |
| `cd backend && go vet ./internal/platform ./cmd/migrate ./migrations` | 通过 |

测试使用本任务新建、带任务 label 的 `gopulse-prfix-8f5d84967611` 容器，随机 loopback 端口 `56940`；核对 label 后已删除容器与匿名卷，未接触已有环境。临时日志和执行脚本位于 `/tmp/gopulse-pr-fix/`。未读取第三方依赖实现。

本地只复现失败原因并验证迁移范围，不声称完整 Compose 已重新通过；推送后由既有权威 CI 再执行完整门禁。提交前检查 diff 和暂存区，本任务仅提交上述三个 Go 文件、本记录及同名方案的 PR 跟进验收补充。
