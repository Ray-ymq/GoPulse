# Phase-11-01：三类数据查询与双使用态前端闭环开发记录

## 1. 执行信息

- 执行日期：2026-09-05
- 开发分支：`develop/1.8.1`
- 开工基线：`upstream/main` / `52a43a7`
- 目标版本：`1.8.1`
- 当前结论：实现、固定本地门禁和隔离真实浏览器闭环已通过；Pull Request、远程 checks 和主分支合入尚未执行，因此未宣称满足方案第 10 节的远程完成条件。

## 2. 实际完成工作

### 2.1 Backend Metrics 固定查询边界

- 新增 `backend/internal/metricquery`，实现 10 个固定 Redis metric family、四档固定时间范围与固定 step 映射。
- 新增 `GET /api/v1/observability/metrics`，复用现有 `Authentication → RequireAdmin` 路由组；路由测试证明未登录与普通用户在 application/client 调用前分别返回 `401/403`。
- VictoriaMetrics client 使用 Basic Auth、`POST /prometheus/api/v1/query_range`、禁止 redirect、有界 timeout 和 2 MiB body 上限。
- 对 matrix 响应执行严格字段、provenance、family label、时间、有限数值、容量、重复/顺序校验，只构造公共 DTO；VictoriaMetrics 的固定 `stats` 字段被验证后丢弃，不进入公共响应。
- 新增 `metrics_unavailable`，将网络、超时、认证、非成功和不可信响应统一映射为安全 `503`。
- 增加 Backend VictoriaMetrics 配置；未将 VictoriaMetrics 加入 `/ready`。

### 2.2 Frontend 双使用态和三类查询页面

- 新增 `/admin/observability` 管理壳层、Metrics/Logs/Events 子路由、独立导航、Exporter 预留项和“返回社交”。
- 新增 `requiresAdmin` 守卫。进入管理域前强制刷新 `/users/me`；匿名进入登录，普通用户进入 `/forbidden`，未知管理子路由也不能绕过守卫。
- 普通主导航仅为 admin 显示“可观测”；运行中查询收到 `403 permission_denied` 时转入无权限页但不清除普通登录。
- 新增 Metrics、Logs、Events 显式 TypeScript DTO、严格 runtime validators、安全 query builder、opaque cursor 续页和 AbortController/请求序号过期响应保护。
- 三个页面实现 loading、empty、成功更新时间、刷新失败保留旧数据、无权限和依赖不可用反馈；Logs/Events 实现代表性分页与 cursor 失效反馈。
- Metrics 页面提供固定目录和范围、最后样本、语义化表格与轻量趋势；Logs/Events 仅显示白名单字段，未使用 raw JSON renderer 或 `v-html`。

### 2.3 生命周期、验收与文档

- `scripts/dev.sh` 增加 Backend VictoriaMetrics 配置传递和一致性校验，并支持受限的随机 `gopulse-observability-<12 hex>` 项目、`/tmp` 环境/运行目录以及可配置 Frontend 端口，供隔离验收使用。
- 新增 `scripts/verify-observability-ui.sh` 与 `--self-test`。真实模式分配随机端口和资源名，启动独立 Compose/应用栈，注册 admin/user、通过 `admin-role promote` 提权、等待当前真实 metric sample、生成当前 Backend log，并运行真实 Chromium 双会话用例；退出时只删除自有随机项目、volume 和临时目录。
- 更新 `.env.example`、根 README、Backend README 和 Linux CI self-test 入口；冻结 PowerShell 未修改。
- 根 `VERSION`、Frontend package metadata 已同步为 `1.8.1`。

## 3. 主要变更文件

- Backend：`backend/internal/metricquery/**`、`backend/internal/http/api.go`、`backend/internal/http/router_metrics_test.go`、`backend/internal/config/config.go`、`backend/cmd/server/main.go`、错误响应映射。
- Frontend：`frontend/src/components/AdminLayout.vue`、`AppNav.vue`、管理路由/auth/http、`services/observability.ts`、`types/observability.ts`、三个查询 view、无权限 view、分页 composable、样式和测试。
- 验收/配置：`scripts/verify-observability-ui.sh`、`scripts/dev.sh`、受配置影响的既有 Bash 验收脚本、`.env.example`、`.github/workflows/quality-gates.yml`。
- 文档/版本：`README.md`、`backend/README.md`、Phase 11 计划状态、`VERSION`、Frontend package metadata。

## 4. 实际验证

以下命令在 2026-09-05 的 WSL2/Linux workspace 中实际执行并成功：

- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd backend && go vet ./...)`
- `(cd backend && go test -race -count=1 ./internal/metricquery ./internal/http/...)`
- `(cd frontend && npm test -- --run)`：10 个 test files、53 个 tests 通过。
- `(cd frontend && npm run build)`
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 个 tests 通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.8.1 --base-ref upstream/main`
- `bash -n`（方案第 8 节列出的全部 Bash 脚本）
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-events.sh --self-test`
- `scripts/verify-logs.sh --self-test`
- `scripts/verify-marshaller.sh --self-test`
- `scripts/verify-business.sh --self-test`
- `scripts/verify-observability-ui.sh --self-test`
- `scripts/verify-observability-ui.sh`：最终 diff 上随机隔离项目中真实 Chromium 两个用例通过，覆盖普通用户无导航/直接 URL 零 observability 请求/API `403`，以及管理员 Metrics、Logs、Events 和返回社交闭环。

最终提交前的完整固定命令结果在本记录提交前再次核对；如有失败，将在本节和已知限制中如实更新。

## 5. 计划偏差与处理

- 预计文件包含 `backend/README.md`，基线中不存在该文件；本批按计划创建了专门的 Backend observability/config 说明。
- VictoriaMetrics `query_range` 的真实成功响应包含固定 `stats` 对象。初版 validator 因完全拒绝该对象而返回 `metrics_unavailable`；最终实现显式验证该固定对象并丢弃，公共 DTO 仍不暴露统计或任意上游字段。
- 首次真实浏览器试跑使用日常生命周期入口暴露出长 Basic Auth 值导致 BusyBox `base64` 换行和前台 lifecycle 阻塞问题。最终验收改为随机隔离 project/ports/run dir，并将示例 VM 密码设为恰好 32 字节；日常栈在调试后通过 `scripts/down.sh` 恢复为停止状态，named volumes 保留。
- 隔离验收初版只重写了 Kafka published port，没有同步 Router/Marshaller broker 地址，导致 Router readiness 失败；最终环境生成器同时绑定两者到随机隔离 Kafka，并增加受限 project/path 校验。最终隔离闭环重复执行成功。
- Phase 11 权威分配表原使用转义反引号，`validate_branch.py` 无法识别；本批仅修复三行分配表的 Markdown 反引号并验证 `develop/1.8.1` 唯一映射。

## 6. 已知限制与后续项

- Exporter 管理页面、上传/更新交互和四区域总览仍属于 Phase-11-02，本批未提前实现。
- 页面不自动轮询，不提供任意 MetricsQL/PromQL/Elasticsearch DSL、全文检索、rate/聚合、告警或复杂图表。
- Phase 8 单节点 VictoriaMetrics 仍复用 Marshaller Basic Auth 身份，这是总方案记录的本地 MVP 最小权限限制。
- 首次 push 后远程 Integration job 在 migration 步骤暴露出 CI 环境缺少新增 `BACKEND_VICTORIAMETRICS_PASSWORD`；已在同一批次补齐该安全测试值并触发新的远程门禁。
- PR、远程 checks 和主分支合入尚待执行；在这些远程条件成功前，Phase-11-01 不标记为最终完成。
