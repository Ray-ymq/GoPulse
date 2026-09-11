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
