# Phase-11-02：Exporter 管理与可观测总览闭环开发记录

## 1. 执行信息

- 执行日期：2026-09-05
- 开发分支：`develop/1.8.2`
- 开工基线：`upstream/main` / `3e776f9`
- 目标版本：`1.8.2`
- 当前结论：本地实现与方案固定完成门禁已通过；Pull Request、远程固定门禁与合入状态尚待完成，因此本批尚未满足实施方案第 10 节的最终完成条件。

## 2. 实际完成工作

### 2.1 Backend Exporter 信任边界

- 将 Monitor 成功响应改为先读取不超过 1 MiB 的完整 body，再递归检查重复 JSON key、尾随 token 和非法结构；data envelope、Status、SafeError 与错误 envelope 均执行精确字段检查。
- 固定 `redis-exporter`、`metrics-exporter`、`redis`、稳定三段 SemVer、已知 desired/observed state、UTC RFC3339Nano 时间、受限名称和安全错误 code/message 组合。
- 校验安装/更新、启动、采集、成功和错误时间关系；running 必须拥有启动时间，last success 不得晚于 last scrape，非法状态组合统一拒绝。
- List 最多接受一个固定插件并稳定排序；Get、start、stop、install、update 只在验证成功后返回显式 Backend DTO，`last_error` 不再使用 `any`。
- Monitor redirect、网络、timeout、超限、畸形、未知字段、未知错误与非预期成功状态统一映射为 `503 monitor_unavailable`；已知 Plugin Manager 业务错误继续映射固定公共 code/status。
- 保留 Backend 与 Monitor 双重 multipart/大小边界；Backend 重新生成 multipart boundary，浏览器不接触 Monitor token 或内部地址。

### 2.2 Frontend Exporter 管理

- 新增 ExporterStatus、SafeError、desired/observed state 与安全错误类型，以及与 Backend 语义一致的严格 runtime validator。
- 新增 list/get/start/stop/install/update client；安装和更新使用原生 `FormData`，公共 HTTP 层不再为 multipart 手工设置 `Content-Type`。
- 增加 `.tar.gz`、非空、64 MiB 前端提示，并在操作成功后清理所选文件；服务端仍是最终安全边界。
- 新增 Exporter 页面，覆盖未安装入口、running/stopped/failed/过渡状态、安装/更新/启动/采集/成功时间、安全错误、重复提交抑制和 stop/update 明确确认。
- 操作成功使用响应 DTO 原子更新当前事实；失败保留已验证旧状态，并区分 package invalid、not found、conflict、operation in progress/failed、Monitor unavailable 和权限变化。

### 2.3 四区域总览与真实浏览器闭环

- `/admin/observability` 改为正式总览，导航加入总览和 Exporter；Metrics、Logs、Events 与 Exporter 四个区域独立发起请求、保存结果、显示更新时间、错误和重试入口。
- 总览固定查询最近 15 分钟的 `gopulse_redis_up`、最近 Logs、最近 Events 和当前 Exporter；不增加 Backend 聚合 API，不把历史样本或 Events 当作插件当前事实。
- 扩展真实 Chromium 验收：普通用户直接 URL 与全部 Exporter API 均为服务端 `403`；管理员通过浏览器完成总览、stop、start、update、Events 核对、Metrics 核对和返回社交域。
- 隔离验收在验证随机 Compose project/container 与 Monitor PID/cwd/executable/start ticks/command marker 归属后，真实停止并恢复 VictoriaMetrics、暂停并恢复 Monitor；浏览器证明只有对应总览区域失败，其他区域保留成功结果并可独立恢复。
- 隔离脚本继续使用随机端口、临时环境、临时 run/plugin root 和退出清理；本批未修改冻结 PowerShell，也未修改 Monitor/Router/Marshaller production code。

### 2.4 文档与版本

- 根 README 和 Backend README 增加四区域总览、Exporter 管理、严格 Monitor DTO、multipart 与故障隔离说明。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 同步为 `1.8.2`。

## 3. 主要变更文件

- Backend：`backend/internal/exporterplugin/client.go`、`client_test.go`、`handler.go`。
- Frontend：`frontend/src/types/exporter.ts`、`services/exporters.ts`、`services/exporters.test.ts`、`services/http.ts`、`views/ObservabilityOverviewView.vue`、`views/ObservabilityExportersView.vue`、管理路由/布局、样式。
- 浏览器与验收：`frontend/e2e/observability.spec.ts`、`scripts/verify-observability-ui.sh`。
- 文档与版本：`README.md`、`backend/README.md`、Phase 11 计划状态、`VERSION`、Frontend package metadata。

## 4. 实际验证

以下方案固定命令在 2026-09-05 的 WSL2/Linux workspace 最终 diff 上实际执行并成功：

- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd backend && go vet ./...)`
- `(cd backend && go test -race -count=1 ./internal/exporterplugin ./internal/http/...)`
- `(cd frontend && npm test -- --run)`：11 个 test files、56 个 tests 通过。
- `(cd frontend && npm run build)`
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 个 tests 通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.8.2 --base-ref upstream/main`
- 方案第 8 节列出的全部 Bash 脚本 `bash -n` 检查。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- `scripts/verify-observability-ui.sh --self-test`
- `scripts/verify-observability-ui.sh`：真实 Chromium 3 个用例通过，包含双使用态、stop/start/update、四区域总览、Events/Metrics 核对、VictoriaMetrics 与 Monitor 真实局部故障及恢复。
- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-events.sh --self-test`
- `scripts/verify-marshaller.sh --self-test`
- `scripts/verify-business.sh --self-test`
- `git diff --check`

固定门禁最终输出：`ALL_PHASE_11_02_GATES_PASSED`。

## 5. 计划偏差与处理

- Monitor 的真实固定 kind 为 `metrics-exporter`，不是泛化的 `metrics`；实现按当前真实 manifest/Status 合同冻结该值。
- 日常 `dev.sh` 会在 Frontend 启动前恢复或安装当前仓库 Exporter，因此真实浏览器主闭环以 stop/start/update 作为代表管理转换；未安装卡片、install multipart client 和边界由同一产品代码与组件/API 测试覆盖，生命周期自身仍真实执行 install。
- 第一次浏览器试跑发生在 Backend kind 收紧修正前，Status 被安全拒绝；按真实 Monitor body 修正固定 kind 后重跑。第二次试跑暴露 Exporter 页面初始 loading 时错误进入已安装模板并读取 null；增加 `loaded` 状态和显式已安装分支后，最终真实浏览器验收重复成功。
- 为完成方案要求的真实局部故障闭环，浏览器测试增加了受限基础设施控制：只允许 `gopulse-observability-<12 hex>` project，停止 VM 前验证 Compose label，暂停 Monitor 前验证完整进程身份；`finally` 总是恢复服务/进程，脚本退出陷阱仍清理自有资源。

## 6. 已知限制与后续项

- 总览是四个独立请求的近期视图，不是强一致快照，也不自动轮询。
- Events 保持 best-effort/at-least-once，可用于核对但不作为插件当前状态事实源。
- Phase 8 单节点 VictoriaMetrics 仍复用 Marshaller Basic Auth 身份；浏览器不可获得该身份，Phase 12 继续负责网络收口。
- 多插件、插件市场、卸载、任意 package URL、自动更新、告警、复杂图表和任意查询均未实现。
- Pull Request、远程固定门禁与主远程合入结果将在实际发生后更新；在此之前不得将 Phase-11-02 标记为最终完成。
