# Phase-15-05：管理大屏与告警权限操作闭环开发记录

## 1. 状态与范围

- 2026-09-12 开工；从 fetch 后的主远程 `origin/main`（`cbef6ea`，与配置的 `upstream` 指向同一仓库）创建 `develop/1.12.5`。用户原有未跟踪文件 `~` 未修改、未纳入提交。
- 已完成：固定批次门禁通过，产品完成版本更新为 `1.12.5`；Phase-15-06 尚未执行。
- 按本批合同实现 overview、默认大屏、alerts/users/audit 页面、插件 requested/completed 审计及唯一同源真实 Compose 验收入口，不重跑 Phase-15-03 全部故障/重启矩阵。

## 2. 已实现的合同

- `GET /api/v1/admin/overview?range=15m`：generated_at/status 及 components/key_metrics/logs/events/plugins/alerts 六个固定 section；每区 status/observed_at/reason_code/items。固定 6 个并发 worker、每区 2.5s 超时、总 context 3s，向 VM、ES、Monitor、MySQL 传播请求取消。
- key metrics 的非身份 label tuple 均为空；通过既有 metric catalog 保留固定 producer/target 身份约束：`gopulse_backend_outbox_pending`、`gopulse_business_worker_messages_in_flight`、`gopulse_search_indexer_retrying`、`gopulse_monitor_event_queue_length`、`gopulse_router_buffered_records`、`gopulse_marshaller_retrying`。单位 count；15m 原始样本窗口，最新样本超过 90s 标为 unknown/stale，缺值保留 null 而非填 0。
- components：六组件 catalog 的固定 dependency tuple；Monitor `last_scrape_success_timestamp_seconds` 使用 `scraped_producer_kind=component,scraped_target_id=<component>-local`，同时判断样本时间和成功时间的新鲜度。
- logs/events：已有内部 count repository + 固定 read alias + level/severity 为 info/warn/error，15m UTC 窗口；没有扩展公开规则 DSL 或 selector 验证。
- plugins：固定六 ID、Monitor 安全列表及对应 up 指标；返回 installed/desired/observed/up/最近成功，不返回错误原文、主机、凭据、PID、内部路径。
- alerts：MySQL 聚合 warning/critical firing、pending/unknown、近期恢复数量及最近五条恢复摘要、evaluator enabled 和最近规则评估成功时间。为当前/历史 DTO 增加只读 `data_status`（当前取 active incident 对应 state，历史为 historical），解决 UI 无法区分 firing 的 stale 数据的问题；不修改状态机或持久结构。
- Frontend：默认 `/admin/` 大屏、严格分区解码（未知字段只隔离该区）、本地时区、加载/失败/空/未知状态和刷新防重入；catalog-driven 三源规则创建/编辑/启停/删除、409 重读不覆盖；精确 ID 角色变更、bootstrap 提示、自降级清空 DOM 返回社交；签名 cursor 审计筛选/分页。
- 插件审计：授权后、调用前写 requested/unknown；调用返回后同 operation_id 写 completed/succeeded|failed。完成追加失败保留 unknown，不假装远程回滚；只写经过固定构造器校验的 details。历史 `/install` 仍是既有 Redis alias，按 Redis 归属记账。

## 3. 实际验证进度与修复

- 首次直接 Backend packages 和两端 unit/typecheck/build 通过；用户应用 16 个文件 / 62 tests；管理应用 6 个文件 / 23 tests。后续直接影响的 DTO/模板变化会以最终检查结果覆盖管理端与 Backend 结果，用户业务代码未改，不因上下文或剩余时间重复检查。
- `.run/gopulse-p1401-45db2e8e2cf3/`：真实六 key metric/count 快照生成；浏览器 self-demotion 通过。主流程发现 select 的隐式 label 把 option 文本包含在可访问名称中，精确定位失败；为新页面 select 显式添加 aria-label，未放宽验收。
- `.run/gopulse-p1401-80826d32c0a7/`：主浏览器 2/2 通过（真实三源创建/编辑、双写者 revision 409、三条实际 firing、停用后的 history、角色提升/降级、bootstrap 禁用、自降级、普通用户不发管理请求）；既有 Metrics/Logs/Events/Plugins 浏览器 1/1 通过；VM、ES、Monitor 三种故障 API/DOM 隔离各通过。完成审计故障注入的 MySQL trigger 创建失败，修正验收 SQL delimiter；本轮整体未完成。
- 为证明本批必要的 unknown/脱敏门槛，补充唯一归属数据库 trigger 拒绝 completed 写入、实际 DOM/bundle 特征串扫描；不是扩大到数据库或依赖审计。样本新鲜度、三秒并发预算使用直接 package 测试，不再重复已有三源状态机测试。
- 所有 Compose fixture 使用 `gopulse-p1401-<随机令牌>` 强归属资源与独立凭据、单一 edge origin、现有 Docker daemon；已有轮次退出时均清理本次资源并保留预存资源。原始凭据仅在未跟踪的受限验收目录中，不提交、不在记录中展开。

## 4. 最终验证结果

最终完整入口：`bash scripts/verify-admin-frontend.sh --dashboard-alerts-users`，证据目录 **`.run/gopulse-p1401-a4491e3e3099/`**，退出码 0；`results.json` 记录通过及强归属清理完成。

| 实际命令 / 场景 | 结果与证据 |
| --- | --- |
| `(cd backend && go test ./internal/adminoverview/... ./internal/exporterplugin ./internal/alert/... ./internal/http ./cmd/server)` | 通过；额外 cmd/server 只验证本批生产接线；`gopulse-dashboard-backend-final.log`。包括部分可用、90s 新鲜度、6-worker 2.5s 并发预算和完成追加失败不伪造成功 |
| `(cd admin-frontend && npm test)` | 6 test files / 23 tests 通过，含未知分区字段不得流入 DOM 数据；`gopulse-dashboard-admin-final.log` |
| 管理端 `npm run typecheck`、`npm run build` | 均通过；同上 |
| 用户端 `npm test`、`npm run typecheck`、`npm run build` | 16 test files / 62 tests、类型检查、构建均通过；`gopulse-dashboard-user-checks.log` |
| `bash scripts/verify-admin-frontend.sh --self-test` | 通过，无 Docker 副作用 |
| 最终唯一入口的 `dashboard.spec.ts` | 2/2 passed（20.4s）；三源真实规则触发、编辑/409/启停/历史、角色、bootstrap、自降级、普通用户不挂载；`browser.log` |
| 最终入口的既有专项页浏览器 | 1/1 passed；真实 Metrics/Logs/Events 和 Redis 停止/启动，六插件目录；`existing-browser.log` |
| VM、ES、Monitor 分别不可用 | 每种 API 3.5s 外部测量上限与正常 MySQL section/角色查询通过，3 个浏览器各 1/1 passed；`*-partial.json` / `*-browser.log`；内部 fan-out 3s 合同另由 package 测试证明 |
| 插件审计失败与脱敏 | 六操作 action 均可查询；真实 DB trigger 拒绝 stop completed 后只剩同 operation_id 的 requested/unknown；错误配置 completed/failed；`dashboard-audit.spec.ts` 1/1 passed（1.3s），DOM 与实际加载 bundle、Backend DTO、日志中特征串均未泄露 |
| 语法与 fixture 迁移 | `python3 -m py_compile scripts/ci/verify_dashboard.py scripts/ci/verify_admin_frontend.py`、`bash -n scripts/verify-admin-frontend.sh` 通过；`(cd frontend && npx playwright test --list)` 通过 |
| 版本 / 分支 | `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/1.12.5 --base-ref upstream/main` 通过 |

实际初始快照（不是 fixture）：六个 key metric 值均为真实 `0`；logs 为 info=5/warn=0/error=0；events 为 info=1/warn=0/error=0。components/plugins 为 degraded，其他四区 healthy。组件 dependency 的实际失败和未安装的其余插件均如实显示降级/unknown，没有为让大屏全绿填数字；完整值、时间、reason 保存在 `overview.json`。

第三轮 `.run/gopulse-p1401-03a36975558e/` 已证明真实 completed 写失败留下 unknown；随后验收对无效 multipart update 错误地期望 422，实际既有合同为 400。修正测试期望，不改变 API 或放宽断言；第四轮最终入口完整通过。前三轮均非整体完成证据。

## 5. 变更文件与直接回归范围

- Backend：新增 `internal/adminoverview/{overview,sources,handler}.go` 及直接测试；`internal/alert/overview.go`、`model.go`、`list.go` 增加只读摘要/data_status；`internal/user/plugin_audit.go`、`internal/exporterplugin/{audit,handler}.go` 及 audit test；`internal/http/api.go`、`cmd/server/main.go`、`componentmetrics/routes.go` 完成受权路由与生产接线。
- 管理端：`services/overview.ts` / test、`views/{Dashboard,Alerts,Users,Audit}View.vue`、router、AdminLayout、useAuth、styles。
- 验收：`scripts/ci/verify_dashboard.py`、`verify_admin_frontend.py`、`scripts/verify-admin-frontend.sh`；`frontend/e2e/dashboard*.spec.ts` 和 `admin-frontend.spec.ts`。
- 默认首页从临时 metrics redirect 变成大屏直接影响旧浏览器登录断言。同步更新 `compose-observability.spec.ts` 和六个 `phase14-*.spec.ts` 的登录后 URL 为 `/admin/`，不改后续测试场景或直接 `/admin/metrics` 路径。最终真实登录已经证明该合同，另以 Playwright `--list` 检查 fixture 可加载；未借此重跑全部 Phase 14 故障矩阵。
- 根 `VERSION`、`.env.example`、两端 package/lockfile 同步到 `1.12.5`，总/分方案与本记录更新状态。版本号按治理规则在业务门禁通过后更新；运行证据构建时的已完成基线仍为 `1.12.4`，不将这些证据伪称为 `1.12.5` 镜像标签验收。产品代码与实际校验镜像一致，最终 `1.12.5` 元数据由版本校验确认。

## 6. 偏差、限制与交接

- overview 计数使用既有受限内部 repository，不改变公开告警 selector；恢复摘要和当前 incident 的 data_status 是为满足本批 UI 所必需的只读 DTO 补充，无 schema/state-machine 修改。
- 管理规则创建使用 catalog 固定 tuple 下拉，不提供自由表达式或任意高级 selector 编辑器；窗口/持续/严重度/运算/聚合仍由服务端复核。页面不重算告警规则。
- evaluator 最近成功为 MySQL 中规则评估成功的最大时间；无成功评估时保持 null，不捏造 evaluator 健康心跳。
- 此次证明 Linux/Bash、唯一 Compose edge origin、既有 daemon；不代表 Phase 16 跨平台、多架构、TLS、升级恢复或 Kubernetes 支持完成。
- Phase-15-06 复用本批最后目录和前四批仍有效证据，运行最终跨批验收；无需重跑本批已成功的 package/UI 单测，除非相关代码/配置/环境变化。
- 提交前工作树与暂存区 whitespace 检查、提交后相对 upstream/main 检查和推送结果，以本次终端实际输出为准。
